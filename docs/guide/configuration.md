# Configuration

[简体中文](configuration.zh-CN.md)

RedApp has two kinds of configuration:

- **Deployment configuration** controls the process: listen address, data directory, trusted proxies and download limits. It comes from a file, environment variables and command-line flags, and is read once at startup.
- **Runtime configuration** covers everything else: vendors, applications, site texts, public address and outbound proxy. Administrators edit it in the web console. It is stored in the data directory and takes effect without a restart.

## Deployment configuration

No configuration file is required. Every field has a built-in default.

### Choosing a configuration file

RedApp reads at most one file. The first match wins:

1. `--config FILE`
2. The `REDAPP_CONFIG` environment variable (an empty value counts as unset)
3. `/etc/redapp/config.yaml`

The default file may be absent. A file selected with `--config` or `REDAPP_CONFIG` must exist. A selected file that is missing, unreadable or invalid stops startup.

RedApp does not search the working directory for other files.

### File format

- The file is YAML, whatever its extension.
- The file must be a single mapping of at most 64 KiB.
- Unknown fields, duplicate keys, `null` values, multiple YAML documents, anchors, aliases, merge keys and custom tags are rejected.
- Integers must be plain numbers. Only `max_artifact_bytes` also accepts a size string.

Example: [config/example.yaml](../../config/example.yaml).

```yaml
schema_version: 1
listen: ':8080'
data_dir: /var/lib/redapp
trusted_proxies: []
download_limits:
  max_writers: 16
  max_readers: 512
  max_artifact_bytes: '4GiB'
```

### Precedence of values

For each field, the highest source wins:

1. Command-line flags
2. Environment variables
3. The selected file
4. Built-in defaults

Lists replace lower-priority lists; they are not merged. Every source is validated on its own, so a higher source cannot hide an invalid value in a lower one.

### Field reference

