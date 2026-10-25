import { describe, expect, it } from "vitest";
import { validNavigationURL } from "./validation";

describe("navigation URLs", () => {
  it.each([
    "/legacy/",
    "/tools/a.zip?x=1#download",
    "https://example.com/docs",
    "http://localhost:8080/",
  ])("accepts %s", (url) => {
    expect(validNavigationURL(url)).toBe(true);
  });
  it.each([
    "javascript:alert(1)",
    "data:text/html,x",
    "//evil.test",
    "/%2fevil.test",
    "/%5cevil.test",
    "https://user:pass@example.com",
    "https:example.com",
    "/bad%0apath",
    "/bad%20path",
    "relative",
    "/bad%zz",
    "/%ff",
  ])("rejects %s", (url) => {
    expect(validNavigationURL(url)).toBe(false);
  });
});
