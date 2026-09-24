---
title: OAuth
description: Authenticate against OAuth-protected MCP servers — authorization-code + PKCE, client-credentials, dynamic registration, and token caching.
---

When an HTTP MCP server responds with `401` and a `WWW-Authenticate` header,
MCP-TUI delegates to its OAuth handler. Two grants are supported, selected by
which flags you provide.

## Authorization code + PKCE (interactive)

For user-facing auth. Triggered when `--oauth-client-id` is set **without** a
secret, or when `--oauth-dynamic-registration` is used. MCP-TUI opens a
loopback redirect listener, sends you through the authorization endpoint, and
exchanges the code with PKCE (RFC 6749 §4.1, RFC 7636).

```bash
mcp-tui --transport http --url https://api.example.com/mcp \
  --oauth-client-id my-client-id \
  --oauth-scopes "mcp.read mcp.write" \
  tool list
```

Control the loopback redirect URI:

| Flag | Default | Purpose |
|------|---------|---------|
| `--oauth-redirect-host` | `127.0.0.1` | Redirect host: `localhost`, `127.0.0.0/8` or `::1` |
| `--oauth-redirect-port` | `0` | Redirect port (`0` = ephemeral) |

## Client credentials (service-to-service)

For non-interactive service auth (RFC 6749 §4.4). Triggered when **both**
`--oauth-client-id` and `--oauth-client-secret` are set.

```bash
mcp-tui --transport http --url https://api.example.com/mcp \
  --oauth-client-id svc-client \
  --oauth-client-secret "$MCP_CLIENT_SECRET" \
  --oauth-scopes "mcp.read" \
  tool list
```

## Dynamic client registration

When you have no client ID, `--oauth-dynamic-registration` registers one on the
fly via RFC 7591, then proceeds with the authorization-code + PKCE flow:

```bash
mcp-tui --transport http --url https://api.example.com/mcp \
  --oauth-dynamic-registration tool list
```

## Client ID Metadata Document

With a Client ID Metadata Document (SEP-991) you host your client's metadata
at an HTTPS URL and use that URL as the client ID; no registration step is
needed. Pass it with `--oauth-client-metadata-url`:

```bash
mcp-tui --transport http --url https://api.example.com/mcp \
  --oauth-client-metadata-url https://example.com/oauth/mcp-tui.json \
  --oauth-dynamic-registration tool list
```

The URL must be `https` with a path. The document is used only when the
authorization server advertises `client_id_metadata_document_supported`.
Otherwise MCP-TUI tries the next configured method: a pre-registered
`--oauth-client-id`, then dynamic registration. The debug log records which
method was chosen and why (`Client registration resolved`).

## Discovery

The handler always discovers endpoints: Protected Resource Metadata (RFC 9728),
then Authorization Server Metadata (RFC 8414 or OpenID Connect discovery).
When a server publishes no authorization server metadata, the MCP SDK falls
back to `/authorize`, `/token` and `/register` on the authorization server's
origin. There is no flag to override the token endpoint.

`--oauth-scopes` accepts a comma- or space-separated list.

## Token cache

Tokens are cached on disk so you are not re-prompted every invocation:

| Platform | Location |
|----------|----------|
| Linux | `$XDG_CACHE_HOME/mcp-tui/oauth` |
| macOS | `~/Library/Caches/mcp-tui/oauth` |
| Windows | `%LOCALAPPDATA%\mcp-tui\oauth` |

Override with `--oauth-cache <dir>`, or pass `--oauth-cache=-` to disable
persistence entirely.

## Re-authenticating in the TUI

Press `A` on the main screen to clear cached OAuth state; the next outgoing
request triggers a fresh authorization.

## Flag reference

| Flag | Default | Description |
|------|---------|-------------|
| `--oauth-client-id` | | Client ID (enables OAuth on HTTP transports) |
| `--oauth-client-secret` | | Client secret (switches to client-credentials grant) |
| `--oauth-client-metadata-url` | | Client ID Metadata Document URL (SEP-991) |
| `--oauth-scopes` | | Comma- or space-separated scopes |
| `--oauth-redirect-host` | `127.0.0.1` | Auth-code redirect host (`localhost`, `127.0.0.0/8` or `::1`) |
| `--oauth-redirect-port` | `0` | Auth-code redirect port (`0` = ephemeral) |
| `--oauth-dynamic-registration` | `false` | RFC 7591 dynamic registration when client ID is empty |
| `--oauth-cache` | platform cache dir | Token cache directory (`-` disables) |
</content>
