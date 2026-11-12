#!/bin/sh
# Boots Hermes for the Pantau compose stack: writes config.yaml so Hermes talks to Pantau's MCP server,
# then runs the messaging gateway, which exposes the OpenAI-compatible API server on :8642.
set -eu
[ "${#API_SERVER_KEY}" -ge 16 ] || { echo "HERMES_API_KEY must be set to 16+ characters in .env (generate: openssl rand -hex 16)" >&2; exit 1; }
[ "${#PANTAU_MCP_TOKEN}" -ge 16 ] || { echo "PANTAU_AGENT_TOKEN must be set to 16+ characters in .env (generate: openssl rand -hex 16)" >&2; exit 1; }
H="${HERMES_HOME:-/home/agent/.hermes}"
mkdir -p "$H"
# LLM that does the thinking. Anthropic (default) reads ANTHROPIC_API_KEY. For any OpenAI-compatible endpoint
# (LiteLLM, vLLM, Ollama, a gateway) set HERMES_LLM_BASE_URL: provider becomes "custom" and HERMES_LLM_API_KEY is the key.
# The key stays a ${VAR} reference that Hermes expands itself, so it is never written to disk.
if [ -n "${HERMES_LLM_BASE_URL:-}" ]; then
  cat > "$H/config.yaml" <<YAML
model:
  provider: "custom"
  default: "${HERMES_LLM_MODEL:?set HERMES_LLM_MODEL when HERMES_LLM_BASE_URL is set}"
  base_url: "${HERMES_LLM_BASE_URL}"
  api_key: "\${HERMES_LLM_API_KEY}"
YAML
else
  cat > "$H/config.yaml" <<YAML
model:
  provider: "${HERMES_LLM_PROVIDER:-anthropic}"
  default: "${HERMES_LLM_MODEL:-claude-sonnet-4-6}"
YAML
fi
# ${...} stays literal here: Hermes expands it from its own environment, so the token never lands in a file.
cat >> "$H/config.yaml" <<'YAML'
mcp_servers:
  pantau:
    url: "${PANTAU_MCP_URL}"
    headers:
      Authorization: "Bearer ${PANTAU_MCP_TOKEN}"
    timeout: 300
    connect_timeout: 30
YAML
exec sh /home/agent/.local/bin/hermes-start.sh gateway run
