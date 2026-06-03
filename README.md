# brain-mcp

`brain-mcp` is a small Go HTTP service that exposes MCP-compatible tools for safe AI-assisted editing of a local Obsidian Brain vault.

It is not a generic file server. It only reads Markdown notes from configured prefixes, only writes Markdown notes to configured writable prefixes, blocks hidden/path-traversal/symlink escapes, and can require the vault itself to be a git repository.

## Features

- Bearer-authenticated HTTP endpoints for health, server info, and MCP JSON-RPC.
- MCP tools for reading notes, listing notes, previewing diffs, writing notes, editing Markdown sections, replacing exact text, and committing vault changes.
- Path policy controls for writable and read-only vault prefixes.
- Git status, diff, and commit tools for keeping AI edits reviewable.
- Docker Compose setup for local service plus Cloudflare Tunnel.
- `.env` config loading with environment variable override support.

## Repository Layout

```text
cmd/brain-mcp/        CLI entrypoint and HTTP listener setup
internal/brain/       Vault path policy, note I/O, and Markdown section editing
internal/config/      .env creation and config loading
internal/diff/        Unified diff generation
internal/git/         Git status, diff, and commit wrappers
internal/httpapi/     Authenticated HTTP routes
internal/mcp/         MCP JSON-RPC server and tool definitions
testdata/Brain/       Minimal test vault fixture
```

## Requirements

- Go 1.25 or newer.
- Git.
- A local Obsidian-style Markdown vault.
- Docker and Docker Compose, only if running the container stack.
- Cloudflare Tunnel token, only if exposing through Cloudflare.

## Configuration

Create `.env` from `.env.example`:

```sh
cp .env.example .env
```

Set values:

```sh
BRAIN_ROOT=/path/to/Brain
BRAIN_ROOT_HOST=/path/to/Brain
BRAIN_MCP_TOKEN=replace-with-long-random-token
BRAIN_MCP_ADDR=127.0.0.1:8787
CLOUDFLARED_TUNNEL_TOKEN=replace-with-cloudflare-token
```

`brain-mcp` creates `.env` automatically when the file is missing. Environment variables override values in the file.

### Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `BRAIN_MCP_CONFIG_FILE` | `.env` | Config file path. `~` is expanded. |
| `BRAIN_ROOT` | none | Local Brain vault root. Required. |
| `BRAIN_ROOT_HOST` | none | Host vault path mounted into Docker as `/brain`. |
| `BRAIN_MCP_TOKEN` | none | Bearer token required for HTTP mode. Required. |
| `BRAIN_MCP_ADDR` | `127.0.0.1:8787` | Listen address. |
| `BRAIN_MCP_WRITABLE_PATHS` | `Knowledge/,System/,Active/,Archive/` | Comma-separated writable prefixes. |
| `BRAIN_MCP_READONLY_PATHS` | `Journal/` | Comma-separated read-only prefixes. |
| `BRAIN_MCP_REQUIRE_GIT` | `true` | Require `BRAIN_ROOT` to contain `.git`. |
| `CLOUDFLARED_TUNNEL_TOKEN` | none | Cloudflare tunnel token used by Compose. |

## Run Locally

```sh
go run ./cmd/brain-mcp
```

Health check:

```sh
curl -H "Authorization: Bearer $BRAIN_MCP_TOKEN" \
  http://127.0.0.1:8787/healthz
```

Server info:

```sh
curl -H "Authorization: Bearer $BRAIN_MCP_TOKEN" \
  http://127.0.0.1:8787/info
```

## Build And Install

Build a local binary:

```sh
go build -o ./bin/brain-mcp ./cmd/brain-mcp
```

Install it into a directory on `PATH`:

```sh
install -m 0755 ./bin/brain-mcp /usr/local/bin/brain-mcp
```

Or install from this checkout with Go:

```sh
go install ./cmd/brain-mcp
```

Run installed binary:

```sh
brain-mcp
```

## HTTP API

All HTTP endpoints require:

```text
Authorization: Bearer <BRAIN_MCP_TOKEN>
```

Endpoints:

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/healthz` | Returns `{"ok":true}`. |
| `GET` | `/info` | Returns vault basename, path policy, and git status summary. Absolute vault path is not exposed. |
| `POST` | `/mcp` | Accepts one MCP JSON-RPC request body. |

Example MCP request:

```sh
curl -H "Authorization: Bearer $BRAIN_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_read_note","arguments":{"path":"Knowledge/Self.md"}}}' \
  http://127.0.0.1:8787/mcp
