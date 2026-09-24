---
title: OAuth
description: Authenticate against OAuth-protected MCP servers — authorization-code + PKCE, client-credentials, enterprise managed authorization, dynamic registration, and token caching.
---

When an HTTP MCP server responds with `401` and a `WWW-Authenticate` header,
MCP-TUI delegates to its OAuth handler. Three modes are supported, selected by
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
  tool list
```

The client-credentials grant always requests the scopes the resource server
advertises; `--oauth-scopes` is rejected in this mode.

## Enterprise managed authorization (SEP-990)

For MCP servers that trust your organization's identity provider (IdP)
instead of running their own sign-in. Triggered by `--oauth-idp-issuer`. The
flow has three steps:

1. You sign in to the IdP in your browser (OpenID Connect, authorization code
   + PKCE, on the loopback redirect). MCP-TUI gets an ID token.
2. MCP-TUI exchanges the ID token at the IdP for an ID-JAG, a signed grant
   addressed to the MCP server's authorization server (RFC 8693 token
   exchange).
3. MCP-TUI redeems the ID-JAG at the MCP authorization server for an access
   token (RFC 7523 JWT bearer grant).

You need two client registrations: one at the IdP (`--oauth-idp-client-id`,
plus `--oauth-idp-client-secret` if it is a confidential client) and one at
the MCP authorization server (`--oauth-client-id`, plus
`--oauth-client-secret` if confidential). Register
`http://127.0.0.1:<port>/callback` as a redirect URI for the IdP client and
pin that port with `--oauth-redirect-port`, since IdPs rarely accept an
ephemeral port.

```bash
mcp-tui --transport http --url https://mcp.example.com/mcp \
  --oauth-idp-issuer https://login.example.com \
  --oauth-idp-client-id "$IDP_CLIENT_ID" \
  --oauth-idp-client-secret "$IDP_CLIENT_SECRET" \
  --oauth-redirect-port 8765 \
  --oauth-client-id "$MCP_CLIENT_ID" \
  --oauth-scopes "mcp.read" \
  tool list
```

- `--oauth-idp-scopes` sets the sign-in scopes. It must include `openid`,
  which is also the default.
- `--oauth-scopes` sets the scopes requested for the MCP server. They are
  sent in the token exchange.
- The MCP authorization server, which is the token exchange `audience`, is
  the first entry in the server's Protected Resource Metadata. Name it with
  `--oauth-issuer` to skip that lookup.
- The IdP and MCP authorization server URLs must be `https`, or `http` on a
  loopback host for local testing.
- Client metadata URLs, dynamic registration and
  `--oauth-accept-unadvertised-iss` do not apply to this mode and are
  rejected.
- If the IdP redirect carries an `iss` that is not the IdP issuer, the flow
  stops before the code is redeemed.

