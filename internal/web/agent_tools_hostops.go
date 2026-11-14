package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// Agent tools for the per-host runtime operations the operator can do in the UI (ADR-0066/0073) that the
// older host tools do not cover: the rest of the Docker management (container remove/pause/kill/rename/run,
// images, volumes, networks, stack deploy, prune), the package tab (upgrade/install/remove), the File Manager
// (write, duplicate, delete, chmod, upload of text content), the S3 upload of a host file, the cross-host
// transfers and the nginx config removal of the Domains tab.
//
// Every change goes through the same HTTP handler the UI uses (loopback), so the protected paths, the disk
// space guard, the cooldowns, the secret masking and the audit log are the UI's. Gate 1 is a reversible
// single-resource change on one host, gate 2 is destructive (delete, prune, remove, kill, overwrite) and
// always asks. Gate 3 is not used here: every action is scoped to one host.

func init() { agentTools = append(agentTools, hostOpsTools...) }

var (
	pFilePath = map[string]interface{}{"type": "string", "description": "absolute path on the host"}
	pText     = map[string]interface{}{"type": "string", "description": "file content"}
	pImage    = map[string]interface{}{"type": "string", "description": "image reference, as listed by docker_info images"}
)

var hostOpsTools = []agentTool{
	{Name: "docker_container_action", Description: "Docker container action on a host: start, stop, restart, pause, unpause, kill, rename (needs new_name) or remove (force, volumes). kill and remove always ask; the rest may wait for operator approval.",
		InputSchema: schema([]string{"host_id", "container", "action"}, map[string]interface{}{"host_id": pHost, "container": pName,
			"action":   map[string]interface{}{"type": "string", "enum": []string{"start", "stop", "restart", "pause", "unpause", "kill", "rename", "remove"}},
			"new_name": map[string]interface{}{"type": "string", "description": "new name for rename"},
			"force":    map[string]interface{}{"type": "boolean", "description": "remove: kill a running container too"},
			"volumes":  map[string]interface{}{"type": "boolean", "description": "remove: also remove anonymous volumes"}}),
		gate: gateHigh, run: toolDockerContainerAction},
	{Name: "docker_run", Description: "Run a new container on a host from an image (name, ports, env, volumes, network, restart policy, command, memory, cpus). May wait for operator approval.",
		InputSchema: schema([]string{"host_id", "image"}, map[string]interface{}{"host_id": pHost, "image": pImage,
			"name": str, "ports": map[string]interface{}{"type": "array", "items": str}, "env": map[string]interface{}{"type": "array", "items": str},
			"volumes": map[string]interface{}{"type": "array", "items": str}, "network": str, "restart": str, "command": str, "memory": str, "cpus": str}),
		gate: gateRoutine, run: toolDockerRun},
	{Name: "docker_image_action", Description: "Docker image action on a host: pull, remove (force) or prune (unused images) / prune_all (every unused image). Removal and prune always ask.",
		InputSchema: schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost,
			"action": map[string]interface{}{"type": "string", "enum": []string{"pull", "remove", "prune", "prune_all"}}, "image": pImage, "force": map[string]interface{}{"type": "boolean"}}),
		gate: gateHigh, run: toolDockerImageAction},
	{Name: "docker_volume_action", Description: "Docker volume action on a host: create, remove, prune (unused anonymous volumes) or prune_all (every unused volume). Removal and prune always ask.",
		InputSchema: schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost,
			"action": map[string]interface{}{"type": "string", "enum": []string{"create", "remove", "prune", "prune_all"}}, "name": str}),
		gate: gateHigh, run: toolDockerVolumeAction},
	{Name: "docker_network_action", Description: "Docker network action on a host: create (driver, subnet, gateway, internal), remove, connect or disconnect a container, prune. Removal, disconnect and prune always ask.",
		InputSchema: schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost,
			"action": map[string]interface{}{"type": "string", "enum": []string{"create", "remove", "connect", "disconnect", "prune"}},
			"name":   str, "container": pName, "driver": str, "subnet": str, "gateway": str, "internal": map[string]interface{}{"type": "boolean"}}),
		gate: gateHigh, run: toolDockerNetworkAction},
	{Name: "docker_prune", Description: "Remove unused Docker objects of one kind on a host: containers, images, images_all (every unused image), volumes, networks or builder. Always asks; there is no \"everything\" target.",
		InputSchema: schema([]string{"host_id", "target"}, map[string]interface{}{"host_id": pHost,
			"target": map[string]interface{}{"type": "string", "enum": []string{"containers", "images", "images_all", "volumes", "networks", "builder"}}}),
		gate: gateHigh, run: toolDockerPrune},
	{Name: "docker_stack_deploy", Description: "Deploy a Compose stack on a host from a compose file (yaml): creates it under the Pantau stack directory or rewrites the compose file of an existing stack, validates it and runs up -d. May wait for operator approval.",
		InputSchema: schema([]string{"host_id", "name", "yaml"}, map[string]interface{}{"host_id": pHost, "name": str, "yaml": map[string]interface{}{"type": "string", "description": "compose file content"}}),
		gate:        gateRoutine, run: toolDockerStackDeploy},

	{Name: "pkg_action", Description: "Change packages on a host with its package manager: upgrade (all pending, or mode security for the security ones only), or install/remove/upgrade the named packages. Remote services can restart; there is no rollback. Always asks.",
		InputSchema: schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost,
			"action":   map[string]interface{}{"type": "string", "enum": []string{"upgrade", "install", "remove"}},
			"mode":     map[string]interface{}{"type": "string", "enum": []string{"all", "security"}, "description": "upgrade only: all pending packages or the security ones"},
			"packages": map[string]interface{}{"type": "array", "items": str, "description": "install/remove/upgrade: the package names (max 20)"}}),
		gate: gateHigh, run: toolPkgAction},

	{Name: "file_action", Description: "Change a file or folder on a host: write (create or overwrite a text file), duplicate (copy next to the original), delete (recursive for a folder) or chmod (mode like 0644). Protected system paths are refused. Deleting, overwriting and chmod always ask.",
		InputSchema: schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost,
			"action": map[string]interface{}{"type": "string", "enum": []string{"write", "duplicate", "delete", "chmod"}},
			"path":   pFilePath, "content": pText, "new_name": map[string]interface{}{"type": "string", "description": "duplicate: the name of the copy"},
			"recursive": map[string]interface{}{"type": "boolean", "description": "delete: remove a folder with its content"},
			"mode":      map[string]interface{}{"type": "string", "description": "chmod: the octal mode, like 0644"}}),
		gate: gateHigh, run: toolFileAction},
	{Name: "file_upload_text", Description: "Write a text file on a host (create it, or overwrite it with overwrite=true). The content is text, not binary. Protected system paths are refused. Overwriting always asks.",
		InputSchema: schema([]string{"host_id", "path", "content"}, map[string]interface{}{"host_id": pHost, "path": pFilePath, "content": pText,
			"overwrite": map[string]interface{}{"type": "boolean", "description": "replace an existing file (default: refuse)"}}),
		gate: gateHigh, run: toolFileUploadText},

	{Name: "transfer_action", Description: "Copy data between hosts or to an S3 account: action = transfer (host to host, dest_host_id and dest_path), s3 (upload a file of the host to the S3 account with that name) or cancel (a running job, job_id). Read the jobs with transfer_jobs.",
		InputSchema: schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost,
			"action": map[string]interface{}{"type": "string", "enum": []string{"transfer", "s3", "cancel"}},
			"path":   pFilePath, "dest_host_id": map[string]interface{}{"type": "integer", "description": "transfer: the destination host id from list_hosts"},
			"dest_path":  map[string]interface{}{"type": "string", "description": "transfer: the destination path on that host"},
			"mode":       map[string]interface{}{"type": "string", "enum": []string{"fast", "verified"}, "description": "transfer: fast or verified (checksummed)"},
			"s3_account": map[string]interface{}{"type": "string", "description": "s3: the name of an S3 account configured in the UI"},
			"job_id":     map[string]interface{}{"type": "integer", "description": "cancel: the job id from transfer_jobs"}}),
		gate: gateHigh, run: toolTransferAction},
	{Name: "transfer_jobs", Description: "The transfer jobs: their kind (host to host, s3, repository), source, destination, status, progress and error. With job_id the status of one job.",
		InputSchema: schema(nil, map[string]interface{}{"job_id": map[string]interface{}{"type": "integer"}, "limit": map[string]interface{}{"type": "integer"}}), run: toolTransferJobs},

	{Name: "domain_config_remove", Description: "Delete one nginx config file of a host (the reverse-proxy vhost), validate nginx and reload it. Always asks; the operator rescans afterwards with inventory_scan domains.",
		InputSchema: schema([]string{"host_id", "path"}, map[string]interface{}{"host_id": pHost, "path": map[string]interface{}{"type": "string", "description": "the config file path, as listed by host_info domains"}}),
		gate:        gateHigh, run: toolDomainConfigRemove},
}

