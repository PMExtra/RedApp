# RedApp frontend

Vue 3, Vue Router, Vite and TypeScript provide the public and administrator SPA. Production assets are embedded by Go; the runtime does not require Node, a CDN, SSR or a separate frontend server. `main.ts` imports the synchronous locale initializer before mounting the router.

## Build and verification

Use Node 24.19.0 and the committed npm lockfile:

```sh
npm ci
npm run typecheck
npm test
npm run build
```

Vite uses `/` as its asset base and writes `internal/httpserver/web`. Rebuild and commit those generated files with frontend changes; CI verifies the embedded output. `npm run dev` is a loopback source preview, not an authenticated deployment: API calls are same-origin. Run CLI HTTP integration against the embedded Go build.

The DOM suite uses Vitest and happy-dom. It exercises canonical navigation, translated rendering and commands, session/CSRF behavior, visibility-aware polling, dirty drafts, conflict responses, failed application switches, late requests, cleanup previews, proxy credential actions, public URL precedence, and the distinct dropdown keyboard models. These checks do not claim browser layout, screen-reader, touch-device or native installer verification.

The obsolete v0.5 browser fixture was removed in v0.6.1: its database tables, routes and metric expectations no longer match this SPA. Current acceptance uses the DOM suite and real CLI HTTP tests. Historical screenshots do not validate the current SPA.

Vue Router is pinned to 4.6.4, compatible with Vue 3.5.43. Its history/router API is sufficient for the explicit route table; file-based routing, plugin loaders and generated route schemas are not used. Other library versions remain pinned in the lockfile.

## Routes and data ownership

`App.vue` owns public bootstrap refreshes. `PublicLayout` and `AdminLayout` own stable page chrome; route components own their forms or status subscriptions. Internal navigation uses `RouterLink`; downloads and external links remain normal anchors.

| UI route | Owner |
| --- | --- |
| `/` | Public application directory |
| `/<vendor>/<app>` | Descriptor-driven application instructions |
| `/admin/login` | Administrator sign-in |
| `/admin/overview` | Global metrics and history |
| `/admin/events` | Structured failure details |
| `/admin/settings/site` | Independent site appearance and public URL forms |
| `/admin/settings/proxy` | Global upstream proxy form |
| `/admin/apps/<vendor>/<app>/versions` | Application metrics/history, versions and resources |
| `/admin/apps/<vendor>/<app>/settings` | Application channel TTL and cleanup |

The server serves the SPA only for recognized UI routes, including direct deep links. `/<vendor>/<app>/<file_path>` is a distribution route, not an SPA fallback. Application identity always includes both vendor and app; neither UI state nor API calls infer Codex from a missing identity.

`GET /api/bootstrap` supplies one snapshot of version, localized site text, application descriptors, effective public origin and revision. The frontend does not separately fetch legacy info or application lists during initialization. Descriptor fields supply application names, publisher, icon URL, shell runner, installer filenames, summary and update policy. Installation commands use the confirmed public origin plus canonical application ID.

Authenticated reads use `/admin/api/status` or `/admin/api/apps/<vendor>/<app>/status`. These responses contain summaries and metrics only. Versions, resources and events use independent cursor pages: application `/versions` and `/resources`, plus global `/events` (or an application `/events` when scoped). Each request uses `limit=50`; the server accepts at most 100. The response is `{items, next_cursor}`, with an omitted initial cursor and `null` for the final page. Previous/next navigation retains only the current items and cursor history. Selecting a version sends an encoded `version` filter to the resources endpoint and resets its cursor. Application/filter changes clear stale items immediately; failed initial requests do not render false empty states. History requests use `metric` and `range` query parameters: global history also requires `scope=global`; application history is selected by the explicit application path. No application-selection header is used. Resource identity uses its authorized `Application`, `Version` and `Key`, rather than display labels.

## Forms, sessions and polling

