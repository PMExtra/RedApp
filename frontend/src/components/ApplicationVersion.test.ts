import { mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import ApplicationVersion from "./ApplicationVersion.vue";
import { boot } from "../testSupport";
import { localDate, setLanguage } from "../i18n";
afterEach(() => { vi.useRealTimers(); setLanguage("en"); });
it("shares localized discovery time, exact keyboard tooltip, and live refresh without fetching", async () => {
  vi.useFakeTimers();
  const now = Date.parse("2026-10-06T12:00:00Z");
  vi.setSystemTime(now);
  const fetchSpy = vi.spyOn(globalThis, "fetch");
  const app = { ...boot.apps[0]!, latest_known_version: { version: "1.10.0", first_seen: new Date(now - 59000).toISOString() } };
  const w = mount(ApplicationVersion, { props: { app } });
  expect(w.text()).toContain("1.10.0");
  expect(w.get("time").text()).toBe("Just now");
  expect(w.get("time").attributes("tabindex")).toBe("0");
  expect(w.get('[role="tooltip"]').text()).toBe(localDate(app.latest_known_version.first_seen));
  await vi.advanceTimersByTimeAsync(30000);
  expect(w.get("time").text()).toBe("1 minute ago");
  setLanguage("zh-CN"); await w.vm.$nextTick();
  expect(w.get("time").text()).toBe("1分钟前");
  for (const [seconds, expected] of [[3600,"1小时前"],[86400,"1天前"],[604800,"1周前"],[2592000,"1个月前"],[31536000,"1年前"]] as const) {
    await w.setProps({ app: { ...app, latest_known_version: {version:"1.10.0",first_seen:new Date(Date.now()-seconds*1000).toISOString()} } });
    expect(w.get("time").text()).toBe(expected);
  }
  await w.setProps({ app: { ...app, latest_known_version: {version:"1.10.0",first_seen:null} } });
  expect(w.find("time").exists()).toBe(false);
  expect(w.text()).toContain("发现时间未知");
  await w.setProps({ app: { ...app, latest_known_version: null } });
  expect(w.text()).toBe("暂无已知版本");
  await w.setProps({ app: { ...app, capabilities: {...app.capabilities!,versions:false} } });
  expect(w.text()).toBe("");
  expect(fetchSpy).not.toHaveBeenCalled();
  w.unmount(); expect(vi.getTimerCount()).toBe(0); fetchSpy.mockRestore();
});
