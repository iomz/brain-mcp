# OAuth design for ChatGPT Custom MCP

## 1. Goal

Enable ChatGPT Custom MCP to connect to `brain-mcp` at `https://brain.sazanka.io/mcp` using OAuth-compatible authentication.

`brain-mcp` should remain a controlled Brain vault interface for AI-assisted Markdown workflows. OAuth is only the gate that lets ChatGPT safely access that interface.

## 2. Non-goals

- Do not build a general multi-user SaaS.
- Do not expose unauthenticated write tools.
- Do not remove existing Bearer token support yet.
- Do not require OpenAI Responses API.
- Do not depend on manual curl workflows.
- Do not add arbitrary shell execution.
- Do not turn `brain-mcp` into a generic file server.

## 3. Current architecture

```text
ChatGPT
-> brain.sazanka.io
-> Cloudflare Tunnel
-> local brain-mcp
-> $BRAIN_ROOT
-> Obsidian Brain Vault
-> git
```

Current server shape:

- Go HTTP service.
- `GET /healthz`, `GET /info`, `POST /mcp`.
- Bearer token middleware protects all routes.
- MCP JSON-RPC handled by `internal/mcp`.
- Vault access controlled by `internal/brain` path policy.
- Git operations controlled by `internal/git`.
- Cloudflare Tunnel exposes Docker Compose service.

## Infrastructure Boundary

Authentik belongs to shared `sazanka.io` identity infrastructure, not the `brain-mcp` repository.

Boundaries:

- Authentik belongs to shared infrastructure.
- `brain-mcp` belongs to the Brain vault interface.
- Cloudflare Tunnel routing belongs to infrastructure.
- Brain vault source-of-truth decision remains separate.

Infrastructure plan:

- Run Authentik on the always-on home server.
- Keep Authentik config/deployment in a separate infrastructure repository or directory, for example `home-infra`.
- Expose `auth.sazanka.io` through Cloudflare Tunnel.
- Prefer a single `cloudflared` tunnel with multiple public hostnames/ingress rules if possible.
- Do not add Authentik services directly to `brain-mcp` Docker Compose.

Current transitional architecture:

- `auth.sazanka.io` runs on the home server.
- `brain.sazanka.io` may continue pointing to `brain-mcp` running on the Mac for now.
- This is acceptable because the Brain vault currently lives on the Mac and remains the source of truth.

## Brain Vault Hosting Question

Open question: should `brain-mcp` eventually move to the always-on home server?

Mac-hosted `brain-mcp`:

- Pros: directly accesses the Obsidian vault working copy.
- Pros: simplest source-of-truth model today.
- Cons: unavailable when Mac sleeps or is offline.
- Cons: not ideal for remote use.

Home-server-hosted `brain-mcp`:

- Pros: always accessible.
- Pros: better for ChatGPT connector and remote use.
- Cons: requires Brain vault sync strategy.
- Cons: possible git conflicts with Obsidian on Mac.
- Cons: must decide whether server has working tree, bare repo, or synced clone.

For now:

- Keep `brain-mcp` where the current Brain source of truth lives.
- Move only Authentik to always-on infrastructure.
- Revisit Brain hosting after OAuth connector works.

Current implemented tools:

- `brain_read_note`
- `brain_list_notes`
- `brain_show_diff`
- `brain_apply_patch`
- `brain_write_note` alias
- `brain_get_section`
- `brain_append_section`
- `brain_replace_section`
- `brain_upsert_section`
- `brain_delete_duplicate_section`
- `brain_replace_text`
- `brain_git_status`
- `brain_git_diff`
- `brain_git_commit`

## Immediate Project Priority

Next milestone is not advanced memory modeling.

Next milestone is making ChatGPT Custom MCP connect successfully to:

```text
https://brain.sazanka.io/mcp
```

OAuth and ChatGPT connector compatibility are the blocking path. Finish plumbing first:

- OAuth protected-resource metadata.
- `WWW-Authenticate` challenge.
- External Authorization Server integration.
- JWT/JWKS validation.
- Tool-level scope enforcement.
- ChatGPT Custom MCP connection test.

Memory modeling should remain documented as future architecture, not implementation scope for this phase. Current generic Markdown tools are enough for initial ChatGPT integration.

## Minimal Connector Success Criteria

Smallest success state:

- ChatGPT Custom MCP successfully connects to `brain-mcp` using OAuth.
- ChatGPT can list tools.
- ChatGPT can read `Knowledge/Self.md`.
- ChatGPT can read one section from `Self.md`.
- ChatGPT can propose a small `Self.md` edit through existing tools.
- User can review diff.
- ChatGPT can call git status/diff.
- Git commit is available but should be used conservatively.

## Current ChatGPT Compatibility Decision

Manual user-defined OAuth client testing appears unreliable for ChatGPT Custom MCP in this setup. Authentik receives the authorization and token requests, but ChatGPT does not attach `Authorization: Bearer` to later `/mcp` requests, leaving `brain-mcp` with `missing_authorization_header`.

Prioritize automatic client registration/discovery for ChatGPT compatibility:

- DCR first: advertise `registration_endpoint` and accept ChatGPT dynamic client registration.
- CIMD later: only advertise `client_id_metadata_document_supported` after implementing and testing that path.
- Keep Authentik as preferred identity provider for production login/session work.
- Do not treat current DCR proof as a production authorization server.

Feasibility result:

- MCP/OpenAI Apps auth expects Protected Resource Metadata, Authorization Server Metadata, HTTP `401` with `WWW-Authenticate`, token verification, and either CIMD, DCR, predefined clients, or PKCE-capable OAuth flow.
- Superlist reference behavior:
  - `/mcp` returns `401` with `WWW-Authenticate: Bearer resource_metadata="https://app.superlist.com/.well-known/oauth-protected-resource"`.
  - Protected Resource Metadata returns `resource`, `authorization_servers`, and `bearer_methods_supported`.
  - Authorization Server Metadata advertises DCR through `registration_endpoint`, PKCE `S256`, `authorization_code`, `refresh_token`, and token auth methods `none`, `client_secret_basic`, and `client_secret_post`.
  - No CIMD support is advertised.
- Authentik supports OAuth2/OIDC, authorization code, PKCE, scopes, JWKS, redirect URI validation, and public-client style flows, but DCR support is still tracked upstream rather than generally available.
- Authentik does not appear to support CIMD.
- Therefore Authentik-native automatic client registration is not currently the lowest-risk path.
- A `brain-mcp` DCR compatibility layer is feasible only as an authorization-server facade. Registration alone is easy; completing the flow requires `/oauth/authorize`, `/oauth/token`, PKCE code binding, one-time code storage, token issuance, JWKS, and user approval/login integration.

Implemented proof:

