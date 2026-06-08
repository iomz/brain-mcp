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
BRAIN_MCP_AUTH_MODE=mixed
BRAIN_MCP_TOKEN=replace-with-long-random-token
BRAIN_MCP_ADDR=127.0.0.1:8787
BRAIN_MCP_OAUTH_RESOURCE=https://brain.sazanka.io/mcp
BRAIN_MCP_OAUTH_ISSUER=https://auth.sazanka.io/application/o/brain-mcp/
BRAIN_MCP_OAUTH_JWKS_URL=https://auth.sazanka.io/application/o/brain-mcp/jwks/
BRAIN_MCP_OAUTH_CLIENT_ID=replace-with-authentik-client-id
BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES=https://brain.sazanka.io/mcp,replace-with-authentik-client-id
BRAIN_MCP_OAUTH_AUTHORIZATION_SERVERS=https://auth.sazanka.io/application/o/brain-mcp/
BRAIN_MCP_OAUTH_DCR_ENABLED=false
BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER_ISSUER=https://brain.sazanka.io
BRAIN_MCP_OAUTH_APPROVAL_TOKEN=replace-with-local-approval-token
BRAIN_MCP_OAUTH_SUBJECT=brain-mcp-user
BRAIN_MCP_OAUTH_EMAIL=you@example.com
BRAIN_MCP_OAUTH_STATE_FILE=.brain-mcp-oauth-state.json
BRAIN_MCP_OAUTH_AUTHENTIK_APPROVAL_ENABLED=false
BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_ID=replace-with-authentik-client-id
BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_SECRET=
# Authentik issuer/discovery URLs include the provider slug, but authorize/token
# endpoints commonly do not. Verify with the provider's OpenID discovery document.
BRAIN_MCP_OAUTH_AUTHENTIK_AUTHORIZE_URL=https://auth.sazanka.io/application/o/authorize/
BRAIN_MCP_OAUTH_AUTHENTIK_TOKEN_URL=https://auth.sazanka.io/application/o/token/
BRAIN_MCP_OAUTH_AUTHENTIK_REDIRECT_URI=https://brain.sazanka.io/oauth/authentik/callback
BRAIN_MCP_OAUTH_SCOPES=openid,email,profile,brain:read,brain:write,brain:git,brain:admin
BRAIN_MCP_OAUTH_DEFAULT_SCOPES=brain:read
BRAIN_MCP_ALLOWED_EMAILS=you@example.com
CLOUDFLARED_TUNNEL_TOKEN=replace-with-cloudflare-token
```

`brain-mcp` creates `.env` automatically when the file is missing. Environment variables override values in the file.

### Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `BRAIN_MCP_CONFIG_FILE` | `.env` | Config file path. `~` is expanded. |
| `BRAIN_ROOT` | none | Local Brain vault root. Required. |
| `BRAIN_ROOT_HOST` | none | Host vault path mounted into Docker as `/brain`. |
| `BRAIN_MCP_AUTH_MODE` | `bearer` locally, `mixed` in Compose | Auth mode: `bearer`, `oauth`, `mixed`, or `none`. |
| `BRAIN_MCP_TOKEN` | none | Static bearer token. Required for `bearer` and `mixed`. |
| `BRAIN_MCP_ADDR` | `127.0.0.1:8787` | Listen address. |
| `BRAIN_MCP_OAUTH_RESOURCE` | `https://brain.sazanka.io/mcp` | OAuth protected resource identifier. |
| `BRAIN_MCP_OAUTH_ISSUER` | none | OAuth/OIDC issuer URL. |
| `BRAIN_MCP_OAUTH_JWKS_URL` | none | JWKS URL for access-token verification. |
| `BRAIN_MCP_OAUTH_CLIENT_ID` | none | Authentik OAuth client ID; accepted as token `aud`. |
| `BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES` | OAuth resource | Comma-separated accepted token `aud` or `resource` values. |
| `BRAIN_MCP_OAUTH_AUTHORIZATION_SERVERS` | Authentik issuer | Comma-separated authorization server issuer URLs for protected-resource metadata. |
| `BRAIN_MCP_OAUTH_DCR_ENABLED` | `false` | Enables experimental ChatGPT DCR and local authorization-code endpoints. Registration/code storage is memory-only. |
| `BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER_ISSUER` | OAuth resource origin | Issuer used by experimental DCR authorization-server metadata. |
| `BRAIN_MCP_OAUTH_APPROVAL_TOKEN` | none | Local approval secret entered in the `/oauth/authorize` form. Required when DCR code flow is enabled. |
| `BRAIN_MCP_OAUTH_SUBJECT` | `brain-mcp-user` | Subject claim for locally issued DCR access tokens. |
| `BRAIN_MCP_OAUTH_EMAIL` | first allowed email | Email claim for locally issued DCR access tokens. |
| `BRAIN_MCP_OAUTH_STATE_FILE` | `.brain-mcp-oauth-state.json` | Stores local OAuth signing key and DCR clients. Treat as secret. Compose defaults to `/state/oauth-state.json` on a named volume. |
| `BRAIN_MCP_OAUTH_AUTHENTIK_APPROVAL_ENABLED` | `false` | Redirect `/oauth/authorize` approval to Authentik before issuing local resource tokens. |
| `BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_ID` | OAuth client ID | Authentik client used for approval login. Defaults to `BRAIN_MCP_OAUTH_CLIENT_ID`. |
| `BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_SECRET` | none | Optional Authentik client secret for code exchange. |
| `BRAIN_MCP_OAUTH_AUTHENTIK_AUTHORIZE_URL` | Authentik discovery `authorization_endpoint` | Authentik authorize endpoint. Usually `https://auth.example.com/application/o/authorize/`, without the provider slug. Verify via OpenID discovery. |
| `BRAIN_MCP_OAUTH_AUTHENTIK_TOKEN_URL` | Authentik discovery `token_endpoint` | Authentik token endpoint. Usually `https://auth.example.com/application/o/token/`, without the provider slug. Verify via OpenID discovery. |
| `BRAIN_MCP_OAUTH_AUTHENTIK_REDIRECT_URI` | local issuer callback | Redirect URI to allow in Authentik: `https://brain.sazanka.io/oauth/authentik/callback`. |
| `BRAIN_MCP_OAUTH_SCOPES` | `brain:read,brain:write,brain:git,brain:admin` | Scopes advertised to OAuth clients. Include `brain:*` scopes for tool authorization and OIDC scopes if Authentik needs them. |
| `BRAIN_MCP_OAUTH_DEFAULT_SCOPES` | `brain:read` | Internal fallback scopes when OAuth token has no `brain:*` scopes. |
| `BRAIN_MCP_ALLOWED_EMAILS` | none | Comma-separated OAuth email allowlist. At least one email, subject, or group allowlist is required for OAuth tokens. |
| `BRAIN_MCP_ALLOWED_SUBJECTS` | none | Comma-separated OAuth subject allowlist. |
| `BRAIN_MCP_ALLOWED_GROUPS` | none | Comma-separated OAuth group allowlist. |
| `BRAIN_MCP_WRITABLE_PATHS` | `Knowledge/,System/,Active/,Archive/,Journal/` | Comma-separated writable prefixes. |
| `BRAIN_MCP_READONLY_PATHS` | none | Comma-separated read-only prefixes. Put `Journal/` here only when journal editing should be blocked. |
| `BRAIN_MCP_REQUIRE_GIT` | `true` | Require `BRAIN_ROOT` to contain `.git`. |
| `CLOUDFLARED_TUNNEL_TOKEN` | none | Cloudflare tunnel token used by Compose. |

