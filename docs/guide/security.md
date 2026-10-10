# Security

[简体中文](security.zh-CN.md)

This guide describes what RedApp protects, what it trusts, and what remains your responsibility.

## Trust model

| Party | Trust |
| --- | --- |
| Administrator | Fully trusted. Can change every setting, including usage instructions with JavaScript. |
| Anonymous user | Can read the public catalog and download published files. Cannot change anything. |
| Reverse proxy | Trusted only if listed in `trusted_proxies`. |
| Upstream sources | Release metadata and binaries are verified. HTTP cache content is passed through as delivered. |
| Imported configuration | Untrusted until an administrator reviews the preview and confirms it. |

RedApp has a single administrator account. There are no roles or per-user permissions.

## Administrator authentication

### Password

- On the first start, RedApp generates a random password and prints it once in the log. Protect that log, and change the password after signing in.
- A new password must be 12 to 72 bytes long. Changing it requires the current password.
- Passwords are stored as bcrypt hashes.
- Changing the password signs out every session, including your own.
- There is no command to reset a lost password.

### Sessions

| Property | Value |
| --- | --- |
| Lifetime | 8 hours from sign-in, not extended by activity |
| Storage | Server memory; a restart signs everyone out |
| Concurrent sessions | Up to 128 |
| Cookie | `redapp_session`, `HttpOnly`, `SameSite=Strict`, `Path=/admin` |
| `Secure` flag | Set when the request origin is HTTPS |

The request origin comes from TLS or from a trusted proxy's forwarded scheme. Behind an HTTPS reverse proxy, configure `trusted_proxies` so that the cookie is marked `Secure`.

### Request protection

- Every admin API request needs a valid session, except sign-in.
- Every admin request that changes data needs the `X-CSRF-Token` header. The console sends it automatically.
- An admin API request whose `Origin` header differs from the request origin is rejected.

### Sign-in rate limit

- Each client may make 10 sign-in attempts per 5 minutes. A client is an IPv4 address or an IPv6 /64 prefix. A successful sign-in clears that client's count.
- All clients together get a burst of 20 password checks, refilled at one per second. This bounds CPU use when many addresses try at once; while such an attack continues, sign-in may be refused for everyone.
- A wrong password returns `401 LOGIN_FAILED`; a rate-limited attempt returns `429 LOGIN_RATE_LIMITED`.
- Changing the password checks the current password against the same per-client limit, so a stolen session cannot be used to guess it. A wrong current password returns `400 CURRENT_PASSWORD_INCORRECT`. A successful change signs out every session.
- Signing out expires the session cookie in the browser and deletes the session on the server.
- The client IP comes from trusted proxy headers when `trusted_proxies` is set; otherwise it is the direct peer address.

## Anonymous access

Without signing in, anyone who can reach RedApp can:

- Open the home page, catalog, vendor pages and application pages of **enabled** vendors and applications.
- Read usage instructions.
- Download installers, release files, hosted files and HTTP cache files of enabled applications.
- Call the health endpoints.

Anonymous users never see private notes, tags, proxy settings, upstream credentials or disabled applications. RedApp does not restrict who may download. Use your reverse proxy or network to limit access if needed.