- `GET /.well-known/oauth-authorization-server`, gated by `BRAIN_MCP_OAUTH_DCR_ENABLED=true`.
- `GET /.well-known/jwks.json`, gated by `BRAIN_MCP_OAUTH_DCR_ENABLED=true`.
- `POST /oauth/register`, gated by `BRAIN_MCP_OAUTH_DCR_ENABLED=true`.
- `GET/POST /oauth/authorize`, gated by `BRAIN_MCP_OAUTH_DCR_ENABLED=true`.
- `POST /oauth/token`, gated by `BRAIN_MCP_OAUTH_DCR_ENABLED=true`.
- Missing-auth challenges now match the Superlist-style lighter header: `Bearer resource_metadata="..."`.
- DCR validates HTTPS ChatGPT redirect URIs, authorization-code grant, code response type, and public-client `token_endpoint_auth_method=none`.
- Unlike Superlist, `brain-mcp` only advertises `token_endpoint_auth_methods_supported=["none"]` because it does not issue client secrets.
- Authorization-code flow requires `resource=https://brain.sazanka.io/mcp`, registered `redirect_uri`, registered `client_id`, and PKCE `S256`.
- Authorization approval uses either Authentik approval login or `BRAIN_MCP_OAUTH_APPROVAL_TOKEN` fallback, then sets a 30-day HttpOnly approval cookie for the browser.
- Token exchange consumes one-time short-lived authorization codes and issues locally signed RS256 JWT access tokens plus rotating refresh tokens.
- Registered clients, refresh tokens, and the local RS256 signing key are persisted in `BRAIN_MCP_OAUTH_STATE_FILE`; Compose stores this in the `brain-mcp-oauth-state` named volume.
- Authorization codes and browser approval sessions remain memory-only.
- `/oauth/clients` exposes registered client summaries and token/session counts for debugging. It is gated by static bearer auth and does not return token secrets.
- MCP tool descriptors now include `outputSchema`, `structuredContent`, read-only annotations, and invocation metadata to improve ChatGPT action exposure.

Next implementation step:

- Test this DCR/code-flow path with ChatGPT Custom MCP.
- Add refresh tokens only if ChatGPT requires reconnect stability. Superlist advertises `refresh_token`, but the current private proof keeps access tokens short-lived and avoids refresh-token storage.
- Replace approval-token form with Authentik login if this path graduates beyond private proof-of-concept use.

## Do Not Implement Yet

- Do not implement full built-in OAuth Authorization Server for production.
- Do not implement memory promotion tools.
- Do not implement semantic memory item storage.
- Do not implement note search unless needed for connector smoke test.
- Do not implement push/sync.
- Do not implement public read-only sharing mode.
- Do not implement broad delete/move operations.

## Git Commit Safety

Current `brain_git_commit(message)` is intentionally available, but it is too direct for frequent AI-assisted use.

Preferred long-term workflow:

1. AI calls `brain_git_status`.
2. AI calls `brain_git_diff`.
3. AI proposes a Conventional Commit message.
4. User reviews the diff and proposed message.
5. Only after explicit user approval, AI calls `brain_git_commit`.

Do not remove `brain_git_commit`. Do not block current manual testing. ChatGPT should not commit automatically.

Future optional tool:

- `brain_git_suggest_commit`

This tool would inspect git diff and return:

- Suggested Conventional Commit message.
- Short summary.
- Changed files.
- Risk level.
- Whether commit is safe to perform.

For now, implement no new commit tool unless explicitly requested. Immediate OAuth/ChatGPT connector work should preserve `brain_git_commit`, while docs and tool descriptions should make clear that commit requires explicit user approval.

## Future Memory Architecture

Future-facing architecture: Brain notes should separate observations, durable self-knowledge, operating procedure, and external knowledge. OAuth design should protect this memory architecture, not dictate it.

Recommended roles:

- `Journal`: primary observations. Daily/episodic record. High volume, time-indexed, noisy, and mostly read-only for MCP. Journal is source material, not final memory.
- `Self`: durable facts, observed patterns, interpretations, and open hypotheses about owner preferences, behavior, goals, constraints, and operating style. Lower volume, curated, and updated through diff-oriented workflows.
- `Knowledge`: externalized understanding about projects, technologies, domains, people, and decisions that are not primarily identity/self-model facts.
- `System`: operating procedures, tool policies, workflows, prompts, standards, and process notes that guide how Brain work should happen.

Preferred workflow:

1. Journal entries capture primary observations.
2. MCP tools read Journal entries as evidence.
3. Durable facts and repeated patterns are extracted into `Self.md`.
4. System notes describe repeatable operating procedures for extraction, review, git commits, and safety.
5. Knowledge notes hold externalized understanding and project/domain context.
6. Git diff/commit preserves reviewable memory changes.

Important boundary:

- Journal is evidence.
- Self is curated model.
- System is procedure.
- Knowledge is external understanding.

This boundary matters for auth. A future read-only sharing mode can expose selected Knowledge or System notes without exposing private Journal/Self data.

## Future Knowledge Modeling

Future-facing architecture: `Self.md` can evolve from raw Markdown into a structured self-knowledge model with semantic layers:

- Facts: stable claims directly supported by evidence. Example: preferred commit format, known timezone, recurring constraints.
- Observed Patterns: repeated behaviors or preferences seen across multiple observations. Example: prefers focused commits after reviewing diffs.
- Interpretations: model-generated explanations that connect patterns to motivations or tradeoffs. These should be labeled as interpretations, not facts.
- Open Hypotheses: tentative claims needing more evidence. These should carry uncertainty and review status.

Future `Self.md` structure can use Markdown sections, then later typed blocks:

```markdown
## Facts

- [fact] Prefer Conventional Commits for this repository.

## Observed Patterns

- [pattern] Often wants architecture settled before code.

## Interpretations

- [interpretation] Values correctness of information architecture over fast OAuth implementation.

## Open Hypotheses

- [hypothesis] May prefer local-first identity infrastructure over hosted identity providers.
```

Future MCP tools could operate on semantic layers instead of raw Markdown:

- `brain_list_memory_items(layer, prefix)`
- `brain_get_memory_item(id)`
- `brain_extract_self_candidates(source_path, date_range)`
- `brain_promote_observation_to_fact(source, claim, evidence)`
- `brain_record_pattern(claim, evidence_refs)`
- `brain_update_hypothesis(id, status, evidence)`
- `brain_link_evidence(memory_id, journal_path, quote_hash)`
- `brain_review_memory_diff(path)`

Tool behavior should preserve reviewability:

- Extraction tools propose candidates.
- Promotion tools require explicit layer, evidence, and diff.
- Facts require evidence links.
- Interpretations and hypotheses must be labeled.
- No tool should silently convert Journal observations into permanent Self facts.

This model gives future ChatGPT workflows a safer memory surface. ChatGPT can ask for "current durable facts" or "open hypotheses" without scanning all raw notes or treating every sentence as equally true.

## Deferred Memory Architecture Work

Semantic memory model is important. It should not block OAuth/ChatGPT connection.

Current generic Markdown tools are good enough for initial ChatGPT integration:

- `brain_read_note`
- `brain_get_section`
- `brain_replace_section`
- `brain_upsert_section`
- `brain_replace_text`
- `brain_show_diff`
- `brain_git_status`
- `brain_git_diff`