| Field | Environment variable | Flag | Default | Allowed values |
| --- | --- | --- | --- | --- |
| `schema_version` | — | — | `1` | Only `1` |
| `listen` | `REDAPP_LISTEN` | `--listen` | `:8080` | `host:port`; host is empty, `localhost` or an IP address; port 1–65535 |
| `data_dir` | `REDAPP_DATA` | `--data` | `/var/lib/redapp` | Absolute path |
| `trusted_proxies` | `REDAPP_TRUSTED_PROXIES` | `--trusted-proxies` | empty | Up to 128 CIDRs; comma-separated in environment and flag |
| `download_limits.max_writers` | `REDAPP_MAX_WRITERS` | `--max-writers` | `16` | 1–1024 |
| `download_limits.max_readers` | `REDAPP_MAX_READERS` | `--max-readers` | `512` | 1–65536 |
| `download_limits.max_artifact_bytes` | `REDAPP_MAX_ARTIFACT_BYTES` | `--max-artifact-bytes` | 4 GiB | 1 byte to 1 TiB |
| — | `REDAPP_PUBLIC_URL` | — | empty | HTTP(S) origin; see [Public address](#public-address) |

Notes:

- `schema_version` is the deployment file format. It is unrelated to the database schema.
- An empty `REDAPP_TRUSTED_PROXIES` or `--trusted-proxies` clears the list.
- `max_writers` limits concurrent upstream fetches across all applications. HTTP cache fetches and upstream `HEAD` requests also use a writer slot.
- `max_readers` limits concurrent downloads being served, including cache hits, `HEAD` and `304` responses.
- When either limit is full, the request fails with `503`. There is no waiting queue.
- `max_artifact_bytes` limits the size of **each** downloaded file. It is not a total cache quota.
- `REDAPP_PUBLIC_URL` is not a deployment field. It only supplies a default for the public address setting. An invalid non-empty value stops startup.

### Byte sizes

`max_artifact_bytes` accepts a plain integer (bytes) or a size string. Units are case-insensitive and decimals are allowed.

| Value | Bytes |
| --- | --- |
| `4294967296` | 4,294,967,296 |
| `4GiB` | 4,294,967,296 (2^30 per GiB) |
| `4GB` or `4gb` | 4,000,000,000 (10^9 per GB) |
| `1.5GiB` | 1,610,612,736 |

Use explicit binary units such as `GiB` to avoid confusion. Do not use thousands separators. `max_writers` and `max_readers` do not accept units.

### Validating the configuration

`redapp config validate` resolves the configuration exactly as `serve` would and reports errors. It does not open or create the data directory.

```sh
redapp config validate
redapp config validate --config ./config.yaml
REDAPP_CONFIG=/srv/redapp/config.yaml redapp serve
redapp serve --config ./custom.yaml --listen 127.0.0.1:8081
```

`redapp healthcheck` uses the same resolution rules. If you change the configuration with flags, pass the same flags to the health check. See [Operations](operations.md#docker).

## Public address

The public address is the origin used in generated download links and install commands. It is chosen in this order:

1. The override saved on the **Site settings** page (`/admin/settings/site`)
2. `REDAPP_PUBLIC_URL`
3. The origin of the current request, derived from the `Host` header and trusted proxy headers

Clearing the override in the console falls back to `REDAPP_PUBLIC_URL`, if set. The settings page shows the effective address and its source.

Rules:

- Only `http://` and `https://` origins are accepted. One trailing `/` is allowed.
- Credentials, paths, query strings and fragments are rejected.
- Saving does not contact the address.
- Commands that users already copied are not rewritten.

The public address only affects generated links. It does not restrict which `Host` values RedApp accepts, and it does not change cookie security or upstream access. Configure allowed domain names at your reverse proxy.

## Trusted proxies

`trusted_proxies` lists the CIDRs of reverse proxies whose forwarding headers RedApp believes. Headers from any other peer are ignored.

- Set it to the address range of your reverse proxy only. Never include ranges of ordinary clients.
- Make the proxy overwrite, not append to, client-supplied `Forwarded` and `X-Forwarded-*` headers.
- The derived request origin decides same-origin checks for the admin console and whether the session cookie is marked `Secure`.

See [Operations](operations.md#reverse-proxy) for an example.

## Outbound proxy

RedApp can reach upstream sources through an HTTP, HTTPS or SOCKS5 proxy. The proxy is resolved per application in three levels: **global → vendor → application**.

| Scope | Modes | Where to set |
| --- | --- | --- |
| Global | `direct`, `url` | **Upstream proxy** page (`/admin/settings/proxy`) |
| Vendor | `inherit`, `direct`, `url` | Vendor settings |
| Application | `inherit`, `direct`, `url` | Application settings |

- `inherit` ("Use parent setting") uses the parent level, including when the parent is direct.
- `direct` connects without a proxy and stops inheritance.
- `url` uses a full proxy URL such as `http://proxy.example.internal:3128` or `socks5://user:pass@proxy.example.internal:1080`.

Proxy URL rules:

- The scheme is `http`, `https` or `socks5`, and an explicit port is required.
- Paths, query strings and fragments are not allowed.
- User name and password are optional and must be percent-encoded.
- The `HTTP_PROXY` and `HTTPS_PROXY` environment variables are ignored.

Behavior:

- With an `http` or `https` proxy, HTTPS upstreams are reached through `CONNECT`. With SOCKS5, the proxy resolves host names.
- Changing a parent never rewrites a child's own setting.
- A change applies to new upstream connections. Transfers already in progress finish on their old connection.
- Proxy passwords are never returned in full. The console shows them redacted. Saving without changing the password keeps the stored one.

## Presets and overrides

RedApp ships built-in presets for vendors, applications and categories. They are compiled into the binary.

- On startup, missing built-in vendors and applications are added **disabled**. Existing records are never overwritten.
- A preset-based vendor or application stores only the fields you changed (overrides). All other fields follow the preset and pick up preset updates when you upgrade RedApp.
- A value equal to the preset, an empty string and an empty list still count as your own override.
- English and Chinese texts are separate fields. Lists (sources, cache rules, categories, tags) and the proxy, prewarm and retention settings are each replaced as a whole.
- Built-in vendors and applications cannot be deleted. Disable them instead.
- A category cannot be renamed to a name that another category already uses in either language (ignoring case).

### Resetting a field

In the editor, a **Reset** button appears next to a field only when the field has an override or an unsaved edit.

1. Click **Reset**. The draft shows the preset value.
2. Click **Save**. The override is removed, and the field follows the preset again.

Reset does not change the enabled state, private notes, IDs or provider. It never deletes cached files, hosted files or history.

### Saving and conflicts

Every save carries the revision that the editor loaded. If someone else saved in between, the save is rejected and your draft is kept. Reload to see the latest values before saving again.

## Export, import and copy

Use these features to move configuration between RedApp instances or to duplicate an application.

### Export

Export creates a ZIP file from a vendor (optionally with its applications) or an application. Choose a mode:

- **Linked** keeps the preset reference and only your overrides.
- **Independent** writes the complete effective configuration without a preset reference.

| Included | Excluded |
| --- | --- |
| Selected vendor and application settings | Hosted files |
| Parent vendor of an exported application | Cached files, metric history, tasks |
| Names of referenced categories | Internal IDs, revisions, source epochs |
| Validated static icons | Enabled state |
| Private notes, only if selected | Admin accounts and sessions |
| Proxy credentials, only if selected | Credentials of parent proxy settings |

The two sensitive options (notes and proxy credentials) are off every time the dialog opens. If proxy credentials are excluded and a proxy URL contains them, the whole proxy setting is left out and marked as omitted.

### Import

Import accepts a ZIP file or a single YAML document.

1. Upload the file. RedApp shows a preview: what will be created, updated or skipped, with field differences.
2. Adjust the choices if needed, for example a target vendor or a new ID for a single application. Changing a choice produces a new preview.
3. If the package changes usage instructions, confirm that you trust them. Instructions can contain HTML and JavaScript; the preview only shows their source text and never runs them.
4. Execute. The whole import commits or nothing changes.

Rules:

- New vendors and applications are created **disabled**. Updated objects keep their enabled state and identity.
- Existing objects are skipped unless you choose to update them.
- Proxy settings that were omitted from the package keep the target's own setting. New objects must choose a proxy mode explicitly.
- Packages are rejected if they contain unknown files, path traversal, symbolic links, duplicate paths, release distribution or trust fields.

| Limit | Value |
| --- | --- |
| Upload size | 32 MiB |
| Total expanded size | 64 MiB |
| Logical entities | 1,000 |
| Each YAML document | 1 MiB |
| Each image | 2 MiB |

A preview expires after 10 minutes, and at most 8 previews are kept. Previews are lost on restart or when your session changes. After a successful import, the receipt stays readable for 24 hours, even after you sign in again. Executing the same import again returns the same receipt instead of importing twice.

### Copy

**Copy** on an application page creates a new application from an existing one, in the same or another vendor.

- The copy is disabled and has a new internal identity.
- No cached files, hosted files, history or tasks are copied.
- Private notes are copied only if selected.
- If the copy's proxy is `inherit`, it follows the target vendor.
- Copies of built-in applications can be deleted.
