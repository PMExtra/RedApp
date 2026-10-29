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
- Both return a non-cacheable temporary redirect to `/{vendor}/{app}/releases/{version}/{key}` on RedApp.
- `/api/apps/{vendor}/{app}/downloads?channel=latest` returns normalized options and local URLs from one snapshot.

The application page downloads from its header. The version badge opens the list of discovered releases; the download button offers one package of the chosen release, and its arrow lists the other visible packages. The offered package fits the visitor's system and, when the browser reports it, architecture: the default package if it fits; else, when the browser does not report the architecture, a universal or x64 package, which ARM Windows and Macs can also run; else the first fitting package by key. When nothing fits or the system is unknown, the default package is offered, else the first visible one. Detection is a suggestion; check the package before installing.

## Version history and lookup

Every release the application discovers stays available while its source epoch lasts: `/api/apps/{vendor}/{app}/releases` lists them, newest version first, and `/api/apps/{vendor}/{app}/downloads?version={version}` returns the packages of one. The public page lets visitors pick one. Version cleanup deletes cached files, not this history; a package downloaded again is verified against the same digest.

In the administration, the versions tab lists the discovered releases by version, each with its cached files; the cache tab removes cached files below a version, and the metrics tab shows the application's downloads. Prewarm is part of `json_release`.

An administrator can discover an older version in the versions tab. The optional `lookup` object has the shape of a channel. `{version}` in its request URLs (escaped) and context values is replaced by the version entered, and its query must return that version. A request answered with 404, or a query result of `null`, means the version has no such package; the version is not found only when no package remains. The discovered release is stored without moving any channel. Looking up a version RedApp already has returns it as stored, without asking the upstream. Changing `lookup` does not start a new source epoch, but its requests must stay inside `sources.metadata`.

The VS Code preset looks versions up at `/api/versions/{version}/<platform>/stable`, so its metadata source covers `/api/`. The JetBrains presets read the product's complete release list and select the version in the query. A mirror looks up the versions its upstream has discovered.

## Download choices

Two application settings, outside `json_release`, decide what the public page offers:

- `default_artifact`: the key of the package recommended when the visitor's environment does not pick one, for example `windows-x64-exe`. The presets set one: `win32-x64-user` for VS Code, `windows-x64-exe` for the JetBrains IDEs and `windows-offline-suite` for dotUltimate.
- `hidden_artifacts`: keys of packages not offered on the public page. Packages are listed as hidden, not as shown, so a package the upstream adds later is offered without a configuration change.

Both are presentation only. Hidden packages stay downloadable by their links, keep their prewarm selection and are still listed by `/api/apps/{vendor}/{app}/downloads` with `hidden: true`. Changing either keeps the source epoch and the cached packages. Keys need not exist in the current release; a default the release lacks is ignored. Each setting can be overridden on its own, so overriding them does not stop the application from following its template's `json_release`.

A release is identified by its version, which appears in its download paths; versions order by semantic versioning precedence, with any number of numeric parts: 1.10.0 is newer than 1.9.2, 2024.3.1 newer than 2024.3, and 2.0.0-rc.1 older than 2.0.0; build metadata after `+` does not count, and versions outside this form sort after the others; version cleanup keeps them and reports them as unknown. The build is shown but does not identify the release: when the upstream publishes another build of a version RedApp already knows, the refresh is refused as conflicting metadata and the known release keeps being served, until a new version appears or the source changes. JSON releases do not have the automatic version retention of Codex and Claude Code.

## Presets and update policy

VS Code uses per-platform update JSON: `productVersion` is the display version, `version` the upstream build/commit and `sha256hash` the digest. JetBrains uses product release JSON and maps supported entries in `downloads`; library metadata is excluded. The configured product codes are `IIU` for IDEA, `PCP` for PyCharm and `CL` for CLion (package and checksum paths under `/cpp/`). These endpoint choices remain subject to the validation boundaries below. Other product codes can reuse the configuration.

The templates have mapping fixtures, including a recorded official dotUltimate response and checksum sidecar. Official metadata and small sidecars can be validated without downloading installers; this does not verify package bytes or installation behavior. Run preview in your own network before enabling prewarm. A changed CDN or checksum format fails closed and requires an explicit configuration correction.

Serving an original IDE package does not modify its installed settings, launcher or official updater. These templates do not disable IDE update checks or redirect them to RedApp. No stable installation-time adaptation has been verified for these raw-package flows; administrators must account for this boundary rather than assume downloads control runtime updates.

### Candidate inventory

The 12 JSON Release presets are VS Code, IntelliJ IDEA, PyCharm, CLion, WebStorm, PhpStorm, GoLand, Rider, DataGrip, RubyMine, RustRover and dotUltimate. New presets start disabled with no prewarm selection. Official metadata and checksum sidecars have been checked; **full package downloads and installation remain unverified**. MPS, DataSpell, AppCode, Fleet, Aqua, Writerside, Gateway and Toolbox are not included.

| JetBrains candidate | Configured product code | Artifact/checksum prefix |
| --- | --- | --- |
| IDEA / PyCharm / CLion | IIU / PCP / CL | /idea/ /python/ /cpp/ |
| WebStorm / PhpStorm / GoLand | WS / PS / GO | /webstorm/ /webide/ /go/ |
| Rider / DataGrip / RubyMine | RD / DG / RM | /rider/ /datagrip/ /ruby/ |
| RustRover / dotUltimate | RR / RSU | /rustrover/ /resharper/ |

Preview must validate current metadata, platform availability, CDN redirects and sidecars before enablement. Recorded responses and CI fixtures do not replace full download and installation validation.

dotUltimate is a Windows .NET suite that coexists with standalone Rider. The official `RSU` metadata identifies the complete offline archive as `downloads.windowsZIP`. The preset requires that entry and a ZIP URL; it rejects missing ZIP metadata, web installers (`windowsWeb`, `windowsWebLP`), and the separate `toolboxWindows` package. The complete offline ZIP is one artifact, preserving its EXE and .dat together; it is never unpacked or duplicated for each suite component. Unknown architecture remains unknown. Metadata and SHA256 sidecars do not establish archive contents or offline installation behavior; verify those before enabling prewarm.

## Storage and deployment

Release metadata, refresh state and source snapshots live in the state database and upgrade with it; see [operations](operations.md#data-directory) for supported upgrades, space requirements and backups. Packages are ordinary cache files. JSON releases need no extra environment variable, container or host privilege.