Once ChatGPT can call `brain-mcp`, real usage will clarify which semantic tools are actually needed.

Deferred post-connector work:

- Memory Promotion Pipeline.
- Self Semantic Layers.
- Candidate Workflow.
- Commit Suggestion.
- Journal to Self extraction.
- Facts / Observed Patterns / Interpretations / Open Hypotheses tooling.

## 4. Threat model

### Private note disclosure

Risk: unauthenticated or wrongly authenticated callers read private Brain notes.

Controls:

- Require OAuth for `/mcp` in production.
- Verify access token signature on every request.
- Verify issuer, audience/resource, expiry, and scopes.
- Preserve path allowlist and hidden path checks.
- Do not expose absolute vault paths.

### Unauthorized writes

Risk: attacker or low-scope client edits notes.

Controls:

- Require `brain:write` for all write tools.
- Require `brain:git` for commit tool.
- Keep path policy under `$BRAIN_ROOT`.
- Return diffs for writes.
- Keep write prefixes narrow.
- Reject read-only prefixes.

### Prompt injection from note contents

Risk: note text instructs ChatGPT to exfiltrate data, ignore user intent, or call destructive tools.

Controls:

- Treat note contents as untrusted data.
- Use tool descriptions that warn clients note contents are data, not instructions.
- Keep tool permissions minimal via scopes.
- Prefer user-visible diff-oriented workflows before writes and commits.
- Keep tool outputs scoped to requested note/section.

### Tool abuse through ChatGPT

Risk: valid ChatGPT session issues excessive, broad, or destructive tool calls.

Controls:

- Enforce scopes inside tool dispatch, not only route middleware.
- Rate-limit mutating requests later if abuse appears.
- Keep destructive operations absent or explicitly safeguarded.
- Require exact paths/headings/text for edits.
- Keep `brain_git_commit` separate from write tools.

### Leaked tokens

Risk: access token, refresh token, or existing static bearer token leaks.

Controls:

- Short-lived OAuth access tokens.
- Let external Authorization Server store refresh tokens.
- Validate signed access tokens with JWKS.
- Use external Authorization Server revocation/session policy.
- Keep `.env` out of git.
- Never log token values.
- Keep old bearer token separate from OAuth tokens.

### Cloudflare Tunnel exposure

Risk: tunnel makes local service reachable from internet.

Controls:

- Bind container service only inside Compose network.
- Require OAuth on public `/mcp`.
- Consider Cloudflare Access only after OAuth flow works.
- Keep MCP metadata HTTPS-only through `brain.sazanka.io`.
- Keep Authorization Server endpoints HTTPS-only through `auth.sazanka.io` or chosen issuer.

### Accidental destructive edits

Risk: valid user asks vague request and model edits wrong file.

Controls:

- Preserve diff-returning write tools.
- Keep `brain_git_diff` and `brain_git_status`.
- Keep commits explicit.
- Add future `brain_propose_patch` if stronger review workflow needed.
- Do not add broad delete/move tools without safeguards.

### OAuth implementation mistakes

Risk: accepting forged tokens, skipping PKCE, redirect abuse, code replay, wrong audience, or overbroad scopes.

Controls:

- Use OAuth 2.1 authorization code flow with PKCE S256.
- Delegate authorization codes, redirect validation, login, consent, refresh, and client registration to external Authorization Server.
- Echo and bind `resource` into token audience.
- Validate token issuer, audience/resource, expiry, subject, and scopes in `brain-mcp`.
- Add tests for negative paths.

### Future read-only sharing mode

Risk: no-auth read mode accidentally exposes private notes or write tools.

Controls:

- Default production mode requires OAuth for all tools.
- Mixed mode must be explicit.
- No-auth mode must never expose write or git tools.
- Public/read-only prefixes should be separate from private Brain prefixes.

## 5. Desired auth architecture

Critical recommendation: `brain-mcp` should not implement its own full OAuth Authorization Server for production.

Recommended approach:

- `brain-mcp` acts as OAuth protected resource server for `/mcp`.
- External OAuth/OIDC Authorization Server handles login, consent, client registration, token issuance, refresh, and MFA.
- `brain-mcp` validates access tokens, enforces scopes, and protects tools.
- Keep existing static Bearer token for local scripts/manual testing.
- Authentik deployment and Cloudflare Tunnel hostname routing stay outside this repo.

Best fit for single-user local-first system: Authentik as the OAuth/OIDC Authorization Server, if owner accepts running a small identity stack.

Reason:

- Authentik is self-hostable and local-first aligned.
- It supports OAuth2/OIDC, authorization code flow, PKCE, scopes, refresh token controls, and normal login security.
- It avoids writing bespoke OAuth security code in `brain-mcp`.
- It can serve shared `sazanka.io` identity needs beyond `brain-mcp`.
- Operational weight is higher than custom code, but security risk is much lower.

Fallback:

- Use Zitadel Cloud if low operations matter more than local-first hosting.
- Use built-in minimal AS only as an experimental learning branch, not production path.

Preferred flow:

1. ChatGPT requests `/mcp` without token or reads protected-resource metadata.
2. `brain-mcp` returns `401` with `WWW-Authenticate` pointing to resource metadata.
3. ChatGPT fetches `/.well-known/oauth-protected-resource`.
4. ChatGPT discovers authorization server from `authorization_servers`.
5. ChatGPT fetches `/.well-known/oauth-authorization-server`.
6. ChatGPT identifies/registers its OAuth client.
7. ChatGPT starts authorization-code flow with PKCE S256.
8. User logs in/approves through external Authorization Server.
9. ChatGPT exchanges code at Authorization Server token endpoint.
10. ChatGPT calls `/mcp` with `Authorization: Bearer <access_token>`.
11. `brain-mcp` verifies token and checks tool scopes.

### Standards target

- OAuth 2.1 style authorization code flow.
- PKCE with `S256`.
- OAuth 2.0 Protected Resource Metadata.
- OAuth 2.0 Authorization Server Metadata supplied by chosen Authorization Server.
- Resource Indicators: ChatGPT sends `resource`; server echoes/binds it to token audience.
- Client registration via one of:
  - Client ID Metadata Documents (CIMD), preferred if feasible.
  - Dynamic Client Registration (DCR), likely needed for broad client compatibility.
  - Static predefined client, only if ChatGPT UI allows creator-entered client details.

### Access tokens

Initial recommendation: JWT access tokens issued by external Authorization Server and validated by `brain-mcp` with issuer JWKS.

Reason:

- Authentik, Zitadel, and Keycloak already issue signed tokens and publish OIDC/JWKS metadata.
- `brain-mcp` only needs resource-server validation.
- No local token issuance, storage, rotation, or refresh-token handling in `brain-mcp`.
- Easier to audit than a bespoke AS.

Access token properties:

- `iss`
- `subject`
- `aud` or token-bound `resource`
- `scopes`
- `exp`
- `client_id` if present

