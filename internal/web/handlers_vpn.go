package web

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"pantau/internal/store"
	"pantau/internal/vpnnet"
)

const (
	maxVPNConfig       = 64 << 10
	maxVPNCommand      = 512
	maxVPNNetworks     = 20
	vpnNamePattern     = `^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`
	vpnIfacePattern    = `^[A-Za-z0-9_.-]{0,15}$`
	secretPlaceholder  = ""
	secretSetSuffix    = "_set"
	vpnRedactedMessage = "(redacted)"
)

var (
	vpnNameRe  = regexp.MustCompile(vpnNamePattern)
	vpnIfaceRe = regexp.MustCompile(vpnIfacePattern)
)

// vpnGateOn reports the opt-in gate that allows vendor binaries (and custom shell commands) to run.
func (s *Server) vpnGateOn() bool {
	v, err := s.db.GetSetting("vpn_enabled")
	return err == nil && v == "1"
}

// handleVPN serves /api/vpn/*.
func (s *Server) handleVPN(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/vpn/"), "/")
	switch rest {
	case "status":
		if r.Method == http.MethodGet {
			s.handleVPNStatus(w)
			return
		}
	case "interfaces":
		if r.Method == http.MethodGet {
			s.handleVPNInterfaces(w)
			return
		}
	case "gate":
		if r.Method == http.MethodPut {
			s.handleVPNGate(w, r)
			return
		}
	}

	// Everything else lives under /api/vpn/networks[/{id}[/{action}]].
	netRest := strings.Trim(strings.TrimPrefix(rest, "networks"), "/")
	parts := []string{}
	if netRest != "" {
		parts = strings.Split(netRest, "/")
	}
	switch {
	case len(parts) == 0:
		switch r.Method {
		case http.MethodGet:
			s.handleVPNNetworkList(w)
		case http.MethodPost:
			s.handleVPNNetworkCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	case len(parts) == 1:
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "bad network id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			s.handleVPNNetworkUpdate(w, r, id)
		case http.MethodDelete:
			s.handleVPNNetworkDelete(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	case len(parts) == 2:
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "bad network id", http.StatusBadRequest)
			return
		}
		switch parts[1] {
		case "up":
			if r.Method == http.MethodPost {
				s.handleVPNNetworkAction(w, r, parts[0], true)
				return
			}
		case "down":
			if r.Method == http.MethodPost {
				s.handleVPNNetworkAction(w, r, parts[0], false)
				return
			}
		case "log":
			if r.Method == http.MethodGet {
				s.handleVPNNetworkLog(w, parts[0])
				return
			}
		}
		_ = id
	}
	http.NotFound(w, r)
}

