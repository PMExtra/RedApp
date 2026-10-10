import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/vue";
import { afterAll, afterEach, beforeAll } from "vitest";
import { configureApi } from "@/shared/api";
import { settleConfirm } from "@/shared/lib/confirm";
import { activeToasts } from "@/shared/lib/toast";
import { server } from "./msw";

beforeAll(() => {
  server.listen({ onUnhandledFrame: "error" });
});

afterEach(() => {
  cleanup();
  settleConfirm(false);
  activeToasts().value = [];
  server.resetHandlers();
  configureApi({});
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

afterAll(() => {
  server.close();
});