Lifetime:

- Access token: 1 hour.
- Authorization code: controlled by external Authorization Server.
- Browser session: controlled by external Authorization Server.
- Refresh token: controlled by external Authorization Server, only if ChatGPT requires stable reconnection.

### Refresh tokens

Initial recommendation: let external Authorization Server decide and store refresh tokens.

If needed:

- Enable refresh tokens in Authentik/Zitadel/Keycloak.
- Prefer rotation.
- Keep lifetime short enough for private Brain exposure.
- Never store ChatGPT refresh tokens in `brain-mcp`.

### Identity verification, user authorization, and tool authorization

Separate three decisions:

1. Identity verification: external Authorization Server verifies who the caller is.
2. User authorization: `brain-mcp` decides whether verified identity can access this Brain vault.
3. Tool authorization: MCP tool scopes decide what permitted user can do.

Identity verification inputs can include:

- `sub`
- `email`
- `groups`
- `client_id`

For v1, user authorization can be env-based allowlists:

```text
BRAIN_MCP_ALLOWED_SUBJECTS=user-sub-1,user-sub-2
BRAIN_MCP_ALLOWED_EMAILS=iori.mizutani@gmail.com
BRAIN_MCP_ALLOWED_GROUPS=brain-mcp-admins
```

Future config file, for example `config/allowed-users.yaml`:

```yaml
allowed_users:
  - email: iori.mizutani@gmail.com
    scopes:
      - brain:read
      - brain:write
      - brain:git
      - brain:admin
```

Tool authorization remains scope-based:

- `brain:read`
- `brain:write`
- `brain:git`
- `brain:admin`

Do not treat `BRAIN_MCP_ALLOWED_USER_EMAIL` as long-term model. If used during bootstrap, keep it as compatibility alias for `BRAIN_MCP_ALLOWED_EMAILS`.

### Client registration assumptions for ChatGPT

OpenAI docs state ChatGPT supports CIMD, DCR, predefined OAuth clients, and PKCE. ChatGPT prefers CIMD when the authorization server advertises `client_id_metadata_document_supported`, and DCR remains supported when `registration_endpoint` is advertised.

Design choice after ChatGPT testing:

- Static/predefined/manual client path is no longer the priority because ChatGPT completed token exchange but did not attach the Bearer token to `/mcp`.
- Implement DCR proof first because ChatGPT can create a dedicated client automatically when `registration_endpoint` is advertised.
- Do not advertise CIMD yet because no CIMD path exists.
- Do not route registered `brain-mcp-*` clients directly to Authentik unless Authentik also knows those clients. Without Authentik DCR/API provisioning, Authentik will reject unknown client IDs.
- Treat the current DCR endpoint as compatibility scaffolding for the next authorization-code flow, not as a complete OAuth solution.

### ChatGPT discovery

ChatGPT can discover auth metadata through:

- `401 Unauthorized` from `/mcp` with `WWW-Authenticate: Bearer resource_metadata="https://brain.sazanka.io/.well-known/oauth-protected-resource", scope="brain:read brain:write brain:git"`.
- Direct `GET https://brain.sazanka.io/.well-known/oauth-protected-resource`.

Both should be implemented.

### Authorization Server choice

Question: should `brain-mcp` implement its own OAuth Authorization Server?

Recommendation: no for production. Use external OAuth/OIDC Authorization Server. For local-first single-user use, choose Authentik.

| Option | Fit | Strengths | Problems |
| --- | --- | --- | --- |
| Built-in OAuth server | Experimental only | Small deployment, direct learning value, can match exact MCP needs | High security risk, must implement login/session/CSRF/PKCE/code replay/refresh/revocation/client registration correctly, distracts from Brain product |
| Authentik | Recommended local-first choice | Self-hosted, OAuth2/OIDC, PKCE, scopes, refresh controls, MFA, practical admin UI | Extra services and configuration; heavier than one Go binary |
| Zitadel | Good hosted fallback | Strong standards support, PKCE/OIDC, low operations with cloud, good for production auth | Less local-first if using cloud; self-hosting still adds identity stack |
| Keycloak | Technically strong but too heavy | Mature, broad OAuth/OIDC features, PKCE, realms, policies | Operationally large for one user; admin complexity can dominate project |
| Cloudflare Access | Outer gate only | Already near tunnel, strong identity/device policies, simple owner allowlist | Not a clean ChatGPT-facing OAuth AS for MCP tokens; can block token exchange/metadata; better as perimeter protection |

Architecture after recommendation:

```text
ChatGPT
-> OAuth flow with Authentik
-> ChatGPT receives access token
-> brain.sazanka.io/mcp
-> brain-mcp validates Authentik token and scopes
-> Brain vault tools
```

`brain-mcp` still serves Protected Resource Metadata because it is the MCP resource. Authorization Server Metadata should point to Authentik/Zitadel/Keycloak issuer metadata, not a hand-built `/oauth/token` unless the built-in AS experiment is explicitly selected.

## 6. Metadata endpoints

### `GET /.well-known/oauth-protected-resource`

Resource server metadata.

Recommended response:

```json
{
  "resource": "https://brain.sazanka.io/mcp",
  "authorization_servers": ["https://auth.sazanka.io/application/o/brain-mcp/"],
  "bearer_methods_supported": ["header"],
  "scopes_supported": ["brain:read", "brain:write", "brain:git", "brain:admin"],
  "resource_documentation": "https://brain.sazanka.io/docs/oauth"
}
```

Notes:

- `resource` should use canonical MCP endpoint URI: `https://brain.sazanka.io/mcp`.
- Authorization requests and token requests should include same `resource`.
- If interoperability problems appear, test `https://brain.sazanka.io` as resource. Keep one canonical value in config.

Optional endpoint for path-specific discovery:

- `GET /.well-known/oauth-protected-resource/mcp`

This can return same document with `resource: "https://brain.sazanka.io/mcp"`.

### `GET /.well-known/oauth-authorization-server`

Experimental DCR compatibility metadata. Enabled only with:

```text
BRAIN_MCP_OAUTH_DCR_ENABLED=true
BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER_ISSUER=https://brain.sazanka.io
BRAIN_MCP_OAUTH_AUTHORIZATION_SERVERS=https://brain.sazanka.io
```

Response shape:

```json
{
  "issuer": "https://brain.sazanka.io",
  "authorization_endpoint": "https://brain.sazanka.io/oauth/authorize",
  "token_endpoint": "https://brain.sazanka.io/oauth/token",
  "registration_endpoint": "https://brain.sazanka.io/oauth/register",
  "jwks_uri": "https://brain.sazanka.io/.well-known/jwks.json",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "token_endpoint_auth_methods_supported": ["none"],
  "code_challenge_methods_supported": ["S256"],
  "scopes_supported": ["brain:read", "brain:write", "brain:git", "brain:admin"]
}
```

Current limitation: this is a private single-user authorization-server facade, not a general OAuth server.

### `POST /oauth/register`

