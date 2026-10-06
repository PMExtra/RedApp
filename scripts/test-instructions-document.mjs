#!/usr/bin/env node
// Execute only locally authored fixture scripts in a DOM emulator. No GUI.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { fileURLToPath } from "node:url";
import { Browser } from "../frontend/node_modules/happy-dom/lib/index.js";

const root = fileURLToPath(new URL("..", import.meta.url));
const temporary = await mkdtemp(join(tmpdir(), "redapp-document-"));
// Happy DOM may fetch blocking script tags synchronously. Serve the fixture in
// another process so its HTTP event loop can run while document parsing waits.
const fixture = spawn(
  process.execPath,
  [
    "-e",
    `
  const server = require('node:http').createServer((_request,response) => {
    response.setHeader('Content-Type','text/javascript');
    response.end('window.externalFixture = "executed";');
  });
  server.listen(0,'127.0.0.1',()=>process.stdout.write(String(server.address().port)+'\\n'));
`,
  ],
  { stdio: ["ignore", "pipe", "ignore"] },
);
const fixturePort = Number(
  String((await once(fixture.stdout, "data"))[0]).trim(),
);
assert(Number.isInteger(fixturePort) && fixturePort > 0);
const probe = createServer();
probe.listen(0, "127.0.0.1");
await once(probe, "listening");
const port = probe.address().port;
await new Promise((resolve) => probe.close(resolve));
const origin = `http://127.0.0.1:${port}`;
const env = Object.fromEntries(
  Object.entries(process.env).filter(([key]) => !key.startsWith("REDAPP_")),
);
Object.assign(env, {
  REDAPP_DATA: join(temporary, "data"),
  REDAPP_LISTEN: `127.0.0.1:${port}`,
});
const processUnderTest = spawn(resolve(root, "bin/redapp"), [], {
  env,
  stdio: ["ignore", "pipe", "pipe"],
});
let privateLog = "",
  csrf = "",
  cookie = "";
for (const stream of [processUnderTest.stdout, processUnderTest.stderr])
  stream.on("data", (chunk) => {
    privateLog += chunk;
  });