```

## MCP Tools

All file paths are relative to `BRAIN_ROOT`.

| Tool | Arguments | Effect |
| --- | --- | --- |
| `brain_read_note` | `path` | Read one Markdown note. |
| `brain_list_notes` | `prefix` | List `.md` notes under a directory prefix. |
| `brain_show_diff` | `path`, `new_content` | Return unified diff without writing. |
| `brain_apply_patch` | `path`, `proposed_content` | Write complete proposed note content and return diff. |
| `brain_append_section` | `path`, `heading`, `content` | Append content inside an existing exact heading section. |
| `brain_get_section` | `path`, `heading` | Read one exact heading section. |
| `brain_replace_section` | `path`, `heading`, `content` | Replace one exact heading section. |
| `brain_upsert_section` | `path`, `heading`, `content`, optional `parent_heading` | Replace an exact heading section, or insert it under a parent heading. |
| `brain_delete_duplicate_section` | `path`, `heading`, `keep` | Delete duplicate exact heading sections while keeping `first` or `last`. |
| `brain_replace_text` | `path`, `old_text`, `new_text` | Replace one exact text occurrence. |
| `brain_git_status` | none | Return `git status --short` for the vault. |
| `brain_git_diff` | none | Return `git diff --` for the vault. |
| `brain_git_commit` | `message` | Stage all vault changes and commit them. |

`brain_apply_patch` also accepts `content` for compatibility. `brain_write_note` is accepted as an alias for `brain_apply_patch`.

### Section Editing Examples

Get a section:

```sh
curl -H "Authorization: Bearer $BRAIN_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_get_section","arguments":{"path":"Knowledge/Self.md","heading":"## Git Commit Preferences"}}}' \
  http://127.0.0.1:8787/mcp
```

Upsert a section under a parent heading:

```sh
curl -H "Authorization: Bearer $BRAIN_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"brain_upsert_section","arguments":{"path":"Knowledge/Self.md","parent_heading":"# Software and Development Preferences","heading":"## Git Commit Preferences","content":"- Prefer small, focused commits.\n- Inspect diffs before committing."}}}' \
  http://127.0.0.1:8787/mcp
```

Replace one exact text occurrence:

```sh
curl -H "Authorization: Bearer $BRAIN_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"brain_replace_text","arguments":{"path":"Knowledge/Self.md","old_text":"Old sentence.","new_text":"New sentence."}}}' \
  http://127.0.0.1:8787/mcp
```

Commit vault changes:

```sh
curl -H "Authorization: Bearer $BRAIN_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"brain_git_commit","arguments":{"message":"docs(self): update preference notes"}}}' \
  http://127.0.0.1:8787/mcp
```

## Safety Model

Path handling blocks:

- Absolute client paths.
- `..` traversal.
- Hidden path segments.
- `.git/` access through hidden-path blocking.
- Symlink escapes outside `BRAIN_ROOT`.
- Paths outside configured readable prefixes.
- Writes outside configured writable prefixes.
- Writes to configured read-only prefixes.
- Writes to non-`.md` files.

Git safety:

- `BRAIN_MCP_REQUIRE_GIT=true` requires the vault root to contain `.git`.
- `brain_git_commit` rejects empty commit messages and clean working trees.
- Write tools return diffs so clients can surface changes before or after writes.

Network safety:

- Default bind address is `127.0.0.1:8787`.
- Keep bearer auth enabled even when using Cloudflare Access.
- Do not bind to `0.0.0.0` outside a container or protected network unless you have an explicit reason.

## Docker Compose

Compose runs two services:

- `brain-mcp`: builds this repo, listens on `0.0.0.0:8787` inside the Compose network, and mounts `${BRAIN_ROOT_HOST}` at `/brain`.
- `cloudflared`: runs the Cloudflare tunnel and routes to `brain-mcp`.

Set `.env`:

```sh
BRAIN_ROOT_HOST=/path/to/Brain
BRAIN_MCP_TOKEN=replace-with-long-random-token
CLOUDFLARED_TUNNEL_TOKEN=replace-with-cloudflare-token
```

Start:

```sh
docker compose up -d --build
```

Check logs:

```sh
docker compose logs -f brain-mcp
```

Cloudflare tunnel service URL:

```text
http://brain-mcp:8787
```

If your public hostname points to `http://127.0.0.1:8787` or `http://localhost:8787`, update it to `http://brain-mcp:8787` for Docker Compose routing.

## Development

Run tests:

```sh
go test ./...
```

Format code:

```sh
gofmt -w cmd internal
```

Build:

```sh
go build ./cmd/brain-mcp
```

## Limitations

- No semantic search.
- No Obsidian graph parsing.
- No NotebookLM integration.
- No broad file rename/delete operations.
- HTTP mode handles one JSON-RPC request per `POST /mcp` body.