Experimental DCR registration endpoint. Accepts ChatGPT client metadata:

```json
{
  "client_name": "ChatGPT brain-mcp",
  "redirect_uris": ["https://chatgpt.com/connector/oauth/callback"],
  "grant_types": ["authorization_code"],
  "response_types": ["code"],
  "token_endpoint_auth_method": "none"
}
```

Validation:

- `redirect_uris` must be absolute HTTPS URLs.
- `redirect_uris` must start with `https://chatgpt.com/connector/oauth/`.
- `grant_types` must include `authorization_code`.
- `response_types` must include `code`.
- `token_endpoint_auth_method` must be `none`.

Storage:

- In-memory only for the proof.
- Restart loses registered clients.

### `GET /.well-known/oauth-authorization-server`

Authorization server metadata.

If Authentik is used, this endpoint should be served by Authentik under its issuer URL, for example:

```text
https://auth.sazanka.io/application/o/brain-mcp/.well-known/openid-configuration
```

`brain-mcp` should not duplicate Authorization Server Metadata in production. It should only point `authorization_servers` to external issuer metadata.

If a built-in experimental AS is enabled, recommended metadata would be:

Recommended initial response with DCR:

```json
{
  "issuer": "https://brain.sazanka.io",
  "authorization_endpoint": "https://brain.sazanka.io/oauth/authorize",
  "token_endpoint": "https://brain.sazanka.io/oauth/token",
  "registration_endpoint": "https://brain.sazanka.io/oauth/register",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "token_endpoint_auth_methods_supported": ["none"],
  "code_challenge_methods_supported": ["S256"],
  "scopes_supported": ["brain:read", "brain:write", "brain:git", "brain:admin"]
}
```

If CIMD is implemented:

```json
{
  "client_id_metadata_document_supported": true
}
```

If refresh tokens are implemented:

```json
{
  "grant_types_supported": ["authorization_code", "refresh_token"]
}
```

### `GET /oauth/authorize`

Authorization endpoint.

Production: handled by external Authorization Server, not `brain-mcp`.

Experimental built-in AS only:

Accept:

- `response_type=code`
- `client_id`
- `redirect_uri`
- `scope`
- `state`
- `code_challenge`
- `code_challenge_method=S256`
- `resource`
- optional `id_token_hint`

Required checks:

- `response_type` must be `code`.
- `code_challenge_method` must be `S256`.
- `code_challenge` must be present.
- `resource` must equal configured `BRAIN_MCP_OAUTH_RESOURCE`.
- Requested scopes must be supported.
- `redirect_uri` must match registered client redirect URI.
- User must be authenticated locally and match allowlist.

Output:

- If no local session: show login page.
- If logged in but no approval: show consent/approval page.
- On approval: redirect to `redirect_uri?code=...&state=...`.

### `POST /oauth/token`

Token endpoint.

Production: handled by external Authorization Server, not `brain-mcp`.

Experimental built-in AS only:

Accept authorization code exchange:

- `grant_type=authorization_code`
- `code`
- `redirect_uri`
- `client_id`
- `code_verifier`
- `resource`

Required checks:

- Authorization code exists, unused, unexpired.
- Code belongs to same `client_id`, `redirect_uri`, `resource`.
- PKCE verifier matches stored S256 challenge.
- User still allowed.
- Client still registered.

Response:

```json
{
  "access_token": "opaque-token",
  "token_type": "Bearer",
  "expires_in": 3600,
  "scope": "brain:read brain:write brain:git"
}
```

If refresh token needed:

```json
{
  "refresh_token": "opaque-refresh-token"
}
```

### `POST /oauth/register`

Dynamic Client Registration endpoint, if implemented.

Production: prefer external Authorization Server support or static client setup. `brain-mcp` should not implement DCR unless built-in AS experiment is selected.

Accept:

- `client_name`
- `redirect_uris`
- `grant_types`
- `response_types`
- `token_endpoint_auth_method`

Policy:

- Allow public clients with `token_endpoint_auth_method=none`.
- Require HTTPS redirect URIs, except localhost for local dev.
- For ChatGPT production, allow redirect URI shown in ChatGPT UI, currently shaped like `https://chatgpt.com/connector/oauth/{callback_id}`.
- Store registered clients in sqlite.

Response:

```json
{
  "client_id": "generated-client-id",
  "client_name": "ChatGPT",
  "redirect_uris": ["https://chatgpt.com/connector/oauth/..."],
  "grant_types": ["authorization_code"],
  "response_types": ["code"],
  "token_endpoint_auth_method": "none"
}
```

### `WWW-Authenticate` on `/mcp`

For missing/invalid token:

```text
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer resource_metadata="https://brain.sazanka.io/.well-known/oauth-protected-resource", scope="brain:read brain:write brain:git"
```

For insufficient scope:

```text
HTTP/1.1 403 Forbidden
WWW-Authenticate: Bearer error="insufficient_scope", scope="brain:write", resource_metadata="https://brain.sazanka.io/.well-known/oauth-protected-resource", error_description="Additional Brain write permission required"
```

For tool-level MCP error result, include `_meta["mcp/www_authenticate"]` when ChatGPT needs in-chat OAuth linking UI.

## 7. Scopes

Scopes:

- `brain:read`: read note contents, list notes, inspect sections, show diffs.
- `brain:write`: modify Markdown note contents.
- `brain:git`: inspect and commit git changes.
- `brain:admin`: future admin operations such as token/client/session management.

Tool mapping:

| Tool | Required scope |
| --- | --- |
| `brain_read_note` | `brain:read` |
| `brain_list_notes` | `brain:read` |
| `brain_get_section` | `brain:read` |
| `brain_show_diff` | `brain:read` |
| `brain_apply_patch` | `brain:write` |
| `brain_write_note` | `brain:write` |
| `brain_append_section` | `brain:write` |
| `brain_replace_section` | `brain:write` |
| `brain_upsert_section` | `brain:write` |
| `brain_delete_duplicate_section` | `brain:write` |
| `brain_replace_text` | `brain:write` |
| `brain_git_status` | `brain:git` |
| `brain_git_diff` | `brain:git` |
| `brain_git_commit` | `brain:git` |

`brain_git_status` and `brain_git_diff` could also be allowed with `brain:read`, but keep `brain:git` separate initially because diffs can reveal private content and git tools form commit workflow.

`brain_apply_patch` appears in README/PLAN and should be included in scope design even if not listed in prompt current tools.

## 8. Tool-level authorization

Do not rely only on route-level auth.

Design:

- HTTP auth middleware validates credential and creates auth context:
  - `subject`
  - `client_id`
  - `resource`
  - `scopes`
  - `auth_kind` (`bearer_static`, `oauth`, `anonymous`)
- Pass auth context into MCP handler.
- `tools/list` should include `securitySchemes` per tool:
  - OAuth-required production tools: `{ "type": "oauth2", "scopes": [...] }`
  - Future no-auth read tools: `{ "type": "noauth" }` plus optional OAuth scheme.
