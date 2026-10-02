import { mount } from "@vue/test-utils";
import { describe, it, expect, vi, afterEach } from "vitest";
import InstallCommands from "./InstallCommands.vue";
afterEach(() => vi.unstubAllGlobals());
describe("Install commands", () => {
  it("renders separate parameter-free commands and copies exact text", async () => {
    vi.stubGlobal("isSecureContext", true);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    const wrapper = mount(InstallCommands, {
      props: { origin: "https://redapp.example:8443" },
    });
    expect(wrapper.findAll("code").map((item) => item.text())).toEqual([
      "curl -fsSL 'https://redapp.example:8443/install.sh' | sh",
      "irm 'https://redapp.example:8443/install.ps1' | iex",
    ]);
    for (const button of wrapper.findAll("button"))
      await button.trigger("click");
    expect(writeText.mock.calls.map((call) => call[0])).toEqual(
      wrapper.findAll("code").map((item) => item.text()),
    );
    expect(wrapper.text()).toContain("Command copied");
    await wrapper.setProps({ origin: "https://other.example" });
    expect(wrapper.find("code").text()).toContain("https://other.example/");
  });
  it("shows clipboard errors without injecting HTML", async () => {
    vi.stubGlobal("isSecureContext", true);
    Object.defineProperty(navigator, "clipboard", {
      value: {
        writeText: vi.fn().mockRejectedValue(Error("Permission denied")),
      },
      configurable: true,
    });
    const wrapper = mount(InstallCommands, {
      props: { origin: "<img src=x onerror=alert(1)>" },
    });
    await wrapper.find("button").trigger("click");
    await vi.waitFor(() =>
      expect(wrapper.text()).toContain("Copy unavailable"),
    );
    expect(wrapper.find("img").exists()).toBe(false);
  });
  it.each([
    { secure: false, clipboard: { writeText: vi.fn() } },
    { secure: true, clipboard: undefined },
  ])(
    "hides copy buttons when copying is unavailable: %j",
    ({ secure, clipboard }) => {
      vi.stubGlobal("isSecureContext", secure);
      Object.defineProperty(navigator, "clipboard", {
        value: clipboard,
        configurable: true,
      });
      const w = mount(InstallCommands, {
        props: { origin: "http://internal" },
      });
      expect(w.find("button").exists()).toBe(false);
      expect(w.findAll('pre[tabindex="0"]')).toHaveLength(2);
      expect(w.text()).not.toContain("HTTPS or localhost");
      w.unmount();
    },
  );
});