// ---------------------------------------------------------------- Docker

func toolDockerContainerAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	name, action := argStr(args, "container"), argStr(args, "action")
	if !inspector.ValidDockerName(name) {
		return "", fmt.Errorf("invalid container name")
	}
	opts := inspector.DockerContainerOpts{
		Force:   argBool(args, "force"),
		Volumes: argBool(args, "volumes"),
		NewName: argStr(args, "new_name"),
	}
	gate := gateRoutine
	switch action {
	case "start", "stop", "restart", "pause", "unpause":
	case "kill", "remove":
		gate = gateHigh
	case "rename":
		if !inspector.ValidDockerName(opts.NewName) {
			return "", fmt.Errorf("rename needs a valid new_name")
		}
	default:
		return "", fmt.Errorf("action must be start, stop, restart, pause, unpause, kill, rename or remove")
	}
	if err := s.agentGate(r, id, "docker_container_action", "docker "+action+" "+name, gate); err != nil {
		return "", err
	}
	out, err := s.inspector.DockerContainerOp(id, name, action, opts)
	if err != nil {
		return out, err
	}
	return "docker " + action + " " + name + ": " + clipTo(out, 500), nil
}

func toolDockerRun(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	spec := inspector.DockerRunSpec{
		Image:   argStr(args, "image"),
		Name:    argStr(args, "name"),
		Network: argStr(args, "network"),
		Restart: argStr(args, "restart"),
		Command: argStr(args, "command"),
		Memory:  argStr(args, "memory"),
		CPUs:    argStr(args, "cpus"),
	}
	spec.Ports = argStrList(args, "ports")
	spec.Env = argStrList(args, "env")
	spec.Volumes = argStrList(args, "volumes")
	if !inspector.ValidDockerImage(spec.Image) {
		return "", fmt.Errorf("image must be a valid image reference, see docker_info images")
	}
	label := ""
	if spec.Name != "" {
		label = " " + spec.Name
	}
	if err := s.agentGate(r, id, "docker_run", "docker run "+spec.Image+label, gateRoutine); err != nil {
		return "", err
	}
	cid, err := s.inspector.DockerRunContainer(id, spec)
	if err != nil {
		return "", err
	}
	return "container started: " + cid, nil
}

func toolDockerImageAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	action, image := argStr(args, "action"), argStr(args, "image")
	gate := gateRoutine
	switch action {
	case "pull":
		if !inspector.ValidDockerImage(image) {
			return "", fmt.Errorf("pull needs a valid image reference")
		}
	case "remove":
		gate = gateHigh
		if !inspector.ValidDockerImage(image) {
			return "", fmt.Errorf("remove needs a valid image reference")
		}
	case "prune", "prune_all":
		gate = gateHigh
	default:
		return "", fmt.Errorf("action must be pull, remove, prune or prune_all")
	}
	summary := "docker image " + action
	if image != "" {
		summary += " " + image
	}
	if err := s.agentGate(r, id, "docker_image_action", summary, gate); err != nil {
		return "", err
	}
	out, err := s.inspector.DockerImageOp(id, action, image, argBool(args, "force"), nil)
	if err != nil {
		return out, err
	}
	return clipTo(out, 2000), nil
}

func toolDockerVolumeAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	action, name := argStr(args, "action"), argStr(args, "name")
	gate := gateRoutine
	switch action {
	case "create":
		if !inspector.ValidDockerName(name) {
			return "", fmt.Errorf("create needs a valid volume name")
		}
	case "remove":
		gate = gateHigh
		if !inspector.ValidDockerName(name) {
			return "", fmt.Errorf("remove needs a valid volume name")
		}
	case "prune", "prune_all":
		gate = gateHigh
	default:
		return "", fmt.Errorf("action must be create, remove, prune or prune_all")
	}
	summary := "docker volume " + action
	if name != "" {
		summary += " " + name
	}
	if err := s.agentGate(r, id, "docker_volume_action", summary, gate); err != nil {
		return "", err
	}
	out, err := s.inspector.DockerVolumeOp(id, action, name)
	if err != nil {
		return out, err
	}
	return clipTo(out, 2000), nil
}

func toolDockerNetworkAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	action := argStr(args, "action")
	spec := inspector.DockerNetworkSpec{
		Name:     argStr(args, "name"),
		Driver:   argStr(args, "driver"),
		Subnet:   argStr(args, "subnet"),
		Gateway:  argStr(args, "gateway"),
		Internal: argBool(args, "internal"),
	}
	container := argStr(args, "container")
	gate := gateRoutine
	switch action {
	case "create":
		if !inspector.ValidDockerName(spec.Name) {
			return "", fmt.Errorf("a valid network name is required")
		}
	case "remove":
		gate = gateHigh
		if !inspector.ValidDockerName(spec.Name) {
			return "", fmt.Errorf("a valid network name is required")
		}
	case "connect", "disconnect":
		gate = gateHigh
		if !inspector.ValidDockerName(spec.Name) || !inspector.ValidDockerName(container) {
			return "", fmt.Errorf("a valid network name and container name are required")
		}
	case "prune":
		gate = gateHigh
	default:
		return "", fmt.Errorf("action must be create, remove, connect, disconnect or prune")
	}
	if err := s.agentGate(r, id, "docker_network_action", "docker network "+action+" "+spec.Name, gate); err != nil {
		return "", err
	}
	out, err := s.inspector.DockerNetworkOp(id, action, spec, container)
	if err != nil {
		return out, err
	}
	return clipTo(out, 2000), nil
}

func toolDockerPrune(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	target := argStr(args, "target")
	switch target {
	case "containers", "images", "images_all", "volumes", "networks", "builder":
	default:
		return "", fmt.Errorf("target must be containers, images, images_all, volumes, networks or builder")
	}
	if err := s.agentGate(r, id, "docker_prune", "docker prune "+target, gateHigh); err != nil {
		return "", err
	}
	out, err := s.inspector.DockerPrune(id, target)
	if err != nil {
		return out, err
	}
	return clipTo(out, 2000), nil
}

func toolDockerStackDeploy(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	name, yaml := argStr(args, "name"), argStr(args, "yaml")
	if err := s.agentGate(r, id, "docker_stack_deploy", "docker compose deploy of stack "+name, gateRoutine); err != nil {
		return "", err
	}
	out, err := s.inspector.DockerStackDeploy(id, name, yaml, nil)
	if err != nil {
		return out, err
	}
	return clipTo(out, 2000), nil
}

// ---------------------------------------------------------------- packages

// toolPkgAction runs the package change the Packages tab runs. The preview (which packages are pending) is
// read with host_info updates; the command itself is built by the inspector, so the privilege fallback
// (passwordless sudo) and the timeout are the UI's.
func toolPkgAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	action := argStr(args, "action")
	if action != "upgrade" && action != "install" && action != "remove" {
		return "", fmt.Errorf("action must be upgrade, install or remove")
	}
	preview, err := s.inspector.GetUpgradePreview(r.Context(), id)
	if err != nil {
		return "", err
	}
	if !preview.Supported {
		return "", fmt.Errorf("package changes are not supported on this host: %s", preview.Note)
	}
	var cmd, summary string
	if action == "upgrade" && len(argStrList(args, "packages")) == 0 {
		mode := argStr(args, "mode")
		if mode == "" {
			mode = "all"
		}
		if cmd, err = inspector.BuildUpgradeCommand(preview.Manager, mode); err != nil {
			return "", err
		}
		summary = "package upgrade (" + preview.Manager + ", " + mode + "): " + cmd
	} else {
		names, perr := inspector.ParsePackageNames(strings.Join(argStrList(args, "packages"), ","))
		if perr != nil {
			return "", perr
		}
		if cmd, err = inspector.BuildPackageCommand(preview.Manager, action, names); err != nil {
			return "", err
		}
		summary = "package " + action + " (" + preview.Manager + "): " + strings.Join(names, " ")
	}
	// The lock is taken after the approval, so a waiting card never blocks the operator's own upgrade.
	if err := s.agentGate(r, id, "pkg_action", summary, gateHigh); err != nil {
		return "", err
	}
	// One upgrade per host at a time: the inspector's lock is the UI's.
	unlock, err := inspector.TryLockUpgrade(id)
	if err != nil {
		return "", err
	}
	defer unlock()

	stdout, stderr, code, err := s.inspector.ExecShell(id, cmd, 15*60*1000*1000*1000)
	if err != nil {
		return "", err
	}
	out := clipTo(strings.TrimSpace(stdout), 4000)
	if strings.TrimSpace(stderr) != "" {
		out += "\n--- stderr ---\n" + clipTo(strings.TrimSpace(stderr), 2000)
	}
	if code != 0 {
		return out, fmt.Errorf("package %s failed (exit %d)", action, code)
	}
	return out, nil
}