- `tools/call` dispatch checks required scopes before calling vault/git code.
- Scope failure returns MCP error result or HTTP 403 with `WWW-Authenticate`, depending on request stage.

Suggested Go shape:

```text
internal/auth/
  context.go
  scopes.go
  oauth_store.go
  bearer.go

internal/mcp/
  Server.HandleBytes(ctx context.Context, data []byte)
  requiredScopes(toolName string) []string
```

Authorization checks should happen before parsing tool arguments if tool name is known.

## 9. Mixed mode

Future mode:

- Read-only tools may run without auth.
- Write/git tools require OAuth.

Initial production mode:

- Require OAuth for all MCP tools.

Mixed-mode rules:

- `tools/list` can include all tools, but each tool must advertise correct `securitySchemes`.
- Anonymous calls allowed only for explicit read-only tools.
- No-auth mode must never expose:
  - `brain_apply_patch`
  - `brain_write_note`
  - `brain_append_section`
  - `brain_replace_section`
  - `brain_upsert_section`
  - `brain_delete_duplicate_section`
  - `brain_replace_text`
  - `brain_git_commit`
- Consider also hiding `brain_git_diff` anonymously because diff content can reveal private notes.

## 10. Existing Bearer token compatibility

Keep static bearer token support for:

- curl/manual testing.
- local scripts.
- possible automation.

Separate it from OAuth:

- Static bearer tokens are configured secrets.
- OAuth access tokens are issued credentials with user/client/scope/resource/expiry.
- Do not store OAuth access tokens in `BRAIN_MCP_TOKEN`.
- Do not treat `BRAIN_MCP_TOKEN` as OAuth token.

Proposed config:

```text
BRAIN_MCP_AUTH_MODE=bearer|oauth|mixed|none
BRAIN_MCP_TOKEN=replace-with-long-random-token
BRAIN_MCP_OAUTH_ISSUER=https://auth.sazanka.io/application/o/brain-mcp/
BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER=https://auth.sazanka.io/application/o/brain-mcp/
BRAIN_MCP_OAUTH_JWKS_URL=https://auth.sazanka.io/application/o/brain-mcp/jwks/
BRAIN_MCP_OAUTH_RESOURCE=https://brain.sazanka.io/mcp
BRAIN_MCP_ALLOWED_SUBJECTS=
BRAIN_MCP_ALLOWED_EMAILS=iori.mizutani@gmail.com
BRAIN_MCP_ALLOWED_GROUPS=
BRAIN_MCP_OAUTH_ACCESS_TOKEN_TTL=1h
BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES=https://brain.sazanka.io/mcp
BRAIN_MCP_OAUTH_REQUIRED_ISSUER=https://auth.sazanka.io/application/o/brain-mcp/
```

Mode semantics:

- `bearer`: current behavior. Static bearer required for all protected routes.
- `oauth`: OAuth required for `/mcp`; static bearer not accepted on public MCP unless explicitly enabled for local routes.
- `mixed`: OAuth and static bearer both accepted; tool-level scopes still enforced for OAuth, static bearer gets configured scopes.
- `none`: local dev only; no-auth tools only. Must not expose write/git tools.

Static bearer scope config:

```text
BRAIN_MCP_BEARER_SCOPES=brain:read,brain:write,brain:git
```

## 11. User identity

Options:

### A. Built-in local login page with username/password

Pros:

- Self-contained.
- No external identity provider.
- Works through ChatGPT browser OAuth flow.
- Easy single-user allowlist.

Cons:

- Must implement password/session security.
- Must store password hash.
- More responsibility for brute-force protection and CSRF.

Fit: experimental only. Useful for learning, not preferred production path.

### B. Cloudflare Access identity headers

Pros:

- Strong outer identity.
- Uses existing Cloudflare Access policies.
- Can enforce owner email before app sees request.

Cons:

- ChatGPT server-to-server token calls may not have browser Access cookies.
- OAuth endpoints could be blocked before ChatGPT can discover metadata or exchange tokens.
- Cloudflare Access headers are not present unless Access protects route and request passes policy.

Fit: avoid initially for OAuth endpoints. Consider later as outer protection for login page only.

### C. Authentik

Pros:

- Self-hosted identity provider.
- OAuth2/OIDC with PKCE and scopes.
- MFA, session, password, refresh-token, and client controls handled outside `brain-mcp`.
- Keeps local-first posture better than hosted identity.

Cons:

- Adds identity infrastructure.
- Requires proxy/issuer/redirect URI setup.
- More moving parts than current single Go service.

Fit: recommended production path for local-first single-user Brain MCP.

### D. Zitadel

Pros:

- Mature OAuth/OIDC provider.
- Hosted option reduces operations.
- PKCE and modern OIDC flows supported.

Cons:

- Hosted mode weakens local-first goal.
- Self-hosted mode still adds identity stack.

Fit: best fallback if low ops matter more than local-first hosting.

### E. Keycloak

Pros:

- Mature, widely used OAuth/OIDC server.
- Very complete feature set.
- Good protocol support.

Cons:

- Too much operational and admin surface for one user.
- Setup complexity can dominate project.

Fit: technically valid but not recommended for this project.

### F. Cloudflare Access

Pros:

- Already near Cloudflare Tunnel.
- Strong access policies for owner email, device posture, MFA, and service-token patterns.
- Good perimeter control.

Cons:

- Not a clean OAuth Authorization Server for ChatGPT MCP resource tokens.
- Can break ChatGPT metadata discovery and token exchange if placed in front of OAuth endpoints.
- Does not remove need for MCP-compatible OAuth resource metadata and tool-level scopes.

Fit: optional outer gate/admin protection, not primary MCP OAuth identity.

Recommendation:

- Initial production: Authentik issues OAuth/OIDC tokens. `brain-mcp` validates tokens and scopes.
- Fallback: Zitadel Cloud if running Authentik is too much operations.
- Avoid built-in AS except explicit learning branch.
- Avoid Cloudflare Access for core OAuth flow until ChatGPT compatibility is proven.

## 12. Cloudflare Access interaction

Initial recommendation: do not put Cloudflare Access in front of MCP OAuth discovery/token endpoints.

Reason:

- ChatGPT must fetch metadata and call `/oauth/token` server-to-server.
- Access can block non-browser requests or require cookies ChatGPT does not have.
- Debugging nested OAuth plus Access is harder.

Endpoint policy:

- Public through tunnel:
  - `GET /.well-known/oauth-protected-resource`
  - `GET /.well-known/oauth-protected-resource/mcp`
  - `POST /mcp`
- Public through external Authorization Server hostname:
  - Authorization Server metadata.
  - Authorization endpoint.
  - Token endpoint.
  - Registration endpoint if external AS supports DCR.
- Protected by app OAuth:
  - `/mcp`
  - write/git tool calls
- Optional Cloudflare Access later:
  - Login page only, if it does not break ChatGPT redirect flow.
  - Admin pages only.

Avoid initially:

