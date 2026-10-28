# Mirroring other RedApp instances

[简体中文](mirrors.zh-CN.md)

A RedApp instance can use selected applications of another RedApp instance as their source. A typical setup has two levels: an instance in a DMZ reaches the official sources, and an internal instance reaches only the DMZ instance. Clients use only the internal instance; its pages, installers and download addresses are its own.

Mirroring works per application: you choose which applications to mirror, and applications added to the upstream later are not added automatically. A mirror only ever contacts its upstream, never the official source. Several levels are allowed (A → B → C), and an instance can mirror applications of several upstreams while also serving its own applications.

## Add an upstream

Open **Upstreams** in the administration and add the address of the other instance, for example `https://dmz.example`. The address is `http(s)://host[:port]` without a path: RedApp serves from the root of its host.

RedApp reads the identity of the other instance before saving. The upstream must run a RedApp version that supports mirroring. An instance cannot be its own upstream. Requests to an upstream's API use the global proxy; file downloads use the proxy of the mirrored application, as for any application.

## Mirror applications

Open an upstream to browse the applications it publishes, select the ones to mirror and confirm the local vendor and application IDs (by default the same as on the upstream). A vendor that does not exist locally is created from the upstream's vendor.

| Upstream application | Local application |
| --- | --- |
| Application info | Application info |
| Hosted files | HTTP cache with the upstream's directory listing setting |
| HTTP cache | HTTP cache |
| Codex, Claude Code | The same provider, with RedApp's own installers |
| JSON release | JSON release that reads the upstream's download choices |

To make an existing local application come from the upstream, for example the built-in Codex application, choose the same local ID and confirm that the existing application is adopted. It must use the same provider. It keeps its local settings (proxy, categories, tags, cache rules, retention, prewarm); its display and source follow the upstream from then on, and it starts a new source epoch.

## What follows the upstream

- **Display**: the name, description, icon, usage instructions, directory listing setting and the default and hidden download packages of a JSON release follow the upstream. Each can be overridden per language in the application settings; resetting a field follows the upstream again. Usage instructions keep their template variables, so `{{base_url}}` and `{{app_url}}` show this instance's addresses.
- **Source**: the address of the upstream application. It cannot be changed in the application; change the upstream's address instead, which starts a new source epoch for every application mirrored from it.
- **Local settings**: everything else (proxy, categories, tags, cache rules, retention, prewarm) is local and never follows the upstream.

A JSON release mirror requests the upstream's download choices for each channel every 300 seconds and downloads every package from the upstream, verified against the upstream's SHA-256 digests. Because its release settings belong to the upstream, it has no prewarm selection of its own.

## Checks and states

RedApp checks every mirrored application every 15 minutes (the first check runs 15 minutes after start), and **Check now** in the application settings checks one at once.

| State | Meaning |
| --- | --- |
| Up to date | The last check succeeded. |
| Error | The last check failed (for example `UPSTREAM_UNAVAILABLE`). The application keeps its last values and keeps serving. |
| No longer published | The upstream does not publish the application any more. It keeps its last values; the settings show that its template is missing. |

A check stops updating an application, and reports `UPSTREAM_INCOMPATIBLE`, when the upstream's address now serves another instance, when the upstream application changed its provider, or when the upstream sends values this instance does not accept. Save the upstream again to verify a moved instance.

## Several levels and loops

A mirrored application can itself be mirrored. Each instance passes on the chain of instances its application comes from, and RedApp refuses to mirror an application whose chain contains itself (`LOOP_DETECTED`), both when mirroring and at every check. A request that an application sends to itself, because its source leads back to it, is refused with `508 LOOP_DETECTED`.

Freshness does not accumulate across levels: HTTP cache files carry the freshness they have left, so a downstream cache never keeps a file longer than its upstream does.

## Failures

When an upstream is unreachable, files already cached locally are still served and release channels keep their last valid state; files that are not cached yet fail. An application the upstream stops publishing is not disabled locally: decide whether to keep, disable or delete it.

## Stop mirroring

**Stop mirroring** in the application settings turns the application into an ordinary one that keeps its current values, including its source address. An application with a built-in template is linked to it again, with the current values kept as overrides. An upstream can only be deleted when no application is mirrored from it.

Exported configuration contains mirrored applications as independent copies of their current values; it does not contain upstreams or mirror bindings. A mirrored application can only be copied as an independent copy, and an import cannot overwrite it.