### Authentik Endpoint Discovery

Authentik has two URL shapes that are easy to confuse.

Provider-specific issuer, discovery, and JWKS URLs usually include the provider slug:

```text
https://auth.example.com/application/o/brain-mcp/
https://auth.example.com/application/o/brain-mcp/.well-known/openid-configuration
https://auth.example.com/application/o/brain-mcp/jwks/
```

The authorization and token endpoints commonly do **not** include the provider slug:

```text
https://auth.example.com/application/o/authorize/
https://auth.example.com/application/o/token/
```

Do not guess these URLs by string concatenation. Confirm them from Authentik's OpenID discovery document:

```sh
curl -s   https://auth.example.com/application/o/brain-mcp/.well-known/openid-configuration   | jq '.issuer, .authorization_endpoint, .token_endpoint, .jwks_uri'
```

Use the returned values directly.

A common wrong configuration is:

```text
https://auth.example.com/application/o/brain-mcp/authorize/
https://auth.example.com/application/o/brain-mcp/token/
```

If the ChatGPT connector redirects to Authentik and the browser shows only `Not Found`, check the resolved `authorization_endpoint` first.

### OAuth Configuration Model

The OAuth settings cover three related but distinct responsibilities:

1. **External token validation**
   - `BRAIN_MCP_OAUTH_ISSUER`
   - `BRAIN_MCP_OAUTH_JWKS_URL`
   - `BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES`
   - allowlist settings such as `BRAIN_MCP_ALLOWED_EMAILS`

2. **Brain MCP authorization-server facade for ChatGPT DCR**
   - `BRAIN_MCP_OAUTH_DCR_ENABLED`
   - `BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER_ISSUER`
   - `BRAIN_MCP_OAUTH_STATE_FILE`
   - `/oauth/register`, `/oauth/authorize`, `/oauth/token`, and local JWKS endpoints