- Cloudflare Access as identity provider for OAuth.
- Access in front of external Authorization Server token endpoint.
- Access in front of MCP protected-resource metadata endpoint.

## 13. Storage

Options:

- Memory only.
- sqlite.
- boltdb.
- Encrypted local file.
- No refresh tokens initially.

Recommendation: no OAuth token/client/session storage in `brain-mcp` for production.

Reason:

- External Authorization Server owns clients, auth codes, sessions, refresh tokens, and access-token issuance.
- `brain-mcp` only validates tokens and enforces tool authorization.
- Less secret material exists near Brain vault code.
- Restart does not invalidate external OAuth state.

Production `brain-mcp` storage:

- JWKS cache in memory with TTL.
- Optional allowlist config from env.
- Optional audit log without token values.
- No refresh tokens.
- No access-token database.
- No authorization-code database.
- No user-password database.

Experimental built-in AS storage, if learning branch proceeds:

- `oauth_clients`
  - `client_id`
  - `client_name`
  - `redirect_uris_json`
  - `token_endpoint_auth_method`
  - `created_at`
  - `last_seen_at`
- `oauth_auth_codes`
  - `code_hash`
  - `client_id`
  - `redirect_uri`
  - `subject`
  - `resource`
  - `scopes`
  - `code_challenge`
  - `expires_at`
  - `used_at`
- `oauth_access_tokens`
  - `token_hash`
  - `subject`
  - `client_id`
  - `resource`
  - `scopes`
  - `expires_at`
  - `revoked_at`
- `oauth_sessions`
  - `session_hash`
  - `subject`
  - `expires_at`
  - `csrf_secret_hash`
- `oauth_settings`
  - future migration metadata, optional.

Allowed users:

- Production: external AS user/group claims plus `BRAIN_MCP_ALLOWED_SUBJECTS`, `BRAIN_MCP_ALLOWED_EMAILS`, and `BRAIN_MCP_ALLOWED_GROUPS`.
- Experimental built-in AS: config env for first version.

Password hash:

- Production: owned by external AS.
- Experimental built-in AS: store outside git, either env or sqlite setting initialized from env:
  - `BRAIN_MCP_OAUTH_PASSWORD_HASH`.
- Never store plaintext password.

## 14. Security constraints

Preserve existing safety policies:

- All filesystem access must remain under `$BRAIN_ROOT`.
- Reject path traversal.
- Reject absolute paths.
- Reject `.git` access except controlled git commands.
- Reject hidden paths unless explicitly allowed.
- Preserve read-only path rules.
- Require diff-oriented workflows for writes.
- Do not add arbitrary shell execution.
- Do not expose absolute vault paths through API responses.
- Do not allow symlink escapes outside `$BRAIN_ROOT`.
- Writes must remain restricted to configured writable prefixes.
- Writes must remain Markdown-only.

Additional OAuth constraints:

- HTTPS public issuer/resource in production.
- Secure cookies for login sessions.
- `SameSite=Lax` session cookies.
- CSRF token on login/approval forms.
- No token values in logs.
- Constant-time token hash comparison if direct compare needed.
- Single-use auth codes.
- Strict redirect URI validation.
- Strict resource/audience validation.

## 15. Operational configuration

### Local dev

```text
BRAIN_ROOT=/path/to/Brain
BRAIN_MCP_ADDR=127.0.0.1:8787
BRAIN_MCP_AUTH_MODE=mixed
BRAIN_MCP_TOKEN=local-long-random-token
BRAIN_MCP_BEARER_SCOPES=brain:read,brain:write,brain:git
BRAIN_MCP_OAUTH_ISSUER=http://127.0.0.1:9000/application/o/brain-mcp/
BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER=http://127.0.0.1:9000/application/o/brain-mcp/
BRAIN_MCP_OAUTH_JWKS_URL=http://127.0.0.1:9000/application/o/brain-mcp/jwks/
BRAIN_MCP_OAUTH_RESOURCE=http://127.0.0.1:8787/mcp
BRAIN_MCP_ALLOWED_EMAILS=iori.mizutani@gmail.com
BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES=http://127.0.0.1:8787/mcp
```

Local OAuth redirect URIs may use `http://localhost` or `http://127.0.0.1` for test clients.

### Production through Cloudflare Tunnel

```text
BRAIN_ROOT=/brain
BRAIN_MCP_ADDR=0.0.0.0:8787
BRAIN_MCP_AUTH_MODE=oauth
BRAIN_MCP_OAUTH_ISSUER=https://auth.sazanka.io/application/o/brain-mcp/
BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER=https://auth.sazanka.io/application/o/brain-mcp/
BRAIN_MCP_OAUTH_JWKS_URL=https://auth.sazanka.io/application/o/brain-mcp/jwks/
BRAIN_MCP_OAUTH_RESOURCE=https://brain.sazanka.io/mcp
BRAIN_MCP_ALLOWED_EMAILS=iori.mizutani@gmail.com
BRAIN_MCP_OAUTH_ACCESS_TOKEN_TTL=1h
BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES=https://brain.sazanka.io/mcp
BRAIN_MCP_TOKEN=manual-testing-long-random-token
```

### Docker Compose

Add persistent state mount:

```yaml
services:
  brain-mcp:
    environment:
      BRAIN_MCP_AUTH_MODE: ${BRAIN_MCP_AUTH_MODE:-oauth}
      BRAIN_MCP_OAUTH_ISSUER: ${BRAIN_MCP_OAUTH_ISSUER}
      BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER: ${BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER}
      BRAIN_MCP_OAUTH_JWKS_URL: ${BRAIN_MCP_OAUTH_JWKS_URL}
      BRAIN_MCP_OAUTH_RESOURCE: ${BRAIN_MCP_OAUTH_RESOURCE}
      BRAIN_MCP_ALLOWED_EMAILS: ${BRAIN_MCP_ALLOWED_EMAILS}
    volumes:
      - ${BRAIN_ROOT_HOST}:/brain
```

### Secrets outside git repo

- Real `.env` stays ignored.
- Static bearer token, Cloudflare tunnel token, and identity provider secrets are not committed.
- Repo contains `.env.example` only.

`.env.example` should show names and safe placeholders, not real values.

## 16. Test plan

Metadata tests:

- `GET /.well-known/oauth-protected-resource` returns `resource`, `authorization_servers`, `scopes_supported`.
- `GET /.well-known/oauth-authorization-server` returns authorization endpoint, token endpoint, PKCE S256, supported scopes.
- Unauthorized `/mcp` returns 401 and `WWW-Authenticate` with `resource_metadata`.

OAuth flow tests:

- Protected-resource metadata points to configured external Authorization Server.
- Authorization Server metadata is reachable from configured issuer in integration environment.
- JWKS is fetched and cached.
- JWT signature validates against issuer JWKS.
- Token with wrong issuer is rejected.
- Token with wrong audience/resource is rejected.
- Token with missing subject is rejected.
- Invalid token rejected.
- Expired token rejected.
- Malformed JWT rejected.
- Opaque/static bearer token does not pass OAuth validator.
- Built-in AS experimental mode, if implemented later, tests authorization code flow, PKCE, redirect URI validation, code expiry, and code replay.

