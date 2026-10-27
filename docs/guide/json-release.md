# JSON release distribution

The `json-release` Provider discovers verified packages from JSON APIs. Applications provide request configuration and a jq expression; adding an application using the same protocol does not require another Provider.

## Configuration and preview

Create a JSON Releases application with its `json_release` object, or use the bundled Visual Studio Code, IntelliJ IDEA, PyCharm and CLion templates. Edit the object in application settings. Preview fetches metadata and checksum sidecars, displays normalized packages and lets you select precise prewarm keys. Save applies the configuration with the normal revision check; preview alone does not publish or cache installers.

Each channel defines `requests` (an explicit matrix of URLs and string-valued `context`) and `query`. The query receives `{body, context}` separately for each request and must return exactly one object:

```json
{
  "version": "1.2.3",
  "build": "upstream-build-id",
  "artifacts": [{
    "key": "windows-x64-user-exe",
    "os": "windows",
    "arch": "x64",
    "format": "exe",
    "scope": "user",
    "url": "https://downloads.example/app/setup.exe",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "size": 123
  }]
}
```

This is illustrative data, not a real digest. `build`, `scope`, `variant` and `size` are optional. Each result in a matrix must have the same version/build; mixed platform releases are rejected. Keys must be unique and stable across versions, including distinct package formats and installation scopes.

OS values are `windows`, `macos`, `linux`, `unknown`; architectures are `x64`, `arm64`, `x86`, `arm`, `universal`, `unknown`. Map vendor aliases and nested fields in the expression. Unknown data must not be guessed. `format` and `variant` accept bounded lowercase identifiers, so package types need no new enum.

A checksum may instead use `checksum_url` and `checksum_filename`. The engine accepts a bare SHA256, or SHA256 checksum lines with an exact filename match; conflicting or ambiguous values fail. If both a digest and sidecar are supplied, they must agree. No package is authorized without an expected SHA256.

## Sources and resource limits

`sources.metadata`, `sources.artifacts` and `sources.checksums` contain explicit `{origin, prefix}` rules. An origin is an HTTP(S) origin without credentials or query; a prefix is a directory such as `/products/`. Authorization applies separately to initial URLs and every redirect. Metadata does not authorize arbitrary URLs in its response. Existing proxy scope, TLS and resolved-address restrictions remain active. HTTPS cannot redirect to HTTP; cross-origin redirects require HTTPS. A configured proxy owns destination DNS policy.

Queries are trusted administrator configuration. They have no installed environment loader, external module loader or custom network/file/process functions. Limits include 32 requests per channel, 128 artifacts, 4 MiB total metadata/output, 16 KiB query text, 64 KiB per sidecar (1 MiB total), 30 seconds discovery and a 2-second query context with two concurrent queries. Cancellation and input/output caps **do not hard-limit intermediate memory or preempt every operation**. Preview is not a sandbox. Strict isolation is tracked in [issue #18](https://github.com/PMExtra/RedApp/issues/18).

## Refresh, prewarm and download links

`refresh_seconds` is 60–86400; a minute-based scheduler checks due channels independently of retention. Failed refreshes record a failure and preserve the last verified snapshot. Version metadata, authorized resources and channel binding commit atomically. Changed sources, queries or channels create a new source epoch; changing only the interval or prewarm selection preserves cached artifacts.

`prewarm` is an array of `{channel, keys}`. It runs after metadata publication through the existing bounded prewarm worker. Empty selection disables automatic prewarm. Worker capacity or download failure does not invalidate metadata; subsequent refreshes can retry. Packages are pinned to that refresh's immutable version, with no architecture or old-version fallback.

- `/{vendor}/{app}/download/latest/{key}` resolves the configured default channel.
- `/{vendor}/{app}/download/stable/{key}` resolves the declared stable channel.
- Both return a non-cacheable temporary redirect to `/{vendor}/{app}/releases/{release_id}/{key}` on RedApp.
- `/api/apps/{vendor}/{app}/downloads?channel=latest` returns normalized options and local URLs from one snapshot.

The public selector makes a best-effort recommendation using browser information. Uncertain architectures or multiple package types require manual choice; Linux package managers are not guessed. Manual selection takes precedence.

Opaque release IDs identify version/build and are not semantically sortable. This initial Provider does not enable the older installer-specific automatic retention policy; it must not delete versions by sorting their digest IDs.

## Presets and update policy

VS Code uses per-platform update JSON: `productVersion` is the display version, `version` the upstream build/commit and `sha256hash` the digest. JetBrains uses product release JSON and maps supported entries in `downloads`; library metadata is excluded. The configured product codes are `IIU` for IDEA, `PCP` for PyCharm and `CL` for CLion (package and checksum paths under `/cpp/`). These endpoint choices remain subject to the live-verification limitation below. Other product codes can reuse the configuration.

The templates are covered by synthetic mapping fixtures. The official endpoints and JetBrains checksum sidecars were not reachable in the implementation environment; their current formats and every platform's availability are **not live-verified**. Run preview in your own network before enabling prewarm. A changed CDN or checksum format fails closed and requires an explicit configuration correction.

Serving an original IDE package does not modify its installed settings, launcher or official updater. These templates do not disable IDE update checks or redirect them to RedApp. No stable installation-time adaptation has been verified for these raw-package flows; administrators must account for this boundary rather than assume downloads control runtime updates.

### Candidate inventory

The 12 JSON Release presets are VS Code, IntelliJ IDEA, PyCharm, CLion, WebStorm, PhpStorm, GoLand, Rider, DataGrip, RubyMine, RustRover and dotUltimate. New presets start disabled with no prewarm selection. They are synthetic-fixture-backed candidates, **not live-verified integrations**. MPS, DataSpell, AppCode, Fleet, Aqua, Writerside, Gateway and Toolbox are not included.

| JetBrains candidate | Configured product code | Artifact/checksum prefix |
| --- | --- | --- |
| IDEA / PyCharm / CLion | IIU / PCP / CL | /idea/ /python/ /cpp/ |
| WebStorm / PhpStorm / GoLand | WS / PS / GO | /webstorm/ /webide/ /go/ |
| Rider / DataGrip / RubyMine | RD / DG / RM | /rider/ /datagrip/ /ruby/ |
| RustRover / dotUltimate | RR / RSU | /rustrover/ /resharper/ |

These codes and paths are configuration candidates, not fresh official observations. Preview must validate metadata, platform availability, CDN redirects and sidecars before enablement. CI fixtures do not resolve that release blocker.

dotUltimate is a Windows .NET suite, not an independent IDE, and coexists with standalone Rider. Its `RSU` code and `downloads.windowsOffline` field are **unverified fixture assumptions**. The candidate rejects ordinary `windows` web-installer entries and unsupported formats; do not enable it until the actual offline metadata and SHA256 source have been confirmed and the configuration corrected if needed. The complete offline ZIP is one artifact, preserving its EXE and .dat together; it is never unpacked or duplicated for each suite component. An explicitly identified offline EXE is also representable for older formats. Unknown architecture remains unknown.

## Storage and deployment

The source snapshot and refresh-status additions use schema 16. Schema 15 is upgraded in place using a pre-migration database backup and a verified transaction; other unsupported schemas are refused unchanged. Blob paths and contents do not change. See [operations](operations.md#data-directory) for space requirements and whole-directory backup/rollback guidance. This development build still requires full validation before deployment. No new mandatory environment variable, worker container or host privilege is introduced. Align the eventual image and supported upgrade path in stacks separately; this implementation does not modify stacks or deploy anything.