// Only this test's locally authored fixture is evaluated.
const browser = new Browser({
  settings: {
    enableJavaScriptEvaluation: true,
    suppressInsecureJavaScriptEnvironmentWarning: true,
  },
});
async function withTimeout(promise, label) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(Error(label)), 10000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}
async function request(path, body, method) {
  const response = await fetch(origin + path, {
    method: method || (body ? "POST" : "GET"),
    headers: {
      Origin: origin,
      Cookie: cookie,
      "X-CSRF-Token": csrf,
      "Content-Type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  assert.equal(response.status, 200, path);
  return response;
}
try {
  let ready = false;
  for (let attempt = 0; attempt < 100; attempt++) {
    assert.equal(
      processUnderTest.exitCode,
      null,
      "server exited during fixture setup",
    );
    try {
      if ((await fetch(origin + "/health/ready")).ok) {
        ready = true;
        break;
      }
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  assert(ready, "server readiness");
  const password = privateLog.match(/Initial admin password: ([0-9a-f]+)/)?.[1];
  assert(password, "fixture bootstrap credential");
  const login = await request("/admin/api/login", { password });
  cookie = login.headers
    .getSetCookie()
    .map((item) => item.split(";")[0])
    .join("; ");
  csrf = (await login.json()).csrf;
  const vendor = (await (await request("/admin/api/vendors/openai")).json())
    .vendor;
  const app = (await (await request("/admin/api/apps/openai/codex")).json())
    .app;
  await request(
    "/admin/api/vendors/openai",
    { revision: vendor.revision, enabled: true },
    "PATCH",
  );
  await request(
    "/admin/api/apps/openai/codex",
    { revision: app.revision, enabled: true },
    "PATCH",
  );
  const instructions = await (
    await request("/admin/api/apps/openai/codex/instructions")
  ).json();
  const en = `# Executable fixture\n\n## Installation instructions\n\n{{app_name}} {{app_key}} {{base_url}}{{app_path}}\n\n<script>window.inlineFixture = "executed";</script>\n<script src="http://127.0.0.1:${fixturePort}/fixture.js"></script>\n\n{{unknown}}\n\n\`\`\`sh\n  first\n\nsecond  \n\`\`\`\n\n\`inline\`\n\n<pre id="raw"><code>raw block</code></pre>`;
  await request(
    "/admin/api/apps/openai/codex/instructions",
    { en, "zh-CN": "# 中文说明\n\n```\n中文代码\n```", revision: instructions.revision },
    "PUT",
  );
  const path = "/api/apps/openai/codex/instructions/document?lang=en";
  const documentResponse = await request(path);
  assert(
    documentResponse.headers
      .get("Content-Security-Policy")
      .includes("'unsafe-inline'"),
  );
  const admin = await request("/admin/overview");
  assert(
    admin.headers.get("Content-Security-Policy").includes("script-src 'self'"),
  );
  const page = browser.newPage();
  console.log("Loading fixture instruction document");
  await withTimeout(page.goto(origin + path), "Document navigation timed out");
  console.log("Waiting for fixture script resources");
  await withTimeout(page.waitUntilComplete(), "Document resources timed out");
  assert.equal(page.evaluate("window.inlineFixture"), "executed");
  assert.equal(page.evaluate("window.externalFixture"), "executed");
  assert.equal(
    page.evaluate('document.querySelector("h1").textContent'),
    "Executable fixture",
  );
  assert.equal(page.evaluate('document.querySelector("h2").textContent'), "Installation instructions");
  assert(page.evaluate("document.body.textContent").includes("openai/codex"));
  assert(page.evaluate("document.body.textContent").includes("{{unknown}}"));
  assert(page.evaluate("document.body.textContent").includes(origin + "/openai/codex"));
  assert.equal(page.evaluate('document.querySelectorAll(".copy-code").length'), 2);
  assert.equal(page.evaluate('document.querySelector("#raw").outerHTML'), '<pre id="raw"><code>raw block</code></pre>');
  await page.evaluate(`(async()=>{
    window.copied=[];
    window.copyNow=0;
    window.copyTimers=[];
    window.setTimeout=(fn,delay)=>window.copyTimers.push({fn,at:window.copyNow+delay});
    window.advanceCopy=ms=>{
      window.copyNow+=ms;
      const due=window.copyTimers.filter(timer=>timer.at<=window.copyNow);
      window.copyTimers=window.copyTimers.filter(timer=>timer.at>window.copyNow);
      due.forEach(timer=>timer.fn());
    };
    Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>window.copied.push(text)},configurable:true});
    document.querySelector('.copy-block button').click();
    document.querySelector('.copy-inline button').click();
    await Promise.resolve();
  })()`);
  assert.deepEqual(Array.from(page.evaluate('window.copied')), ['  first\n\nsecond  \n','inline']);
  assert.equal(page.evaluate('document.querySelector("[role=status]").textContent'), 'Copied');
  for (const kind of ['block', 'inline']) {
    assert.equal(page.evaluate(`document.querySelector('.copy-${kind} button').disabled`), true);
    assert.equal(page.evaluate(`document.querySelector('.copy-${kind} button').textContent`), 'Copied');
    assert.equal(page.evaluate(`document.querySelector('.copy-${kind} button').getAttribute('aria-label')`), 'Copied');
    assert.equal(page.evaluate(`document.querySelector('.copy-${kind} button svg path').getAttribute('d')`), 'M4 12l5 5L20 6');
  }
  page.evaluate('window.advanceCopy(2999);document.querySelector(".copy-block button").click()');
  assert.equal(page.evaluate('document.querySelector(".copy-block button").disabled'), true);
  assert.equal(page.evaluate('window.copied.length'), 2);
  page.evaluate('window.advanceCopy(1)');
  for (const kind of ['block', 'inline']) {
    assert.equal(page.evaluate(`document.querySelector('.copy-${kind} button').disabled`), false);
    assert.equal(page.evaluate(`document.querySelector('.copy-${kind} button').textContent`), 'Copy');
    assert(page.evaluate(`document.querySelector('.copy-${kind} button svg rect') !== null`));
  }
  await page.evaluate(`(async()=>{
    navigator.clipboard.writeText=async()=>{throw Error('denied')};
    document.querySelector('.copy-inline button').focus();
    document.querySelector('.copy-inline button').click();
    await Promise.resolve();
  })()`);
  assert.equal(page.evaluate('document.activeElement.getAttribute("aria-label")'), 'Copy code');
  assert.equal(page.evaluate('document.querySelector(".copy-inline [role=status]").textContent'), 'Copy failed; select and copy the code manually');
  assert.equal(page.evaluate('document.querySelector(".copy-heading .copy-title").textContent'), 'Shell');
  assert.equal(page.evaluate('document.querySelector(".copy-heading button").getAttribute("aria-label")'), 'Copy code');
  await page.evaluate(`(()=>{
    window.copyCalls=0;
    navigator.clipboard.writeText=()=>{window.copyCalls++;return new Promise(resolve=>window.finishCopy=resolve)};
    document.querySelector('.copy-block button').click();
    document.querySelector('.copy-block button').click();
  })()`);
  assert.equal(page.evaluate('window.copyCalls'), 1);
  assert.equal(page.evaluate('document.querySelector(".copy-block button").disabled'), true);
  assert.equal(page.evaluate('document.querySelector(".copy-block button").textContent'), 'Copying…');
  await page.evaluate('(async()=>{window.finishCopy();await Promise.resolve()})()');
  assert.equal(page.evaluate('document.querySelector(".copy-block button").disabled'), true);
  assert.equal(page.evaluate('document.querySelector(".copy-block button").textContent'), 'Copied');
  page.evaluate('window.advanceCopy(3000)');
  assert.equal(page.evaluate('document.querySelector(".copy-block button").disabled'), false);
  assert.equal(page.evaluate('getComputedStyle(document.querySelector(".copy-block .copy-status")).display'), 'block');
  await page.evaluate(`(async()=>{
    Object.defineProperty(navigator,'clipboard',{value:undefined,configurable:true});
    document.querySelector('.copy-block button').click();await Promise.resolve();
  })()`);
  assert.equal(page.evaluate('document.querySelector(".copy-block .copy-status").hasAttribute("data-error")'), true);
  await page.goto(origin + path.replace("lang=en", "lang=zh-CN"));
  assert.equal(
    page.evaluate('document.querySelector("h1").textContent'),
    "中文说明",
  );
  assert.equal(page.evaluate('document.querySelector(".copy-title").textContent'), '代码');
  assert.equal(page.evaluate('document.querySelector(".copy-code").textContent'), '复制');
  await page.evaluate(`(async()=>{
    window.setTimeout=(fn,delay)=>{window.resetCopy=fn;window.resetDelay=delay};
    Object.defineProperty(navigator,'clipboard',{value:{writeText:async()=>{}},configurable:true});
    document.querySelector('.copy-code').click();await Promise.resolve();
  })()`);
  assert.equal(page.evaluate('document.querySelector(".copy-code").textContent'), '已复制');
  assert.equal(page.evaluate('document.querySelector(".copy-code").getAttribute("aria-label")'), '已复制');
  assert.equal(page.evaluate('document.querySelector(".copy-code").disabled'), true);
  assert.equal(page.evaluate('window.resetDelay'), 3000);
  page.evaluate('window.resetCopy()');
  assert.equal(page.evaluate('document.querySelector(".copy-code").textContent'), '复制');
  assert.equal(page.evaluate('document.querySelector(".copy-code").disabled'), false);
  console.log(
    "Real HTTP instruction document: Markdown-only copy controls, exact whitespace/newlines, check/Copied feedback and three-second reset, keyboard focus, clipboard failure, controlled placeholders, inline and external local fixture scripts, bilingual navigation and separate CSP headers passed (Happy DOM; no GUI).",
  );
} finally {
  await browser.abort();
  await browser.close();
  if (processUnderTest.exitCode === null) {
    const exited = once(processUnderTest, "exit");
    processUnderTest.kill("SIGTERM");
    await exited;
  }
  const fixtureExit = once(fixture, "exit");
  fixture.kill("SIGTERM");
  await fixtureExit;
  await rm(temporary, { recursive: true, force: true });
}
