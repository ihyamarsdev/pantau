package store

import (
	"encoding/json"
	"strings"
	"time"
)

// fleetSearchSSHKeys searches the SSH Key Library (ADR 0035). The private key
// is never searched or returned; only name, fingerprint and public key are.
func (d *DB) fleetSearchSSHKeys(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"k.name", "k.fingerprint", "k.public_key"}, tokens)
	q := `SELECT k.id, k.name, k.fingerprint, k.created_at FROM ssh_keys k WHERE ` + where + ` ORDER BY k.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, fingerprint string
		var created time.Time
		if err := rows.Scan(&id, &name, &fingerprint, &created); err != nil {
			return err
		}
		key := FleetSearchSSHKey + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchSSHKey, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "keys", Subtitle: fingerprint,
		}, FleetSearchHost{Name: name, DataAge: &created})
	}
	return rows.Err()
}

// fleetSearchAccounts searches audited accounts and keys from account_audits.
// The payload is JSON (users_json, keys_json, findings_json), so we scan only
// hosts with an audit and filter in Go.
func (d *DB) fleetSearchAccounts(b *fleetGroupBuilder, tokens []string, limit int) error {
	rows, err := d.Query(`SELECT a.host_id, a.users_json, a.keys_json, a.findings_json, a.scanned_at, h.name, h.group_name
		FROM account_audits a JOIN hosts h ON h.id = a.host_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var hostID int64
		var usersRaw, keysRaw, findingsRaw string
		var scanned time.Time
		var hostName, groupName string
		if err := rows.Scan(&hostID, &usersRaw, &keysRaw, &findingsRaw, &scanned, &hostName, &groupName); err != nil {
			return err
		}
		var users []AccountUser
		_ = json.Unmarshal([]byte(usersRaw), &users)
		var keys []AccountKey
		_ = json.Unmarshal([]byte(keysRaw), &keys)
		var findings []AccountFinding
		_ = json.Unmarshal([]byte(findingsRaw), &findings)

		for _, u := range users {
			text := strings.Join([]string{u.Name, u.Shell, u.Home, u.PasswordState}, " ")
			if !matchesTokens(text, tokens) {
				continue
			}
			key := FleetSearchAccount + "\x00user\x00" + strings.ToLower(u.Name)
			b.addMeta(FleetSearchAccount, key, u.Name, FleetSearchGroup{
				EntityID: strconvItoa64(hostID), TargetTab: "security",
				Subtitle: "account · " + u.Shell, StatusBadge: u.PasswordState,
			}, FleetSearchHost{HostID: hostID, Name: hostName, Group: groupName, DataAge: &scanned})
			found++
			if limit > 0 && found >= limit {
				return nil
			}
		}
		for _, k := range keys {
			text := strings.Join([]string{k.User, k.Comment, k.Fingerprint, k.Type}, " ")
			if !matchesTokens(text, tokens) {
				continue
			}
			label := k.User
			if k.Comment != "" {
				label += " · " + k.Comment
			}
			key := FleetSearchAccount + "\x00key\x00" + strings.ToLower(k.Fingerprint)
			badge := "key"
			if k.Unknown {
				badge = "unknown key"
			} else if k.Weak {
				badge = "weak key"
			}
			b.addMeta(FleetSearchAccount, key, label, FleetSearchGroup{
				EntityID: strconvItoa64(hostID), TargetTab: "security",
				Subtitle: k.Fingerprint, StatusBadge: badge,
			}, FleetSearchHost{HostID: hostID, Name: hostName, Group: groupName, DataAge: &scanned})
			found++
			if limit > 0 && found >= limit {
				return nil
			}
		}
		for _, f := range findings {
			text := strings.Join([]string{f.ID, f.Title, f.Detail, f.User}, " ")
			if !matchesTokens(text, tokens) {
				continue
			}
			key := FleetSearchAccount + "\x00finding\x00" + strings.ToLower(f.ID)
			b.addMeta(FleetSearchAccount, key, f.Title, FleetSearchGroup{
				EntityID: strconvItoa64(hostID), TargetTab: "security",
				Subtitle: f.User, StatusBadge: f.Severity,
			}, FleetSearchHost{HostID: hostID, Name: hostName, Group: groupName, DataAge: &scanned})
			found++
			if limit > 0 && found >= limit {
				return nil
			}
		}
	}
	return rows.Err()
}
