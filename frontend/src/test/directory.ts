import userEvent from "@testing-library/user-event";
import { bootstrap, session } from "./factories";
import { providers } from "./factories/directory";
import { mockApi, useHandlers } from "./msw";
import { renderEntry } from "./render";

/**
 * Renders the admin entry at `path` as a signed-in administrator, with the
 * bootstrap, session and provider list answered. Install the page's own
 * handlers with `useHandlers` before calling.
 */
export async function renderAdminPage(path: string) {
  useHandlers(
    mockApi("get", "/api/bootstrap", () => bootstrap()),
    mockApi("get", "/admin/api/session", () => session()),
    mockApi("get", "/admin/api/providers", () => providers()),
  );
  const rendered = await renderEntry("admin", path);
  return { ...rendered, user: userEvent.setup() };
}

/** Collects the JSON bodies (and If-Match headers) a handler receives. */
export function recorder<T = unknown>() {
  const calls: { body: T; ifMatch: string | null; url: URL }[] = [];
  return {
    calls,
    async record(request: Request): Promise<T> {
      const text = await request.text();
      const body = (text ? JSON.parse(text) : undefined) as T;
      calls.push({ body, ifMatch: request.headers.get("If-Match"), url: new URL(request.url) });
      return body;
    },
  };
}