3. **Optional Authentik-backed browser approval**
   - `BRAIN_MCP_OAUTH_AUTHENTIK_APPROVAL_ENABLED`
   - `BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_ID`
   - `BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_SECRET`
   - `BRAIN_MCP_OAUTH_AUTHENTIK_AUTHORIZE_URL`
   - `BRAIN_MCP_OAUTH_AUTHENTIK_TOKEN_URL`
   - `BRAIN_MCP_OAUTH_AUTHENTIK_REDIRECT_URI`

These settings should not be collapsed into one URL pattern. In particular, an Authentik provider issuer can contain `/brain-mcp/` while its authorize and token endpoints do not.

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

`brain-mcp` exposes both MCP resource endpoints and experimental OAuth/DCR endpoints.

The public MCP resource is `/mcp`. OAuth discovery starts from the protected-resource metadata endpoint, while the optional DCR authorization-server facade uses `/oauth/*` endpoints when enabled.

`GET /healthz` and `GET /info` require static bearer auth unless `BRAIN_MCP_AUTH_MODE=none`.

```text
Authorization: Bearer <BRAIN_MCP_TOKEN>
```

`POST /mcp` allows unauthenticated `initialize` and `tools/list` so ChatGPT can discover tool metadata. Tool calls require either a valid static bearer token or a valid OAuth access token. Missing or invalid OAuth returns `401` with a `WWW-Authenticate` challenge pointing to `/.well-known/oauth-protected-resource`.

Endpoints:

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/.well-known/oauth-protected-resource` | Returns OAuth protected-resource metadata for ChatGPT. |
| `GET` | `/.well-known/oauth-protected-resource/mcp` | Same metadata, path-specific variant. |
| `GET` | `/.well-known/oauth-authorization-server` | Returns experimental authorization-server metadata when `BRAIN_MCP_OAUTH_DCR_ENABLED=true`. |
| `GET` | `/.well-known/jwks.json` | Returns JWKS for locally issued DCR access tokens. |
| `POST` | `/oauth/register` | Accepts experimental ChatGPT DCR client registration when `BRAIN_MCP_OAUTH_DCR_ENABLED=true`. |
| `GET` | `/oauth/clients` | Lists DCR clients and token counts. Requires static bearer token. |
| `GET`, `POST` | `/oauth/authorize` | Runs private approval-token authorization-code flow with PKCE S256 and a 30-day approval cookie. |
| `GET` | `/oauth/authentik/callback` | Optional Authentik approval callback. |
| `POST` | `/oauth/token` | Exchanges one-time authorization codes for locally signed JWT access tokens. |
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

Tool descriptors include `inputSchema`, `outputSchema`, `structuredContent`, security schemes, and read-only annotations for ChatGPT integration.

| Tool | Arguments | Effect |
| --- | --- | --- |
| `brain_read_note` | `path` | Read one Markdown note. |
| `brain_list_notes` | `prefix` | List `.md` notes under a directory prefix. |
| `brain_search_notes` | `query`, optional `prefix`, `limit`, `include_snippets` | Search Markdown notes by path, title, headings, and body without returning full contents. |
| `brain_get_journal_config` | none | Return journal root and daily/monthly/yearly note patterns. |
| `brain_get_today_journal` | none | Resolve today's daily journal path and whether it exists. |
| `brain_find_recent_journals` | optional `limit` | List recent journal notes newest first. |
| `brain_show_diff` | `path`, `new_content` | Return unified diff without writing. |
| `brain_apply_patch` | `path`, `proposed_content` | Write complete proposed note content and return diff. |
| `brain_create_note` | `path`, `content` | Create a new `.md` note, including parent directories, without overwriting existing files. |
| `brain_append_section` | `path`, `heading`, `content` | Append content inside an existing exact heading section. |
| `brain_get_section` | `path`, `heading` | Read one exact heading section. |
| `brain_replace_section` | `path`, `heading`, `content` | Replace one exact heading section. |
| `brain_upsert_section` | `path`, `heading`, `content`, optional `parent_heading` | Replace an exact heading section, or insert it under a parent heading. |
| `brain_delete_duplicate_section` | `path`, `heading`, `keep` | Delete duplicate exact heading sections while keeping `first` or `last`. |
| `brain_replace_text` | `path`, `old_text`, `new_text` | Replace one exact text occurrence. |
| `brain_git_status` | none | Return `git status --short` for the vault. |
| `brain_git_diff` | none | Return `git diff --` for the vault. |
| `brain_git_commit` | `message` | Stage all vault changes and commit them. |

`brain_apply_patch` also accepts `content` for compatibility. `brain_write_note` is accepted as an alias for `brain_apply_patch`. `brain_create_note` writes UTF-8 Markdown content, ensures a trailing newline, and returns an error if the target file already exists.

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
