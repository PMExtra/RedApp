#!/usr/bin/env node
// Local fixture-only headless acceptance. Never installs Codex or contacts upstream.
"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const net = require("node:net");
const { spawn, execFileSync } = require("node:child_process");
const { chromium } = require(
  process.env.REDAPP_PLAYWRIGHT_MODULE || "playwright",
);
const root = path.resolve(__dirname, "..");
const data = fs.mkdtempSync(path.join(os.tmpdir(), "redapp-headless-"));
const artifacts =
  process.env.REDAPP_TEST_ARTIFACT_DIR ||
  fs.mkdtempSync(path.join(os.tmpdir(), "redapp-headless-artifacts-"));
fs.mkdirSync(artifacts, { recursive: true });
let processHandle,
  browser,
  password = "",
  base = "";
const failures = [],
  expectedFailures = [],
  checks = [];
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function stop() {
  if (processHandle && processHandle.exitCode === null) {
    processHandle.kill("SIGTERM");
    await new Promise((resolve) => processHandle.once("exit", resolve));
  }
}
async function start() {
  let log = "";
  processHandle = spawn(path.join(root, "bin/redapp"), [], {
    env: {
      ...process.env,
      REDAPP_DATA: data,
      REDAPP_LISTEN: new URL(base).host,
      REDAPP_PUBLIC_URL: "",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  processHandle.stderr.on("data", (chunk) => {
    log += chunk.toString();
  });
  for (let i = 0; i < 100; i++) {
    if (processHandle.exitCode !== null)
      throw Error("Fixture server exited before readiness");
    try {
      const response = await fetch(base + "/health/ready");
      if (response.ok) {
        const match = log.match(/Initial admin password: ([0-9a-f]+)/);
        if (match) password = match[1];
        return;
      }
    } catch {}
    await wait(50);
  }
  throw Error("Fixture server not ready");
}
function seed() {
  execFileSync(
    "python3",
    [
      "-c",
      String.raw`
import sqlite3,json,hashlib,datetime,pathlib,sys
p=pathlib.Path(sys.argv[1]);assert p.name.startswith('redapp-headless-')
db=sqlite3.connect(p/'state.sqlite');now=datetime.datetime.now(datetime.timezone.utc);stamp=lambda t:t.isoformat().replace('+00:00','Z')
body=b'fixture cached archive\n'*256;digest=hashlib.sha256(body).hexdigest();source='https://releases.openai.com/codex/releases/0.159.2/archive.tgz';rid=hashlib.sha256((source+'\0'+digest).encode()).hexdigest();generation='a'*32
archive=p/'objects'/rid/(generation+'.blob');archive.parent.mkdir(parents=True,exist_ok=True);archive.write_bytes(body)
resource={'ID':rid,'Source':source,'Hash':digest,'Size':len(body),'Labels':{'app':'codex','version':'0.159.2','name':'archive.tgz'}}
g={'ID':generation,'Resource':resource,'State':'complete','Path':str(archive),'Bytes':len(body),'Total':len(body),'Started':stamp(now-datetime.timedelta(seconds=2)),'Received':stamp(now-datetime.timedelta(seconds=1)),'Finished':stamp(now),'VerificationNS':1000000,'SourceBytes':len(body),'Resumes':0,'Retired':False,'Error':''}
for kind,key,value in [('generation',generation,g),('current',rid,{'Resource':rid,'Generation':generation})]:db.execute('INSERT OR REPLACE INTO records VALUES(?,?,?)',(kind,key,json.dumps(value)))
db.execute('INSERT OR REPLACE INTO versions VALUES(?,?)',('0.159.2',stamp(now-datetime.timedelta(days=3))))
db.execute('INSERT INTO events(time,resource,category,message) VALUES(?,?,?,?)',(stamp(now),'fixture-resource','upstream','Upstream HTTP 502'))
for key,n in [('artifact_requests',30),('cache_hit_requests',20),('miss_requests',10),('upstream_bytes',10000000),('downstream_bytes',20000000)]:db.execute('INSERT OR REPLACE INTO counters VALUES(?,?)',(key,n))
start=int(now.timestamp())//3600*3600-2*3600;end=int(now.timestamp())//60*60
for t in range(start,end+1,60):
 value=(8+(t-start)/3600)*1024**3
 db.execute('INSERT OR IGNORE INTO metric_samples VALUES(?,?,?,?,?,?,?)',('disk.cache_bytes',t,t,'fixture',value,None,0))
 db.execute('INSERT OR IGNORE INTO metric_samples VALUES(?,?,?,?,?,?,?)',('counters.upstream_bytes',t,t,'fixture',(t-start)*100,None if t==start else 6000,0 if t==start else 60))
db.execute('UPDATE metric_history_state SET aggregated_before=0');db.commit();db.close()
`,
      data,
    ],
    { stdio: "pipe" },
  );
}
async function screenshot(page, name) {
  assert(
    await page
      .locator("input[type=password]")
      .evaluateAll((inputs) => inputs.every((input) => !input.value)),
    "Refusing screenshot with populated password fields",
  );
  if (!name.includes("history"))
    await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: path.join(artifacts, name + ".png"),
    fullPage: !name.includes("history"),
  });
}
async function noOverflow(page) {
  assert(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
    "Page overflows viewport",
  );
  const buttons = page.getByRole("button");
  for (let i = 0; i < (await buttons.count()); i++) {
    const button = buttons.nth(i);
    if (await button.isVisible()) {
      const bounds = await button.boundingBox();
      assert(
        bounds && bounds.width > 0 && bounds.height > 0,
        "Invisible button geometry",
      );
    }
  }
}
async function fixtureAPI(page) {
  const r = await page.request.get(base + "/admin/api/proxy");
  assert(r.ok());
  return r.json();
}
async function selectMenu(page, scope, text) {
  await scope.getByRole("combobox").click();
  await scope.getByRole("option", { name: text, exact: true }).click();
}
async function dropdownKeyboard(page, scope) {
  const trigger = scope.getByRole("combobox");
  const initial = await trigger.textContent();
  for (let i = 0; i < 2; i++) {
    await trigger.focus();
    await page.keyboard.press("ArrowDown");
    await scope.getByRole("listbox").waitFor();
    assert(await trigger.evaluate((e) => e === document.activeElement));
    await page.keyboard.press("End");
    const active = await trigger.getAttribute("aria-activedescendant");
    assert.equal(
      await page.locator(`[id="${active}"]`).getAttribute("role"),
      "option",
    );
    await page.keyboard.press("Escape");
    assert.equal(await trigger.getAttribute("aria-expanded"), "false");
    assert.equal(await trigger.getAttribute("aria-controls"), null);
    assert.equal(await trigger.textContent(), initial);
    assert(await trigger.evaluate((e) => e === document.activeElement));
  }
  await trigger.click();
  await page.locator(".brand").click({ position: { x: 1, y: 1 }, trial: true });
  // A non-navigation outside pointer closes the popup without changing its value.
  await page
    .locator("h1, .dialog-heading h2")
    .last()
    .dispatchEvent("pointerdown");
  assert.equal(await trigger.getAttribute("aria-expanded"), "false");
  await trigger.focus();
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Tab");
  assert.equal(await trigger.getAttribute("aria-expanded"), "false");
  assert(!(await trigger.evaluate((e) => e === document.activeElement)));
}
async function publicChrome(page) {
  const delta = await page.evaluate(() => {
    const main = document.querySelector(".public-main");
    const padding = getComputedStyle(main);
    const left =
      main.getBoundingClientRect().left + parseFloat(padding.paddingLeft);
    const right =
      main.getBoundingClientRect().right - parseFloat(padding.paddingRight);
    return [
      document.querySelector(".brand").getBoundingClientRect().left - left,
      document.querySelector(".app-footer a").getBoundingClientRect().left -
        left,
      document.querySelector(".topbar-actions").getBoundingClientRect().right -
        right,
    ];
  });
  assert(
    delta.every((n) => Math.abs(n) <= 1),
    `Public alignment: ${delta}`,
  );
  assert(!(await page.locator(".app-footer").textContent()).includes("linux/"));
  assert.equal(await page.locator(".footer-notice").count(), 1);
  assert.equal(
    await page.locator(".app-footer a").getAttribute("href"),
    "https://github.com/PMExtra/RedApp",
  );
  assert.equal(await page.locator(".app-footer a").textContent(), "RedApp");
  assert.equal(await page.locator("select").count(), 0);
}
async function siteSettingsTest(page, context, lang, prefix, restart, login) {
  const defaults = JSON.parse(
    fs.readFileSync(path.join(root, "internal/site/defaults.json"), "utf8"),
  );
  const custom = {
    title: {
      en: "Enterprise tools <b>EN</b>",
      "zh-CN": "企业应用 <b>中文</b>",
    },
    subtitle: {
      en: "Internal application downloads",
      "zh-CN": "内部应用下载服务",
    },
    disclaimer: {
      en: '<img src=x onerror="window.__redappXSS=1"> Independent fixture service.',
      "zh-CN": "<script>window.__redappXSS=1</script> 独立测试分发服务。",
    },
  };
  async function edit(value) {
    const section = page.locator(".site-settings");
    for (const [i, locale] of ["en", "zh-CN"].entries()) {
      const fields = section.locator(".site-locale").nth(i);
      await fields.locator("input").nth(0).fill(value.title[locale]);
      await fields.locator("input").nth(1).fill(value.subtitle[locale]);
      await fields.locator("textarea").fill(value.disclaimer[locale]);
    }
    let calls = 0;
    const count = (request) => {
      if (
        request.method() === "POST" &&
        new URL(request.url()).pathname === "/admin/api/site"
      )
        calls++;
    };
    page.on("request", count);
    const saved = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/admin/api/site",
    );
    await section.locator("form").evaluate((form) => {
      form.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      );
      form.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      );
    });
    assert((await saved).ok());
    await section.locator(".notice").waitFor();
    page.off("request", count);
    assert.equal(calls, 1);
    assert.equal(
      await page.locator(".brand strong").textContent(),
      value.title[lang],
    );
    assert.equal(
      await page.locator(".footer-notice").textContent(),
      value.disclaimer[lang],
    );
    assert.equal(
      await page
        .locator(".brand b, .footer-notice img, .footer-notice script")
        .count(),
      0,
    );
    assert.equal(await page.evaluate(() => window.__redappXSS), undefined);
  }
  await edit(custom);
  if (restart) {
    await stop();
    await start();
    await page.reload();
    await page.locator(".login").waitFor();
    await login();
    if (
      (await page.locator(".auto-refresh").getAttribute("aria-pressed")) ===
      "true"
    )
      await page.locator(".auto-refresh").click();
    await page.locator(".sidebar nav button").nth(3).click();
    await page.waitForFunction(
      () =>
        document.querySelector(".site-settings input")?.value ===
        "Enterprise tools <b>EN</b>",
    );
    assert.deepEqual(
      (await (await page.request.get(base + "/api/info")).json()).site,
      custom,
    );
  }
  await noOverflow(page);
  await screenshot(page, prefix + "-site-settings");
  const publicPage = await context.newPage();
  await publicPage.goto(base + "/apps/codex");
  await publicPage.waitForFunction(
    (title) => document.querySelector(".brand strong")?.textContent === title,
    custom.title[lang],
  );
  assert.equal(
    await publicPage.locator(".brand span").textContent(),
    custom.subtitle[lang],
  );
  assert.equal(
    await publicPage.locator(".footer-notice").textContent(),
    custom.disclaimer[lang],
  );
  assert.equal(
    await publicPage
      .locator(".brand b, .footer-notice img, .footer-notice script")
      .count(),
    0,
  );
  await publicChrome(publicPage);
  await noOverflow(publicPage);
  await screenshot(publicPage, prefix + "-custom-site");
  await publicPage.close();
  await edit(defaults);
  checks.push(
    prefix +
      ": bilingual site save, duplicate-submit guard, plain-text injection safety and public reflection" +
      (restart ? ", real process restart persistence" : ""),
  );
}
async function insecureCopyTest(width, height, lang) {
  const fake = "http://redapp.test:" + new URL(base).port;
  const context = await browser.newContext({
    viewport: { width, height },
    locale: lang === "zh-CN" ? "zh-CN" : "en-US",
  });
  await context.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    assert.equal(url.origin, fake, "Unexpected insecure-context request");
    const response = await fetch(base + url.pathname, {
      headers: { Host: url.host },
    });
    await route.fulfill({
      status: response.status,
      headers: Object.fromEntries(response.headers),
      body: Buffer.from(await response.arrayBuffer()),
    });
  });
  const page = await context.newPage();
  page.on("pageerror", (error) => failures.push(error.message));
  await page.goto(fake + "/apps/codex");
  await page.locator(".command").first().waitFor();
  assert.equal(await page.evaluate(() => window.isSecureContext), false);
  assert.equal(await page.locator(".copy-button").count(), 0);
  assert.equal(await page.locator('.command pre[tabindex="0"]').count(), 2);
  assert(!(await page.locator("main").textContent()).includes("HTTPS"));
  await noOverflow(page);
  await screenshot(
    page,
    (width < 500 ? "mobile" : "desktop") +
      "-" +
      (lang === "zh-CN" ? "zh" : "en") +
      "-insecure-copy",
  );
  checks.push(
    `${width}/${lang}: real insecure origin hides copy controls and preserves selectable commands`,
  );
  await context.close();
}
async function runViewport(width, height, mobile, lang) {
  const zh = lang === "zh-CN",
    prefix = (mobile ? "mobile" : "desktop") + "-" + (zh ? "zh" : "en");
  const context = await browser.newContext({
    viewport: { width, height },
    locale: zh ? "zh-CN" : "en-US",
    timezoneId: zh ? "Asia/Shanghai" : "America/Los_Angeles",
    permissions: ["clipboard-read", "clipboard-write"],
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  context.on("requestfailed", (request) => {
    if (!request.failure()?.errorText.includes("ERR_ABORTED"))
      failures.push("Request failed " + new URL(request.url()).pathname);
  });
  context.on("response", (response) => {
    if (response.status() < 400) return;
    const route = new URL(response.url()).pathname;
    const expected =
      (response.status() === 401 &&
        ["/admin/api/session", "/admin/api/status"].includes(route)) ||
      (response.status() === 503 && route === "/admin/api/history") ||
      (response.status() === 400 &&
        ["/admin/api/proxy", "/admin/api/password"].includes(route));
    (expected ? expectedFailures : failures).push(
      response.status() + " " + route,
    );
  });
  page.on("pageerror", (error) => failures.push(error.message));
  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      !message.text().startsWith("Failed to load resource:")
    )
      failures.push(message.text());
  });
  await context.route("**/*", async (route) => {
    if (new URL(route.request().url()).origin !== base) {
      failures.push("Unexpected external page request");
      await route.abort();
    } else await route.continue();
  });
  await page.goto(base + "/");
  await page.locator(".application-card").waitFor();
  assert.equal(await page.locator("html").getAttribute("lang"), lang);
  await selectMenu(
    page,
    page.locator(".language-control"),
    zh ? "English" : "简体中文",
  );
  assert.equal(
    await page.locator("h1").textContent(),
    zh ? "Applications" : "应用",
  );
  await selectMenu(
    page,
    page.locator(".language-control"),
    zh ? "简体中文" : "English",
  );
  await page.reload();
  await page.locator(".application-card").waitFor();
  assert.equal(await page.locator("html").getAttribute("lang"), lang);
  assert(
    await page
      .locator(".application-card img")
      .evaluate((img) => img.complete && img.naturalWidth > 0),
  );
  assert.equal(
    await page.locator(".application-card img").getAttribute("alt"),
    zh ? "OpenAI 品牌标志" : "OpenAI brand mark",
  );
  await noOverflow(page);
  await publicChrome(page);
  await dropdownKeyboard(page, page.locator(".language-control"));
  await page.locator(".language-control").getByRole("combobox").click();
  await screenshot(page, prefix + "-language-dropdown");
  await page.keyboard.press("Escape");
  await screenshot(page, prefix + "-home");
  await page.locator(".application-card").click();
  await page.getByRole("heading", { name: "Codex CLI", exact: true }).waitFor();
  assert(
    await page
      .locator(".application-identity img")
      .evaluate((img) => img.complete && img.naturalWidth > 0),
  );
  for (const [i, expected] of [
    [0, `curl -fsSL '${base}/install.sh' | sh`],
    [1, `irm '${base}/install.ps1' | iex`],
  ]) {
    await page.locator(".command button").nth(i).click();
    assert.equal(
      await page.evaluate(() => navigator.clipboard.readText()),
      expected,
    );
  }
  await noOverflow(page);
  await publicChrome(page);
  await screenshot(page, prefix + "-detail");
  checks.push(
    prefix +
      ": public pages, persisted language, local brand mark and exact clipboard commands",
  );
  await page.locator(".admin-link").click();
  await page.locator(".login").waitFor();
  if ((mobile && zh) || (!mobile && !zh))
    await screenshot(page, prefix + "-login");
  async function login() {
    await page.locator(".login input[type=password]").fill(password);
    await page.locator(".login form").evaluate((form) => form.requestSubmit());
    await page.locator(".metric-catalog").waitFor();
  }
  await login();
  assert.equal(await page.locator(".metric-catalog .metric-card").count(), 43);
  assert(
    (await page.locator(".app-footer").textContent()).includes(
      "v" + fs.readFileSync(path.join(root, "VERSION"), "utf8").trim(),
    ),
  );
  assert((await page.locator(".app-footer").textContent()).includes("(linux/"));
  assert(
    (await page.locator(".brand").textContent()).includes(
      zh ? "应用再分发平台" : "Application Redistribution Platform",
    ),
  );
  assert.equal(await page.locator("input[type=checkbox]").count(), 0);
  assert(
    await page.locator(".snapshot time").evaluate(
      (element, locale) =>
        element.textContent ===
        new Intl.DateTimeFormat(locale, {
          year: "numeric",
          month: "short",
          day: "numeric",
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
          timeZoneName: "short",
        }).format(new Date(element.getAttribute("datetime"))),
      lang,
    ),
  );
  await page.locator(".auto-refresh").click();
  assert.equal(
    await page.locator(".auto-refresh").getAttribute("aria-pressed"),
    "false",
  );
  await noOverflow(page);
  await screenshot(page, prefix + "-overview");
  const trigger = page.locator(".account-trigger");
  await trigger.focus();
  await page.keyboard.press("ArrowDown");
  await page.locator("[role=menu]").waitFor();
  assert(
    await page
      .locator("[role=menuitem]")
      .first()
      .evaluate((element) => element === document.activeElement),
  );
  await page.keyboard.press("ArrowDown");
  assert(
    await page
      .locator("[role=menuitem]")
      .nth(1)
      .evaluate((element) => element === document.activeElement),
  );
  if ((!mobile && zh) || (mobile && !zh))
    await screenshot(page, prefix + "-account");
  await page.keyboard.press("Escape");
  assert(
    await trigger.evaluate((element) => element === document.activeElement),
  );
  await trigger.click();
  await page.locator("[role=menuitem]").first().click();
  let dialog = page.locator(".password-dialog");
  await dialog.waitFor();
  await noOverflow(page);
  await screenshot(page, prefix + "-password");
  await page.keyboard.press("Escape");
  assert.equal(await page.locator(".password-dialog").count(), 0);
  assert(
    await trigger.evaluate((element) => element === document.activeElement),
  );
  await trigger.click();
  await page.locator("[role=menuitem]").first().click();
  dialog = page.locator(".password-dialog");
  const inputs = dialog.locator("input");
  const nextPassword = "fixture-v040-password-" + prefix;
  await inputs.nth(0).fill("wrong-fixture-password");
  await inputs.nth(1).fill(nextPassword);
  await inputs.nth(2).fill("mismatch");
  let passwordCalls = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/admin/api/password")
      passwordCalls++;
  });
  await dialog.locator("form").evaluate((form) => form.requestSubmit());
  await dialog.getByRole("alert").waitFor();
  assert.equal(passwordCalls, 0);
  await inputs.nth(2).fill(nextPassword);
  await dialog.locator("form").evaluate((form) => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  await page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/admin/api/password" &&
      response.status() === 400,
  );
  await dialog.getByRole("alert").waitFor();
  assert.equal(passwordCalls, 1);
  await inputs.nth(0).fill(password);
  await dialog.locator("form").evaluate((form) => {
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  await page.locator(".login").waitFor();
  assert.equal(passwordCalls, 2);
  password = nextPassword;
  await login();
  if (
    (await page.locator(".auto-refresh").getAttribute("aria-pressed")) ===
    "true"
  )
    await page.locator(".auto-refresh").click();
  checks.push(
    prefix +
      ": local snapshot/footer, refresh toggle, keyboard menu and password cancel/failure/duplicate/success",
  );
  const openHistory = page.getByRole("button", {
    name: zh ? "查看缓存历史" : "View Cache history",
    exact: true,
  });
  await openHistory.click();
  dialog = page.locator(".history-dialog");
  await dialog.locator("canvas").waitFor();
  assert.equal(
    await dialog
      .locator(".segmented button")
      .nth(1)
      .getAttribute("aria-pressed"),
    "true",
  );
  if ((mobile && zh) || (!mobile && !zh))
    await screenshot(page, prefix + "-history");
  for (const i of [0, 2, 1]) {
    await dialog.locator(".segmented button").nth(i).click();
    await dialog.locator("canvas").waitFor();
  }
  await dialog.locator("summary").click();
  assert((await dialog.locator("tbody tr").count()) > 0);
  await page.keyboard.press("Escape");
  assert(
    await openHistory.evaluate((element) => element === document.activeElement),
  );
  if (!mobile && !zh) {
    let historyFailure = true;
    await page.route("**/admin/api/history", async (route) =>
      historyFailure
        ? route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({ error: "fixture failure" }),
          })
        : route.continue(),
    );
    await openHistory.click();
    dialog = page.locator(".history-dialog");
    await dialog.getByRole("alert").waitFor();
    historyFailure = false;
    await dialog.getByRole("alert").getByRole("button").click();
    await dialog.locator("canvas").waitFor();
    await page.keyboard.press("Escape");
    await page.unroute("**/admin/api/history");
    await page.route("**/admin/api/history", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          key: "disk.cache_bytes",
          label: "Cache",
          kind: "gauge",
          unit: "bytes",
          range: "7d",
          resolution_seconds: 3600,
          points: [],
        }),
      }),
    );
    await openHistory.click();
    await page.locator(".history-dialog .empty").waitFor();
    await page.keyboard.press("Escape");
    await page.unroute("**/admin/api/history");
  }
  checks.push(
    prefix +
      ": real history canvas, windows, accessible table and focus return",
  );
  const traffic = page.locator(".metric-group").filter({
    has: page.getByText(
      zh
        ? "制品回源流量（HTTP 载荷）"
        : "Artifact upstream traffic (HTTP payload)",
      { exact: true },
    ),
  });
  await traffic.locator("summary").click();
  assert(
    (await traffic.textContent()).includes(
      zh
        ? "制品分发流量（压缩前）"
        : "Artifact downstream traffic (before compression)",
    ),
  );
  await traffic
    .getByRole("button", {
      name: zh
        ? "查看制品回源流量（HTTP 载荷）历史"
        : "View Artifact upstream traffic (HTTP payload) history",
      exact: true,
    })
    .click();
  dialog = page.locator(".history-dialog");
  await dialog.locator("canvas").waitFor();
  // Escape closes the nested dropdown first, then the containing dialog.
  const counterMode = dialog.getByRole("combobox");
  await counterMode.focus();
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("End");
  await page.keyboard.press("Escape");
  assert(await dialog.isVisible());
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  assert(
    (await counterMode.textContent()).includes(
      zh ? "观测增量" : "Observed increment",
    ),
  );
  await screenshot(page, prefix + "-counter-history");
  await page.keyboard.press("Escape");
  for (const i of [1, 2, 3]) {
    await page.locator(".sidebar nav button").nth(i).click();
    await noOverflow(page);
    if (i === 1) {
      const version = page.locator(".inline-label");
      await dropdownKeyboard(page, version);
      await selectMenu(page, version, "0.159.2");
      assert.equal(await page.locator("tbody tr").count(), 1);
      await selectMenu(page, version, zh ? "全部版本" : "All versions");
    }
    if (i < 3)
      await screenshot(page, prefix + (i === 1 ? "-resources" : "-events"));
  }
  await page
    .locator('.settings-stack input[placeholder="http://proxy.example:3128"]')
    .waitFor();
  await page.waitForFunction(
    () =>
      !document.querySelector(
        '.settings-stack input[placeholder="http://proxy.example:3128"]',
      ).disabled,
  );
  if ((mobile && zh) || (!mobile && !zh))
    await screenshot(page, prefix + "-settings");
  await siteSettingsTest(page, context, lang, prefix, !mobile && !zh, login);
  const proxy = page.locator("section").filter({
    has: page.locator('input[placeholder="http://proxy.example:3128"]'),
  });
  await dropdownKeyboard(page, proxy);
  await proxy
    .locator('input[placeholder="http://proxy.example:3128"]')
    .fill("http://127.0.0.1:3128");
  await selectMenu(page, proxy, zh ? "替换凭据" : "Replace credentials");
  await proxy.locator("input[type=password]").fill("fixture-only-secret");
  await proxy.locator("form").evaluate((form) => form.requestSubmit());
  await proxy.locator(".notice").waitFor();
  let saved = await fixtureAPI(page);
  assert(
    saved.has_credentials &&
      !JSON.stringify(saved).includes("fixture-only-secret"),
  );
  await proxy.locator("form").evaluate((form) => form.requestSubmit());
  await proxy.locator(".notice").waitFor();
  assert((await fixtureAPI(page)).has_credentials);
  await selectMenu(page, proxy, zh ? "清除凭据" : "Clear credentials");
  await proxy
    .locator('input[placeholder="http://proxy.example:3128"]')
    .fill("");
  await proxy.locator("form").evaluate((form) => form.requestSubmit());
  await proxy.locator(".notice").waitFor();
  assert(!(await fixtureAPI(page)).has_credentials);
  await page.locator(".ttl-form input").fill("120");
  await page.locator(".ttl-form").evaluate((form) => form.requestSubmit());
  await page.locator(".maintenance-stack > .notice").waitFor();
  assert.equal(
    (await (await page.request.get(base + "/admin/api/settings")).json())
      .latest_ttl_seconds,
    120,
  );
  await page.locator("form.cleanup input").fill("0.160.0");
  await page.locator("form.cleanup").evaluate((form) => form.requestSubmit());
  await page.locator(".cleanup-review").waitFor();
  await page.locator(".cleanup-review .secondary").click();
  assert.equal(await page.locator(".cleanup-review").count(), 0);
  if (mobile && zh) {
    await page.locator("form.cleanup").evaluate((form) => form.requestSubmit());
    await page.locator(".cleanup-review").waitFor();
    await page.locator(".cleanup-review .danger").click();
    await page.locator(".cleanup-review").waitFor({ state: "detached" });
    await page.locator(".sidebar nav button").nth(1).click();
    await page.getByText("当前筛选下没有缓存资源。", { exact: true }).waitFor();
  }
  checks.push(
    prefix +
      ": resource/event/settings navigation, proxy secrets, TTL and cleanup",
  );
  await page.locator(".sidebar nav button").first().click();
  await page.locator(".auto-refresh").click();
  assert.equal(
    await page.locator(".auto-refresh").getAttribute("aria-pressed"),
    "true",
  );
  let statusRequests = 0;
  await page.route("**/admin/api/status", async (route) => {
    statusRequests++;
    await route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ error: "Sign in required" }),
    });
  });
  await page.locator(".refresh-actions > button").last().click();
  await page.locator(".login").waitFor();
  if (!mobile && !zh) {
    const before = statusRequests;
    await wait(5500);
    assert.equal(
      statusRequests,
      before,
      "Polling continued after session expiration",
    );
  }
  checks.push(prefix + ": expired session clears private state and polling");
  await context.close();
  await insecureCopyTest(width, height, lang);
}
(async () => {
  try {
    const probe = net.createServer();
    await new Promise((resolve) => probe.listen(0, "127.0.0.1", resolve));
    base = "http://127.0.0.1:" + probe.address().port;
    await new Promise((resolve) => probe.close(resolve));
    await start();
    assert(password, "Missing private fixture bootstrap");
    await stop();
    seed();
    await start();
    browser = await chromium.launch({
      headless: true,
      executablePath: process.env.REDAPP_CHROMIUM || "/usr/bin/chromium",
    });
    await runViewport(1366, 900, false, "en");
    await runViewport(1366, 900, false, "zh-CN");
    await runViewport(390, 844, true, "en");
    await runViewport(390, 844, true, "zh-CN");
    assert.deepEqual(failures, [], "Unexpected console/network failures");
    fs.writeFileSync(
      path.join(artifacts, "report.json"),
      JSON.stringify(
        {
          checks,
          expectedFailures,
          unexpectedFailures: failures,
          viewports: ["1366x900", "390x844"],
          languages: ["en", "zh-CN"],
          timezones: ["America/Los_Angeles", "Asia/Shanghai"],
          browser: await browser.version(),
          fixtureOnly: true,
        },
        null,
        2,
      ),
    );
    console.log(
      "Headless acceptance passed; " +
        checks.length +
        " checks; artifacts: " +
        artifacts,
    );
  } catch (error) {
    const message = String(error.stack || error).replaceAll(
      password || "__no_password__",
      "[redacted]",
    );
    console.error(message);
    process.exitCode = 1;
  } finally {
    if (browser) await browser.close();
    await stop();
    fs.rmSync(data, { recursive: true, force: true });
  }
})();
