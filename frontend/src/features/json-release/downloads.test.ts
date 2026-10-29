import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Schema } from "@/shared/api";
import { mockApi, useHandlers } from "@/test/msw";
import { renderWithApp } from "@/test/render";
import ReleaseDownloadButton from "./ReleaseDownloadButton.vue";
import ReleaseVersionPicker from "./ReleaseVersionPicker.vue";

const choice = (
  key: string,
  os: "windows" | "linux",
  arch: "x64" | "arm64",
  format: string,
  scope: string,
  hidden = false,
): Schema<"ReleaseDownloadChoice"> => ({
  key,
  os,
  arch,
  format,
  scope,
  url: `/microsoft/vscode/releases/fixed/${key}`,
  path: `releases/fixed/${key}`,
  sha256: "b".repeat(64),
  size: 2048,
  hidden,
});
const current: Schema<"ReleaseDownloads"> = {
  stale: true,
  channel: "stable",
  release_id: "r-" + "a".repeat(64),
  version: "1.2.3",
  build: "abc",
  artifacts: [
    choice("win-user", "windows", "x64", "exe", "user"),
    choice("win-system", "windows", "x64", "exe", "system"),
    choice("linux-deb", "linux", "arm64", "deb", "system"),
    choice("linux-rpm", "linux", "arm64", "rpm", "system", true),
  ],
  default_artifact: "win-system",
};
const older: Schema<"ReleaseDownloads"> = {
  ...current,
  stale: false,
  channel: "",
  release_id: "r-" + "c".repeat(64),
  version: "1.0.0",
  artifacts: [choice("win-user", "windows", "x64", "exe", "user")],
  default_artifact: null,
};
const history: Schema<"ReleaseSummary">[] = [
  {
    release_id: current.release_id,
    version: "1.2.3",
    build: "abc",
    discovered_at: "2026-10-01T00:00:00Z",
    channels: ["stable"],
  },
  {
    release_id: older.release_id,
    version: "1.0.0",
    build: "",
    discovered_at: "2026-10-20T00:00:00Z",
    channels: [],
  },
];

beforeEach(() => {
  // Windows without architecture hints, as Firefox reports it.
  vi.spyOn(navigator, "userAgent", "get").mockReturnValue(
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0",
  );
  useHandlers(
    mockApi("get", "/api/apps/{vendor}/{app}/downloads", ({ request }) =>
      new URL(request.url).searchParams.get("release") === older.release_id ? older : current,
    ),
    mockApi("get", "/api/apps/{vendor}/{app}/releases", () => ({
      items: history,
      truncated: false,
    })),
  );
});
afterEach(() => vi.restoreAllMocks());

describe("release download button", () => {
  it.each(["en", "zh-CN"] as const)(
    "downloads the default among the packages for the system (%s)",
    async (locale) => {
      await renderWithApp(ReleaseDownloadButton, {
        props: { vendor: "microsoft", app: "vscode" },
        locale,
      });
      const main = await screen.findByRole("link", { name: /Windows · x64 · exe · system/ });
      expect(main).toHaveAttribute("href", "/microsoft/vscode/releases/fixed/win-system");
    },
  );

  it("lists the other visible packages, never hidden ones", async () => {
    await renderWithApp(ReleaseDownloadButton, { props: { vendor: "microsoft", app: "vscode" } });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Other packages" }));
    const menu = await screen.findByRole("menu");
    const items = within(menu).getAllByRole("menuitem");
    expect(items.map((item) => item.getAttribute("href"))).toEqual([
      "/microsoft/vscode/releases/fixed/win-user",
      "/microsoft/vscode/releases/fixed/win-system",
      "/microsoft/vscode/releases/fixed/linux-deb",
    ]);
    expect(within(menu).getByText("Version 1.2.3")).toBeInTheDocument();
    expect(within(menu).getAllByText("2.00 KiB")).toHaveLength(3);
  });

  it("offers the packages of a chosen release", async () => {
    await renderWithApp(ReleaseDownloadButton, {
      props: { vendor: "microsoft", app: "vscode", release: older.release_id },
    });
    // The old release lacks the default package: the system's package wins.
    expect(await screen.findByRole("link", { name: /Windows · x64 · exe · user/ })).toHaveAttribute(
      "href",
      "/microsoft/vscode/releases/fixed/win-user",
    );
  });
});

describe("release version picker", () => {
  it("shows the default release and switches to a discovered one", async () => {
    const update = vi.fn();
    await renderWithApp(ReleaseVersionPicker, {
      props: {
        vendor: "microsoft",
        app: "vscode",
        latest: { version: "1.2.3", first_seen: "2026-10-01T00:00:00Z" },
        modelValue: undefined,
        "onUpdate:modelValue": update,
      },
    });
    const user = userEvent.setup();
    const badge = await screen.findByRole("button", { name: /Version 1.2.3/ });
    expect(await screen.findByText("May be outdated")).toBeInTheDocument();
    await user.click(badge);
    const options = await screen.findAllByRole("menuitemradio");
    expect(options.map((option) => option.textContent)).toEqual([
      expect.stringContaining("1.2.3"),
      expect.stringContaining("1.0.0"),
    ]);
    const [first, second] = options;
    if (!first || !second) throw new Error("two versions expected");
    expect(first).toHaveAttribute("aria-checked", "true");
    expect(within(first).getByText("stable")).toBeInTheDocument();
    await user.click(second);
    await waitFor(() => expect(update).toHaveBeenCalledWith(older.release_id));
  });

  it("returns to the default release with undefined", async () => {
    const update = vi.fn();
    await renderWithApp(ReleaseVersionPicker, {
      props: {
        vendor: "microsoft",
        app: "vscode",
        latest: { version: "1.2.3", first_seen: null },
        modelValue: older.release_id,
        "onUpdate:modelValue": update,
      },
    });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /Version 1.0.0/ }));
    expect(screen.queryByText("May be outdated")).not.toBeInTheDocument();
    const [first] = await screen.findAllByRole("menuitemradio");
    if (!first) throw new Error("versions expected");
    await user.click(first);
    await waitFor(() => expect(update).toHaveBeenCalledWith(undefined));
  });
});
