# Brain MCP Server Plan

Status: initial HTTP MCP milestone

## Goal

Build a local MCP-compatible service that lets AI clients safely read, edit, diff, and commit Markdown notes in a local Obsidian Brain vault.

The service is a controlled Brain vault editor, not a generic file server.

## Current Architecture

```text
AI client
-> MCP JSON-RPC over HTTP
-> brain-mcp bearer auth
-> path policy and symlink checks
-> BRAIN_ROOT Markdown files
-> git status / diff / commit
```

Optional production exposure:

```text
AI client
-> Cloudflare Access
-> Cloudflare Tunnel
-> Docker Compose brain-mcp service
-> mounted Brain vault
```

## Implemented

- HTTP service with `GET /healthz`, `GET /info`, and `POST /mcp`.
- Bearer token authentication on every endpoint.
- `.env` config file creation and loading, with environment variable overrides.
- Vault path policy with writable and read-only prefixes.
- Blocking for absolute paths, `..`, hidden path segments, non-allowed prefixes, read-only writes, non-Markdown writes, and symlink escapes.
- Optional requirement that `BRAIN_ROOT` contains `.git`.
- MCP initialize, tool listing, and tool calls.
- Whole-note read, list, diff, and write operations.
- Heading-scoped Markdown section read, append, replace, upsert, duplicate cleanup, and exact text replacement.
- Git status, diff, and commit tools.
- Dockerfile and Docker Compose setup for service plus Cloudflare Tunnel.
- Unit tests for config, path policy, notes, sections, diff, git helpers, and MCP behavior.

## Configuration

Environment variables:

- `BRAIN_MCP_CONFIG_FILE`: config file path, default `.env`.
- `BRAIN_ROOT`: absolute path to the local Brain vault.
- `BRAIN_ROOT_HOST`: host Brain vault path mounted into Docker as `/brain`.
- `BRAIN_MCP_TOKEN`: bearer token required for HTTP requests.
- `BRAIN_MCP_ADDR`: listen address, default `127.0.0.1:8787`.
- `BRAIN_MCP_WRITABLE_PATHS`: comma-separated writable prefixes.
- `BRAIN_MCP_READONLY_PATHS`: comma-separated read-only prefixes.
- `BRAIN_MCP_REQUIRE_GIT`: require vault to be git repo, default `true`.
- `CLOUDFLARED_TUNNEL_TOKEN`: Cloudflare tunnel token used by Docker Compose.

Default writable prefixes:

- `Knowledge/`
- `System/`
- `Active/`
- `Archive/`

Default read-only prefixes:

- `Journal/`

## MCP Tools

Implemented:

- `brain_read_note`
- `brain_list_notes`
- `brain_show_diff`
- `brain_apply_patch`
- `brain_write_note` alias
- `brain_append_section`
- `brain_get_section`
- `brain_replace_section`
- `brain_upsert_section`
- `brain_delete_duplicate_section`
- `brain_replace_text`
- `brain_git_status`
- `brain_git_diff`
- `brain_git_commit`

Planned later:

- `brain_search_notes`
- `brain_propose_patch`
- `brain_move_note`
- `brain_delete_note` with explicit safeguards
- `brain_note_backlinks`
- `brain_recent_notes`

## Safe Write Workflow

Recommended client workflow:

1. Read note.
2. Prepare proposed full-note or section change.
3. Show diff when user review is needed.
4. Apply only after explicit approval for user-visible changes.
5. Check git status and diff.
6. Commit if requested.

Write operations return unified diffs and log changed paths without exposing absolute vault paths through API responses.

## Cloudflare Tunnel

Quick local tunnel:

```sh
cloudflared tunnel --url http://127.0.0.1:8787
```

Docker Compose service target:

```text
http://brain-mcp:8787
```

Production hostname target:

```text
your protected Cloudflare Access hostname
```

Cloudflare Access should restrict access to the owner email. Brain MCP should still require bearer auth as inner defense.

## Non-Goals For Current Milestone

- Generic file server behavior.
- Broad file delete/rename operations.
- Semantic search.
- Obsidian graph parsing.
- NotebookLM integration.
- Automatic classification.
- Auto-commit without explicit tool call.
