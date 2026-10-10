import type { ConsoleMessage, Page } from "@playwright/test";

// Signed-out visitors get 401 from the session probe (getSession) by contract;
// the browser reports every 4xx resource as a console error.
function expectedFailure(message: ConsoleMessage): boolean {
  const { url } = message.location();
  return (
    message.text().startsWith("Failed to load resource") &&
    message.text().includes("status of 401") &&
    URL.canParse(url) &&
    new URL(url).pathname === "/admin/api/session"
  );
}

/** Collects console errors and uncaught exceptions (including CSP violations); tests expect none. */
export function collectConsoleErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" && !expectedFailure(message)) errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  return errors;
}