// ---------------------------------------------------------------- files

func toolFileAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	action, path := argStr(args, "action"), argStr(args, "path")
	// The protected paths are refused before the operator is asked, so the card never shows an action
	// the File Manager would refuse anyway.
	if isProtectedPathString(path) {
		return "", fmt.Errorf("protected path: cannot modify system location")
	}
	switch action {
	case "write":
		content := argStr(args, "content")
		if content == "" {
			return "", fmt.Errorf("content is required to write a file")
		}
		if err := s.agentGate(r, id, "file_action", "write file "+path, gateHigh); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/write", id),
			map[string]string{"path": path, "content": content})
		if err != nil {
			return "", err
		}
		return fmt.Sprint(m["status"]), nil
	case "duplicate":
		newName := argStr(args, "new_name")
		if newName == "" {
			return "", fmt.Errorf("duplicate needs new_name")
		}
		if strings.ContainsAny(newName, "/\\") || newName == "." || newName == ".." {
			return "", fmt.Errorf("new_name must be a plain name, without a path")
		}
		if err := s.agentGate(r, id, "file_action", "duplicate "+path+" as "+newName, gateRoutine); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/duplicate", id),
			map[string]string{"path": path, "new_name": newName})
		if err != nil {
			return "", err
		}
		return fmt.Sprint(m["path"]), nil
	case "delete":
		if err := s.agentGate(r, id, "file_action", "delete "+path, gateHigh); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/delete", id),
			map[string]interface{}{"path": path, "recursive": argBool(args, "recursive")})
		if err != nil {
			return "", err
		}
		return fmt.Sprint(m["status"]), nil
	case "chmod":
		mode := argStr(args, "mode")
		if mode == "" {
			return "", fmt.Errorf("chmod needs mode, like 0644")
		}
		if err := s.agentGate(r, id, "file_action", "chmod "+mode+" "+path, gateHigh); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/chmod", id),
			map[string]string{"path": path, "mode": mode})
		if err != nil {
			return "", err
		}
		return fmt.Sprint(m["status"]), nil
	}
	return "", fmt.Errorf("action must be write, duplicate, delete or chmod")
}

func toolFileUploadText(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	path := argStr(args, "path")
	content := argStr(args, "content")
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	if isProtectedPathString(path) {
		return "", fmt.Errorf("protected path: cannot modify system location")
	}
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	if err := s.agentGate(r, id, "file_upload_text", "write file "+path, gateHigh); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/write", id),
		map[string]string{"path": path, "content": content})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(m["status"]), nil
}

// ---------------------------------------------------------------- transfers

func toolTransferAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	action := argStr(args, "action")
	switch action {
	case "transfer":
		destID := argInt(args, "dest_host_id")
		destPath := argStr(args, "dest_path")
		srcPath := argStr(args, "path")
		if destID < 1 || destPath == "" || srcPath == "" {
			return "", fmt.Errorf("transfer needs path, dest_host_id and dest_path")
		}
		mode := argStr(args, "mode")
		if mode == "" {
			mode = "fast"
		}
		if mode != "fast" && mode != "verified" {
			return "", fmt.Errorf("mode must be fast or verified")
		}
		summary := fmt.Sprintf("copy %s to host %d (%s)", srcPath, destID, destPath)
		if err := s.agentGate(r, id, "transfer_action", summary, gateHigh); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/transfers", map[string]interface{}{
			"source_host_id": id, "source_path": srcPath, "dest_host_id": destID, "dest_path": destPath, "mode": mode})
		if err != nil {
			return "", err
		}
		return jobText(m), nil
	case "s3":
		srcPath := argStr(args, "path")
		acctName := argStr(args, "s3_account")
		if srcPath == "" || acctName == "" {
			return "", fmt.Errorf("s3 needs path and s3_account (the name of an S3 account configured in the UI)")
		}
		acct, err := s.s3AccountByName(acctName)
		if err != nil {
			return "", err
		}
		summary := "upload " + srcPath + " to S3 account " + acct.Name
		if err := s.agentGate(r, id, "transfer_action", summary, gateHigh); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/transfers/s3", map[string]interface{}{
			"source_host_id": id, "source_path": srcPath, "account_id": acct.ID})
		if err != nil {
			return "", err
		}
		return jobText(m), nil
	case "cancel":
		jobID := argInt(args, "job_id")
		if jobID < 1 {
			return "", fmt.Errorf("cancel needs job_id")
		}
		if err := s.agentGate(r, id, "transfer_action", fmt.Sprintf("cancel transfer job #%d", jobID), gateHigh); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/transfers/%d/cancel", jobID), nil)
		if err != nil {
			return "", err
		}
		return fmt.Sprint(m["status"]), nil
	}
	return "", fmt.Errorf("action must be transfer, s3 or cancel")
}

func toolTransferJobs(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if jobID := argInt(args, "job_id"); jobID > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/transfers/%d", jobID), nil)
		if err != nil {
			return "", err
		}
		return jobText(m), nil
	}
	// The list is a JSON array, so loopJSON (objects only) does not fit.
	code, raw := s.loopback(r.Context(), http.MethodGet, "/api/transfers", nil)
	if code/100 != 2 {
		return "", fmt.Errorf("cannot list the transfer jobs (HTTP %d)", code)
	}
	var jobs []map[string]interface{}
	if err := json.Unmarshal(raw, &jobs); err != nil {
		return "", fmt.Errorf("unexpected answer from /api/transfers")
	}
	if len(jobs) > 20 {
		jobs = jobs[:20]
	}
	var b strings.Builder
	tsvLine(&b, "id", "kind", "status", "source", "destination", "progress", "error")
	for _, j := range jobs {
		tsvLine(&b, j["id"], j["kind"], j["status"], j["source_path"], j["dest_path"], j["percentage"], j["error"])
	}
	return b.String(), nil
}

// jobText renders one transfer job as one line.
func jobText(m map[string]interface{}) string {
	return fmt.Sprintf("job %v: %v %v -> %v (%v%%) %v", m["id"], m["kind"], m["source_path"], m["dest_path"], m["percentage"], m["status"])
}

// s3AccountByName finds an S3 account by the name the operator gave it. The secret key never leaves the store.
func (s *Server) s3AccountByName(name string) (*store.S3Account, error) {
	list, err := s.db.ListS3Accounts()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("no S3 account named %q; the operator must create it in the UI first", name)
}

// ---------------------------------------------------------------- domains

func toolDomainConfigRemove(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	path := argStr(args, "path")
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	if err := s.agentGate(r, id, "domain_config_remove", "delete nginx config "+path, gateHigh); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/domains/remove", map[string]interface{}{"host_id": id, "path": path})
	if err != nil {
		return "", err
	}
	return fmt.Sprint(m["status"]), nil
}

// ---------------------------------------------------------------- helpers

// argBool reads a boolean argument (false when absent).
func argBool(args map[string]interface{}, k string) bool {
	b, _ := args[k].(bool)
	return b
}

// argStrList reads an array of strings.
func argStrList(args map[string]interface{}, k string) []string {
	list, _ := args[k].([]interface{})
	out := make([]string, 0, len(list))
	for _, v := range list {
		s, _ := v.(string)
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