Scope tests:

- `brain:read` can call `brain_read_note`.
- `brain:read` can call `brain_list_notes`.
- `brain:read` can call `brain_get_section`.
- `brain:read` cannot call write tools.
- `brain:write` can modify notes.
- `brain:write` cannot commit unless `brain:git` also present.
- `brain:git` can call `brain_git_status`, `brain_git_diff`, `brain_git_commit`.
- Insufficient scope returns 403 or MCP auth challenge with required scope.

Compatibility tests:

- Existing static Bearer auth still works when `BRAIN_MCP_AUTH_MODE=bearer`.
- Static Bearer auth works with configured scopes in `mixed`.
- OAuth token works in `oauth`.
- No-auth mode does not expose write tools.
- No-auth mode rejects write/git calls even if tool name is manually invoked.

Safety regression tests:

- Path traversal still rejected.
- Absolute paths still rejected.
- Hidden paths still rejected.
- `.git` note reads/writes still rejected.
- Read-only prefixes still block writes.
- Symlink escapes still rejected.
- Write tools still return diffs.

Integration tests:

- MCP initialize works with OAuth token.
- `notifications/initialized` still ignored safely.
- `tools/list` includes `securitySchemes`.
- ChatGPT-style auth challenge metadata is present.
- Full flow: authorize, token exchange, list tools, read note, write section, inspect git diff, commit.

## 17. Migration plan

### Phase 1: OAuth resource-server foundation

- Keep existing Bearer token mode working.
- Add auth mode config.
- Add OAuth protected-resource metadata endpoint.
- Add `WWW-Authenticate` challenge from `/mcp`.
- Add issuer/audience/scope configuration for later JWT/JWKS validation.
- Do not implement semantic memory tools yet.
- Do not implement JWT validation yet.

### Phase 2: External Authorization Server integration

- Choose Authentik as first target unless a strong blocker appears.
- Run Authentik on the always-on home server.
- Keep Authentik deployment/config in a separate infrastructure repository or directory, for example `home-infra`.
- Expose `auth.sazanka.io` through Cloudflare Tunnel.
- Prefer a single `cloudflared` tunnel with multiple public hostnames/ingress rules if possible.
- Do not add Authentik services directly to `brain-mcp` Docker Compose.
- Configure Authentik application/provider for `brain-mcp`.
- Configure scopes:
  - `brain:read`
  - `brain:write`
  - `brain:git`
  - `brain:admin`
- Configure static OAuth client for ChatGPT if possible.
- Allow transitional architecture where `auth.sazanka.io` runs on the home server and `brain.sazanka.io` still points to Mac-hosted `brain-mcp`.
- If ChatGPT requires DCR/CIMD, document exact blocker and decide whether to:
  - configure Authentik differently
  - add a small compatibility proxy
  - switch to Zitadel
  - temporarily implement minimal DCR elsewhere
- Do not implement a full OAuth Authorization Server inside `brain-mcp` unless all external AS options fail.

### Phase 3: Tool-level scope enforcement

- Add auth context.
- Map current tools to scopes.
- Enforce scopes inside MCP tool dispatch.
- Add `securitySchemes` to tool definitions.
- Ensure read, write, and git tools behave correctly by scope.

### Phase 4: ChatGPT Custom MCP connection test

- Register Custom MCP in ChatGPT UI.
- Use `https://brain.sazanka.io/mcp`.
- Select OAuth.
- Complete authorization flow.
- Verify:
  - ChatGPT discovers tools.
  - ChatGPT can call read-only tools.
  - ChatGPT can call write tools only with proper scope.
  - ChatGPT can inspect git status/diff.
  - Commit support is tested carefully.
- Capture any ChatGPT-specific requirements.

### Phase 5: Hardening

- Improve logs without leaking tokens or note contents.
- Add negative tests.
- Add rate limits if needed.
- Decide whether Cloudflare Access belongs anywhere.
- Update README and `.env.example`.

### Phase 6: Future memory workflows

Post-connector work:

- Memory Promotion Pipeline.
- Self Semantic Layers.
- Candidate Workflow.
- Commit Suggestion.
- Journal to Self extraction.
- Facts / Observed Patterns / Interpretations / Open Hypotheses tooling.
- Decide whether to reintroduce Cloudflare Access on selected routes.

## 18. Open questions

- Does ChatGPT Custom MCP require dynamic client registration for this UI path, or can it use CIMD/static clients?
- What exact metadata shape does current ChatGPT Custom MCP scan require in practice?
- Can ChatGPT work with a single static OAuth client for a private custom MCP server?
- Does ChatGPT always support public clients with PKCE and `token_endpoint_auth_method=none` for custom MCP?
- Does ChatGPT require refresh tokens for long-lived connector links?
- What exact redirect URI will ChatGPT show/use for this custom MCP server?
- Should `resource` be `https://brain.sazanka.io/mcp` or `https://brain.sazanka.io` for best ChatGPT compatibility?
- Does Authentik support the exact ChatGPT Custom MCP client-registration path needed here, or is Zitadel easier for first connection?
- How should Authentik scopes map into JWT claims: `scope`, `scp`, groups, or custom claim?
- Should Cloudflare Access be removed entirely from OAuth endpoints, or kept only for admin/login routes?
- Should `brain_git_status` and `brain_git_diff` require `brain:git`, or is `brain:read` enough?
- Should `brain_show_diff` require `brain:read` only, or `brain:write` because it previews write intent?
- Should future read-only sharing mode use separate public note prefixes?
- Should built-in AS remain an experiment branch, or be dropped entirely after Authentik works?
- What audit logs are useful without leaking note content or tokens?

## Sources

- OpenAI Apps SDK authentication docs: `https://developers.openai.com/apps-sdk/build/auth`
- OpenAI MCP server guide: `https://developers.openai.com/api/docs/mcp`
- MCP authorization specification 2025-11-25: `https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization`
- MCP authorization specification 2025-06-18: `https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization`
- OAuth 2.0 Protected Resource Metadata RFC 9728: `https://www.rfc-editor.org/rfc/rfc9728`
- OAuth 2.0 Authorization Server Metadata RFC 8414: `https://www.rfc-editor.org/rfc/rfc8414`
- OAuth 2.0 Dynamic Client Registration RFC 7591: `https://www.rfc-editor.org/rfc/rfc7591`
- OAuth 2.0 Resource Indicators RFC 8707: `https://www.rfc-editor.org/rfc/rfc8707`
- Authentik OAuth2 provider docs: `https://docs.goauthentik.io/add-secure-apps/providers/oauth2/`
- Zitadel OIDC/OAuth docs: `https://zitadel.com/docs`
- Keycloak server administration docs: `https://www.keycloak.org/docs/latest/server_admin/`
- Cloudflare Access docs: `https://developers.cloudflare.com/cloudflare-one/`
