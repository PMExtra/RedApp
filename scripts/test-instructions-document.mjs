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
  const en = `# Executable fixture\n\n{{app_name}} {{app_key}}\n\n<script>window.inlineFixture = "executed";</script>\n<script src="http://127.0.0.1:${fixturePort}/fixture.js"></script>\n\n{{unknown}}`;
  await request(
    "/admin/api/apps/openai/codex/instructions",
    { en, "zh-CN": "# 中文说明", revision: instructions.revision },
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
  assert(page.evaluate("document.body.textContent").includes("openai/codex"));
  assert(page.evaluate("document.body.textContent").includes("{{unknown}}"));
  await page.goto(origin + path.replace("lang=en", "lang=zh-CN"));
  assert.equal(
    page.evaluate('document.querySelector("h1").textContent'),
    "中文说明",
  );
  console.log(
    "Real HTTP instruction document: Markdown, controlled placeholders, inline and external local fixture scripts, bilingual navigation and separate CSP headers passed (Happy DOM; no GUI).",
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