func (s *Server) handleVPNStatus(w http.ResponseWriter) {
	type driverInfo struct {
		Driver  string `json:"driver"`
		Binary  string `json:"binary"`
		Version string `json:"version"`
		Error   string `json:"error,omitempty"`
	}
	out := struct {
		Enabled bool         `json:"enabled"`
		Drivers []driverInfo `json:"drivers"`
	}{Enabled: s.vpnGateOn()}
	for _, d := range vpnnet.Drivers {
		path, version, errText := vpnnet.DetectBinary(d)
		out.Drivers = append(out.Drivers, driverInfo{Driver: d, Binary: path, Version: version, Error: errText})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVPNInterfaces(w http.ResponseWriter) {
	type ifaceInfo struct {
		Name  string   `json:"name"`
		Index int      `json:"index"`
		Addrs []string `json:"addrs"`
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]ifaceInfo, 0, len(ifaces))
	for _, ifi := range ifaces {
		info := ifaceInfo{Name: ifi.Name, Index: ifi.Index}
		if addrs, err := ifi.Addrs(); err == nil {
			for _, a := range addrs {
				info.Addrs = append(info.Addrs, a.String())
			}
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVPNGate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	v := "0"
	if req.Enabled {
		v = "1"
	}
	if err := s.db.SetSetting("vpn_enabled", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// vpnConfigJSON parses a submitted config object, rejecting unknown/oversized input.
func vpnConfigJSON(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	if len(raw) > maxVPNConfig {
		return nil, fmt.Errorf("config is larger than 64 KiB")
	}
	cfg := map[string]string{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("config must be an object of strings")
	}
	for k, v := range cfg {
		if len(v) > maxVPNConfig {
			return nil, fmt.Errorf("config field %q is too large", k)
		}
	}
	return cfg, nil
}

func vpnSecretKeys(driver string) []string {
	return vpnnet.SecretKeys(driver)
}

// redactVPNConfig blanks secrets for the API and marks which of them are stored.
func redactVPNConfig(driver, rawConfig string) map[string]string {
	cfg := map[string]string{}
	if strings.TrimSpace(rawConfig) != "" {
		_ = json.Unmarshal([]byte(rawConfig), &cfg)
	}
	for _, k := range vpnSecretKeys(driver) {
		if strings.TrimSpace(cfg[k]) != "" {
			cfg[k+secretSetSuffix] = "true"
		}
		cfg[k] = secretPlaceholder
	}
	return cfg
}

// keepStoredSecrets fills blank secrets in an update with the values already saved.
func keepStoredSecrets(newCfg, oldCfg map[string]string, driver string) {
	for _, k := range vpnSecretKeys(driver) {
		if strings.TrimSpace(newCfg[k]) == "" {
			if old := oldCfg[k]; old != "" {
				newCfg[k] = old
			}
			delete(newCfg, k+secretSetSuffix)
		}
	}
}

// validateVPNNetwork enforces the shapes and the cross-network rules (one shared netbird, unique iface/port).
func (s *Server) validateVPNNetwork(n *store.VPNNetwork, cfg map[string]string, existing []store.VPNNetwork, selfID int64) error {
	n.Name = strings.TrimSpace(n.Name)
	if !vpnNameRe.MatchString(n.Name) {
		return fmt.Errorf("name must match %s", vpnNamePattern)
	}
	if !vpnnet.ValidDriver(n.Driver) {
		return fmt.Errorf("unknown driver %q", n.Driver)
	}
	if !vpnIfaceRe.MatchString(n.Iface) {
		return fmt.Errorf("interface must match %s", vpnIfacePattern)
	}
	if n.Driver == vpnnet.DriverCustom {
		if strings.TrimSpace(n.Iface) == "" {
			return fmt.Errorf("custom driver needs an interface")
		}
		for _, k := range []string{"up", "status"} {
			if strings.TrimSpace(cfg[k]) == "" {
				return fmt.Errorf("custom driver needs a %q command", k)
			}
		}
	}
	for _, k := range vpnSecretKeys(n.Driver) {
		if v := cfg[k]; len(v) > maxVPNConfig {
			return fmt.Errorf("config field %q is too large", k)
		}
	}
	for _, k := range []string{"up", "down", "status"} {
		if v := cfg[k]; len(v) > maxVPNCommand {
			return fmt.Errorf("%q command is longer than %d bytes", k, maxVPNCommand)
		}
	}
	if n.Driver == vpnnet.DriverNetBird {
		mode := cfg["mode"]
		if mode != "" && mode != "shared" && mode != "instance" {
			return fmt.Errorf(`netbird mode must be "shared" or "instance"`)
		}
	}
	if selfID == 0 && len(existing) >= maxVPNNetworks {
		return fmt.Errorf("at most %d networks", maxVPNNetworks)
	}
	ifaceKey := func(driver string, c map[string]string, iface string) string {
		if iface != "" {
			return iface
		}
		switch driver {
		case vpnnet.DriverNetBird:
			if v := c["interface_name"]; v != "" {
				return v
			}
			return "wt0"
		case vpnnet.DriverTailscale:
			if v := c["interface_name"]; v != "" {
				return v
			}
			return "tailscale0"
		case vpnnet.DriverWireGuard:
			if v := c["interface_name"]; v != "" {
				return v
			}
			return "wg0"
		case vpnnet.DriverNetMaker:
			if v := c["interface"]; v != "" {
				return v
			}
			return "netmaker"
		}
		return ""
	}
	myIface := ifaceKey(n.Driver, cfg, n.Iface)
	myPort := cfg["wireguard_port"]
	if n.Driver == vpnnet.DriverNetBird {
		myMode := cfg["mode"]
		if myMode == "" {
			myMode = "shared"
		}
		if myMode == "shared" {
			for _, other := range existing {
				if other.ID == selfID || other.Driver != vpnnet.DriverNetBird {
					continue
				}
				otherCfg := map[string]string{}
				_ = json.Unmarshal([]byte(other.Config), &otherCfg)
				if m := otherCfg["mode"]; m == "" || m == "shared" {
					return fmt.Errorf("only one netbird network may drive the shared daemon: %q already does; use mode \"instance\" for a second account", other.Name)
				}
			}
		}
	}
	for _, other := range existing {
		if other.ID == selfID {
			continue
		}
		otherCfg := map[string]string{}
		_ = json.Unmarshal([]byte(other.Config), &otherCfg)
		if n.Driver == vpnnet.DriverNetBird && other.Driver == vpnnet.DriverNetBird {
			if cfg["mode"] == "instance" && otherCfg["mode"] == "instance" && otherCfg["wireguard_port"] != "" && myPort == otherCfg["wireguard_port"] {
				return fmt.Errorf("wireguard port %s is already used by %q", myPort, other.Name)
			}
		}
		if oi := ifaceKey(other.Driver, otherCfg, other.Iface); oi != "" && oi == myIface {
			return fmt.Errorf("interface %s is already used by %q", myIface, other.Name)
		}
	}
	return nil
}

func (s *Server) vpnNetworkFromRequest(r *http.Request, id int64) (*store.VPNNetwork, map[string]string, error) {
	var req struct {
		Name    string          `json:"name"`
		Driver  string          `json:"driver"`
		Enabled bool            `json:"enabled"`
		AutoUp  *bool           `json:"auto_up"`
		Iface   string          `json:"iface"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, nil, fmt.Errorf("bad request")
	}
	cfg, err := vpnConfigJSON(req.Config)
	if err != nil {
		return nil, nil, err
	}
	autoUp := true
	if req.AutoUp != nil {
		autoUp = *req.AutoUp
	}
	n := &store.VPNNetwork{ID: id, Name: req.Name, Driver: req.Driver, Enabled: req.Enabled, AutoUp: autoUp, Iface: strings.TrimSpace(req.Iface)}
	return n, cfg, nil
}

func (s *Server) handleVPNNetworkList(w http.ResponseWriter) {
	rows, err := s.db.ListVPNNetworks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	counts, err := s.db.CountHostsByNetwork()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		ID          int64             `json:"id"`
		Name        string            `json:"name"`
		Driver      string            `json:"driver"`
		Enabled     bool              `json:"enabled"`
		AutoUp      bool              `json:"auto_up"`
		Iface       string            `json:"iface"`
		Config      map[string]string `json:"config"`
		LastState   string            `json:"last_state"`
		LastError   string            `json:"last_error"`
		LastChecked interface{}       `json:"last_checked,omitempty"`
		LastUpAt    interface{}       `json:"last_up_at,omitempty"`
		HostsCount  int               `json:"hosts_count"`
	}
	out := make([]row, 0, len(rows))
	for _, n := range rows {
		state := n.LastState
		if s.vpnMgr != nil {
			if st := s.vpnMgr.Status(n.ID); st.State != "" {
				state = st.State
			}
		}
		out = append(out, row{ID: n.ID, Name: n.Name, Driver: n.Driver, Enabled: n.Enabled, AutoUp: n.AutoUp, Iface: n.Iface,
			Config: redactVPNConfig(n.Driver, n.Config), LastState: state, LastError: n.LastError,
			LastChecked: n.LastChecked, LastUpAt: n.LastUpAt, HostsCount: counts[n.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVPNNetworkCreate(w http.ResponseWriter, r *http.Request) {
	n, cfg, err := s.vpnNetworkFromRequest(r, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := s.db.ListVPNNetworks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.validateVPNNetwork(n, cfg, existing, 0); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	raw, _ := json.Marshal(cfg)
	n.Config = string(raw)
	id, err := s.db.CreateVPNNetwork(n)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) handleVPNNetworkUpdate(w http.ResponseWriter, r *http.Request, id int64) {
	current, err := s.db.GetVPNNetwork(id)
	if err != nil || current == nil {
		http.Error(w, "network not found", http.StatusNotFound)
		return
	}
	n, cfg, err := s.vpnNetworkFromRequest(r, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newRaw, _ := json.Marshal(cfg)
	n.Config = string(newRaw)
	if r.URL.Query().Get("keep_secrets") != "0" {
		oldCfg := map[string]string{}
		_ = json.Unmarshal([]byte(current.Config), &oldCfg)
		keepStoredSecrets(cfg, oldCfg, n.Driver)
		merged, _ := json.Marshal(cfg)
		n.Config = string(merged)
	}
	existing, err := s.db.ListVPNNetworks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.validateVPNNetwork(n, cfg, existing, id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	if err := s.db.UpdateVPNNetwork(n); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	if s.vpnMgr != nil {
		s.vpnMgr.Invalidate(id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleVPNNetworkDelete(w http.ResponseWriter, r *http.Request, id int64) {
	current, err := s.db.GetVPNNetwork(id)
	if err != nil || current == nil {
		http.Error(w, "network not found", http.StatusNotFound)
		return
	}
	counts, err := s.db.CountHostsByNetwork()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if used := counts[id]; used > 0 {
		if r.URL.Query().Get("force") != "1" {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "network still used by hosts", "hosts": used})
			return
		}
		if err := s.db.DetachHostsFromNetwork(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if s.vpnMgr != nil {
		_ = s.vpnMgr.Down(r.Context(), id)
	}
	if err := s.db.DeleteVPNNetwork(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.vpnMgr != nil {
		s.vpnMgr.Forget(id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleVPNNetworkAction(w http.ResponseWriter, r *http.Request, rawID string, up bool) {
	if !s.vpnGateOn() {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "VPN gate is disabled; enable it first"})
		return
	}
	if s.vpnMgr == nil {
		http.Error(w, "vpn manager unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		http.Error(w, "bad network id", http.StatusBadRequest)
		return
	}
	if up {
		st, err := s.vpnMgr.Up(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error(), "status": st})
			return
		}
		writeJSON(w, http.StatusOK, st)
		return
	}
	if err := s.vpnMgr.Down(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleVPNNetworkLog(w http.ResponseWriter, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		http.Error(w, "bad network id", http.StatusBadRequest)
		return
	}
	if s.vpnMgr == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"lines": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"lines": s.vpnMgr.Logs(id)})
}

// validNetworkID reports whether a Host may point at that network (0 always means direct).
func (s *Server) validNetworkID(id int64) error {
	if id == 0 {
		return nil
	}
	n, err := s.db.GetVPNNetwork(id)
	if err != nil {
		return err
	}
	if n == nil {
		return fmt.Errorf("vpn network %d not found", id)
	}
	return nil
}