Global settings live at `/admin/api/settings/site`, `/settings/proxy` and `/settings/public-url`. Application TTL uses `/admin/api/apps/<vendor>/<app>/settings` with `channel_ttl_seconds`. Each GET returns `revision`; PUT carries `If-Match` and a settings-only body. A 409 preserves the draft with a localized conflict message. Successful responses replace the baseline. Navigating away or reloading a dirty draft asks before discarding it; nothing autosaves.

Settings pages do not fetch or poll status, and a failed status snapshot does not gate their forms. Changing an application's settings scope clears its old TTL immediately, cancels the old request and ignores late responses. A failed GET leaves saving disabled. Cleanup preview and execution retain the canonical app and frozen job identity; editing the threshold invalidates the preview.

Public URL priority is administrator override, then `REDAPP_PUBLIC_URL`, then the server's safe request origin. The form displays the effective value and its source. Clearing the input and saving sends `override_url: null`, exposing the environment fallback again. A successful save updates the bootstrap command origin immediately. Site text is plain text; the fixed RedApp source link is independent of custom branding.

Only visible overview, events and versions pages poll, with a five-second delay after each completed request. Summary and each displayed cursor page refresh independently; a failed summary does not block version/resource lists. The events page does not request status. Requests never overlap. Hidden pages, route departure, logout and 401 stop their subscription; stale responses cannot restore it. History collection on the server is independent of UI polling. Forms own their errors, so successful status requests cannot clear a save error.

The typed API client retains structured error `code`, `message`, `request_id` and `retryable` fields. Display messages use stable codes before HTTP status: settings revision conflicts preserve drafts, while invalid cleanup previews request a new preview. Unknown codes and older string errors have a generic status fallback; an arbitrary 409 is never labeled a settings conflict.

The auth module owns session/CSRF state. Each API request captures the current session generation and checks it, along with cancellation, after JSON parsing. A late 401 from an earlier session cannot expire a newer login or clear its CSRF token, even if token text is reused. Mutations use session cookies and CSRF headers; the server remains authoritative for authentication and origin checks. Password and proxy credential inputs remain in component memory and clear on completion or unmount. Stored credentials are never displayed. A transient session-check failure is shown without discarding an already authenticated form; a current-session 401 expires it.

## Localization and controls

Locale selection happens synchronously before first render: a saved explicit `en`/`zh-CN` choice, then the first supported entry in `navigator.languages`, then `navigator.language` only when the list is unavailable or empty, then English. Valid regional `zh-*` entries map to the existing Chinese pack. Automatic inference is never written as a manual preference. Storage failures affect persistence only, and `html.lang` follows the selected language immediately.

Custom site text loads asynchronously in a local header/footer skeleton. Bootstrap has a finite timeout and localized fallback/retry; unavailable commands are not synthesized from an unchecked browser origin. Site saves and bootstrap request tickets prevent late responses overwriting new state. The home directory retains the site brand and omits the former organization-download subtitle.

`usePopover` shares dismissal, ownership, IDs and focus return, with common trigger/panel CSS. `SelectMenu` retains combobox/listbox/option semantics, typeahead and focus on its trigger; `AccountMenu` retains menu/menuitem semantics and moves focus through actions. They do not pretend to be the same input type.

Metrics retain stable IDs and backend-defined scope. Application charts request application history explicitly; global charts request global history. The overview shows 16 common metrics and 25 initially collapsed diagnostic metrics. Collapsing diagnostics leaves polling, collection and history available; the two retired cards remain hidden even in an older status payload, while error event details remain accessible. Version labels distinguish global and single-application counts without assuming imported Codex history.

`HistoryDialog` and `historyChart` share descriptors for plotted values and exact point readouts. Hover, keyboard and touch show browser-local bucket time with an offset, base units and API precision, gap/coverage semantics, gauge/rate average-min-max or counter last/delta. UTC aggregation and the table remain explicit. Scope/range changes abort old requests and discard late results; language, mode and range changes clear the selection. Tests use DOM and mocked uPlot cursor events, not a GUI or physical input device. See [metric semantics](../docs/metrics-history.md).