The debug log shows each step: the resolved authorization server, the IdP
sign-in, the ID token's issuer, audience and expiry, the token exchange
(audience, resource, token types, and the ID-JAG's audience), and the JWT
bearer grant. It never includes a token value. When the MCP authorization
server rejects the ID-JAG, compare the exchange's `audience` with the
`id_jag_aud` the IdP issued.

The enterprise flow issues no refresh token. When the access token expires,
MCP-TUI runs the whole flow again, so the IdP's policy is checked each time.

## Dynamic client registration

When you have no client ID, `--oauth-dynamic-registration` registers one on the
fly via RFC 7591, then proceeds with the authorization-code + PKCE flow:

```bash
mcp-tui --transport http --url https://api.example.com/mcp \
  --oauth-dynamic-registration tool list
```

## Issuer check on the redirect (RFC 9207)

When the authorization server advertises
`authorization_response_iss_parameter_supported`, the redirect must carry an
`iss` equal to the discovered issuer, or the flow fails before the code is
redeemed (mix-up attack defense). A server that sends `iss` without
advertising support is refused too; `--oauth-accept-unadvertised-iss` accepts
a matching `iss` from such a server. Use it only to test non-conforming
servers; it is logged as a warning.

## Binding a client to its issuer

A pre-registered client ID is only valid at the authorization server that
issued it. Pass `--oauth-issuer` to refuse any other: if the discovered
authorization server metadata names a different `issuer`, the flow stops
before any credential is sent.

```bash
mcp-tui --transport http --url https://api.example.com/mcp \
  --oauth-client-id my-client-id \
  --oauth-issuer https://login.example.com \
  tool list
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

### Private-network endpoints

The endpoints MCP-TUI talks to during sign-in come from metadata the MCP
server publishes, so a hostile or misconfigured server could point them at
hosts inside your network. MCP-TUI refuses any auth request whose address
resolves to a private (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
`fc00::/7`), link-local (including the `169.254.169.254` cloud metadata
address), carrier-grade NAT (`100.64.0.0/10`), multicast or unspecified
address. Loopback is always allowed, so a local authorization server works.

If your authorization server lives on a private network, pass
`--oauth-allow-private-network`. Each request it lets through is logged as a
warning with the address and its class. The flag covers hostnames that
resolve to private addresses; a literal private IP in a discovered URL is
still rejected by the MCP SDK. With an HTTP proxy configured the check is off,
because every connection goes to the proxy.

## Scopes

In the authorization-code flow, MCP-TUI requests the scopes the server's
`WWW-Authenticate` challenge names, or else the `scopes_supported` in its
Protected Resource Metadata. `--oauth-scopes` (a comma- or space-separated
list) replaces that set. The debug log's `Scopes selected` line shows both
sets.

## Token cache

Tokens are cached on disk so you are not re-prompted every invocation:

| Platform | Location |
|----------|----------|
| Linux | `$XDG_CACHE_HOME/mcp-tui/oauth` |
| macOS | `~/Library/Caches/mcp-tui/oauth` |
| Windows | `%LOCALAPPDATA%\mcp-tui\oauth` |

Each entry holds the token and, for the authorization-code grant, the client
ID and token endpoint needed to refresh it. Enterprise entries hold only the
MCP access token; the ID token and ID-JAG are never written to disk. Their
cache key includes the IdP issuer and IdP client. MCP-TUI asks for `offline_access`
when the authorization server supports it (SEP-2207), so it gets a refresh
token. When a cached access token has expired, the next run refreshes it
instead of opening the browser. Every refreshed token is written back to the
cache atomically. Files are mode `0600`. Entries written by older versions,
which have no refresh endpoint, are ignored and replaced after the next
sign-in.

Override with `--oauth-cache <dir>`, or pass `--oauth-cache=-` to disable
persistence entirely.

## Re-authenticating in the TUI

Press `A` on the main screen to clear cached OAuth state; the next outgoing
request triggers a fresh authorization.

## Flag reference

| Flag | Default | Description |
|------|---------|-------------|
| `--oauth-client-id` | | Client ID (enables OAuth on HTTP transports) |
| `--oauth-client-secret` | | Client secret (switches to client-credentials grant, except in enterprise mode) |
| `--oauth-issuer` | | Issuer the pre-registered client is bound to; in enterprise mode, the MCP authorization server |
| `--oauth-idp-issuer` | | Enterprise IdP issuer; selects enterprise managed authorization |
| `--oauth-idp-client-id` | | Client ID at the IdP |
| `--oauth-idp-client-secret` | | Client secret at the IdP (confidential IdP client) |
| `--oauth-idp-scopes` | `openid` | IdP sign-in scopes; must include `openid` |
| `--oauth-client-metadata-url` | | Client ID Metadata Document URL (SEP-991) |
| `--oauth-scopes` | | Comma- or space-separated scopes (authorization code and enterprise) |
| `--oauth-redirect-host` | `127.0.0.1` | Auth-code redirect host (`localhost`, `127.0.0.0/8` or `::1`) |
| `--oauth-redirect-port` | `0` | Auth-code redirect port (`0` = ephemeral) |
| `--oauth-dynamic-registration` | `false` | RFC 7591 dynamic registration when client ID is empty |
| `--oauth-accept-unadvertised-iss` | `false` | Accept `iss` from an AS that does not advertise RFC 9207 support (testing only) |
| `--oauth-allow-private-network` | `false` | Allow auth requests to private-network addresses |
| `--oauth-cache` | platform cache dir | Token cache directory (`-` disables) |
</content>