Each client may run at most `max_downloads_per_client` downloads at once (16 by default), so that one client cannot hold all download capacity. A client is an IPv4 address or an IPv6 /64 prefix, taken from trusted proxy headers when `trusted_proxies` is set. Further downloads fail with `503 TRANSFER_CAPACITY` until one finishes. See [Configuration](configuration.md#field-reference).

## Usage instructions sandbox

Administrators write usage instructions in Markdown, HTML and JavaScript, and may load external resources. These instructions are shown inside a sandboxed frame:

- The frame has no same-origin privileges (an opaque origin).
- Scripts and external resources run.
- Scripts cannot read the admin session, cannot access the parent page, and cannot call RedApp APIs with the visitor's credentials.
- The frame reports its height to the parent page with `postMessage`. No other data is exchanged.

Instructions still run arbitrary code in the visitor's browser. Only publish scripts and external resources that you trust.

## Imported configuration

Configuration packages may come from another administrator or another organization.

- The import preview shows instruction changes as source text. It never renders or runs them.
- If a package changes usage instructions, you must explicitly confirm that you trust them before the import runs.
- Packages cannot contain release distribution rules, signing keys or trust settings. Such packages are rejected.
- Imported vendors and applications start disabled.
- Proxy credentials and private notes are only exported when the exporting administrator selects them.

## Uploads

### Icons

| Rule | Value |
| --- | --- |
| Formats | PNG, JPEG, static SVG |
| Upload size | 2 MiB |
| Raster dimensions | Up to 4096 pixels per side and 4,194,304 pixels in total |

- PNG and JPEG images are decoded and re-encoded, which removes metadata.
- SVG files are checked against an allowlist of shapes, gradients and presentation attributes. Scripts, event handlers, external references, foreign elements and entities are rejected, not stripped.
- Icons are served with a restrictive Content Security Policy.

### Hosted files

- The upload size limit is `max_artifact_bytes` (4 GiB by default).
- URL import accepts `http` and `https` URLs without credentials. It follows at most 4 redirects and never from HTTPS to HTTP. Credentials and cookies are dropped on redirect.
- Hosted files and HTTP cache files are sent as attachments with `X-Content-Type-Options: nosniff` and a sandboxing Content Security Policy, so a browser does not run them as pages.

## Upstream verification

| Provider | Verification |
| --- | --- |
| Claude Code | Release manifest signed with a pinned Anthropic OpenPGP key (RSA 4096, SHA-512); each binary checked against the SHA-256 in the signed manifest |
| Codex | Each release file checked against the SHA-256 digest in the release metadata |
| HTTP cache | No publisher signature. A local SHA-256 detects storage corruption only. |

Common rules for all upstream traffic:

- TLS certificates are always verified. There is no option to skip verification.
- A downloaded file is published only after its full size and checksum are verified. Resumed downloads are verified again.
- Upstream responses must not use content encoding, so checksums apply to the exact bytes served.
- Release providers follow redirects only within the configured source. HTTPS is never downgraded to HTTP.
- The HTTP cache may follow redirects to other HTTPS hosts. It drops credentials and cookies when the host changes.
- A redirect to another host can only reach the same kind of address as the configured source: a source on the public internet can only redirect to public addresses, and an intranet source only to intranet addresses. The source host itself may be on the intranet. With an outbound proxy, the proxy resolves host names and this check does not apply.
- Clients cannot choose the upstream host; they can only request paths below the configured source.

Choosing a mirror as the source does not make arbitrary files trusted. Codex and Claude Code files from a mirror must still pass the same checks.

## Outbound proxy credentials

- Proxy URLs may contain a user name and password.
- APIs never return the stored password. The console shows it redacted, and saving without changing it keeps the stored password.
- Public pages, logs and events never contain proxy credentials.
- The credentials are stored in the database. Protect the data directory and its backups.

## Reverse proxy trust

- Forwarding headers are believed only from peers in `trusted_proxies`.
- Malformed forwarding headers from a trusted peer are rejected with `400`.
- RedApp checks only the syntax of `Host`. Enforce allowed domain names at the proxy.
- The public address setting only affects generated links. It does not grant or restrict access.

See [Operations](operations.md#reverse-proxy) for a configuration example.

## Installer integrity

RedApp serves the official Codex and Claude Code installers with minimal patches:

- The original installers are stored unchanged in the repository, together with their licenses.
- Patches are minimal: they redirect downloads to your RedApp server and keep the upstream verification steps.
- Every installer update must apply its patches exactly and pass offline checks before it is accepted.
- Installer downloads come from RedApp. Codex and Claude Code still contact their own services when they run.

The Claude Code installer also installs a launcher that disables the client's self-update, so updates come through RedApp.
