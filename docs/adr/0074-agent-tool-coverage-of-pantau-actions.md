# 0074. The agent covers the actions of Pantau, except the ones that need a secret

Extends the tool set of [ADR-0066](0066-hermes-ai-agent-and-mcp-tools.md) (run by the in-process agent of
[ADR-0073](0073-in-process-ai-agent.md)) so that the operator can ask the agent to do what the UI can do, not only to
read and to repair a host.

## Context

The agent could inspect and repair one host (systemd, Docker start/stop/restart, Kubernetes, files, firewall, Cloudflare,
repositories, security scans) but not create a container, upgrade a package, add a host, change a Desired State rule,
run a Terraform plan or an Ansible playbook, or manage the configuration around them. An operator who wanted any of
those had to leave the chat for the UI, so "the agent can do everything Pantau can" was false.

## Decision

1. **Three new tool files, one per area, each with its own SOUL section.**
   - `agent_tools_hostops.go`: Docker container, image, volume, network, prune and stack actions, `docker_run`,
     `pkg_action`, `file_action`, `file_upload_text`, `transfer_action` and `transfer_jobs`, `domain_config_remove`.
   - `agent_tools_fleet.go`: hosts, host groups, host import, Desired State rules, alerts, notes, terminal presets,
     shortcuts, port forwards, the SSH Key Library, the registries, general settings, S3 accounts, notification
     channels and the System Snapshot.
   - `agent_tools_iac.go`: Ansible playbooks, schedules and resources, Terraform workspaces, runs, data and catalog,
     Kubernetes clusters, repository management and Docker Hub / release information.
2. **The tools call the UI's own HTTP handlers in-process** (loopback), so validation, cooldowns, secret masking and
   audit entries are the UI's, and a rule changed in a handler cannot be bypassed by the agent. Where a tool wraps a
   host operation (Docker, packages, files) it uses the same runner as the existing host tools.
3. **Every new change goes through the existing gates** (routine, destructive on one host, Pantau-wide). Anything that
   removes data, changes real infrastructure or touches more than one host is gate 3: it asks every time and a session
   grant never covers it.
4. **A credential never crosses the model.** Tools take the id or name of something the operator already stored (an SSH
   Key Library entry, an S3 account) and never a key, token, passphrase or kubeconfig. Reads mask secret-looking values;
   sensitive Terraform variables are never returned.
5. **Left to the operator on purpose** (no tool, and the SOUL says so): restoring a System Snapshot, creating an S3
   account, a Cloudflare token or a Kubernetes cluster (each needs a secret), uploading or reading a private SSH key,
   and any authentication, 2FA, agent, update or secret setting.

## Consequences

- The tool list is much longer, and so is the specification sent with every request; each tool groups related actions
  behind an `action` argument to keep the count down, and the compact results of ADR-0066 still apply.
- A mistake by the model can now delete a host, a workspace or a playbook. The approval card names what is removed;
  nothing in the agent path can skip it.
- A new UI action is not covered by the agent until a tool is added: the coverage is a list in the SOUL, not a
  guarantee.
