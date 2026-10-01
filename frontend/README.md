# RedApp admin frontend

Vue 3, Vite and TypeScript provide a small, locally bundled administrator interface. Source is split into feature components and a typed API client. There is no CDN, SSR, Pinia, external frontend server, or Node dependency in the runtime image.

## Build and verify

Use Node 24.19.0 and npm:

```sh
npm ci
npm run typecheck
npm test
npm run build
```

From the repository root, `make frontend-test` and `make build` perform these steps as appropriate. Vite writes production files to `internal/httpserver/web`; those files are committed and embedded by Go. Rebuild after source changes. CI compares generated assets with the committed files. Docker rebuilds them in a Node build stage before compiling Go and retains a scratch runtime.

`npm run dev` serves UI source on loopback for developer use. API calls remain same-origin; use the embedded Go build for end-to-end tests rather than treating the development server as an authenticated deployment.

The lockfile pins Vue 3.5.43, Vite 8.3.2, plugin-vue 6.0.9, vue-tsc 3.3.11, Vitest 5.0.3, test-utils 2.5.1 and happy-dom 20.14.5, the stable versions verified against npm at implementation time. TypeScript is pinned to 6.0.3: current vue-tsc fails with TypeScript 7.0.2 (`typescript/lib/tsc` is no longer exported), so the latest compatible maintained stable branch is used and typechecking is preserved.

## UI and security

Overview, versions/resources, failures, and settings use native semantic controls and responsive CSS. Shell and PowerShell installation commands have separate copy buttons. The Clipboard API needs HTTPS/localhost and browser permission; failures offer manual copying. Default commands use CODEX_RELEASE/latest; version-pinned examples remain in administrator documentation.

API mutations retain session cookies, same-origin enforcement and CSRF tokens. Login expiration clears private state and stops polling. Requests are aborted on unmount, polling never overlaps, and failed refreshes retain the last successful snapshot with a visible error and retry control. Password changes sign out all sessions. Cleanup requires a server preview and explicit confirmation; editing its version invalidates the visible preview.

Proxy settings never display saved usernames/passwords. Administrators explicitly preserve, replace or clear credentials. Server-side validation and persistence remain authoritative. All user/upstream values render as text, not HTML; production assets comply with the existing self-only CSP without inline scripts/styles or eval.

Tests run through CLI using Vitest and happy-dom, including copying, clipboard failure, session/CSRF behavior, failed refresh recovery, polling cancellation, cleanup preview invalidation and proxy credential actions. This is DOM interaction verification, not browser/visual or Windows/macOS installer execution.
