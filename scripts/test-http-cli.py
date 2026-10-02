#!/usr/bin/env python3
"""通过真实可执行文件验证启动、HTTP 管理 API、健康检查和停止；不输出密码。"""
import http.cookiejar
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parents[1]
version_output = subprocess.check_output([str(root / "bin/redapp"), "version"], text=True)
assert re.fullmatch(r"RedApp [^\s]+ \(commit [^\s]+\)\n", version_output), "版本信息无效"
with tempfile.TemporaryDirectory(prefix="redapp-http-cli-") as temp:
    directory = Path(temp)
    with socket.socket() as socket_probe:
        socket_probe.bind(("127.0.0.1", 0))
        port = socket_probe.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    env = os.environ.copy()
    env.update(REDAPP_LISTEN=f"127.0.0.1:{port}", REDAPP_PUBLIC_URL=base, REDAPP_DATA=str(directory / "data"))
    log = (directory / "server.log").open("w+")
    process = subprocess.Popen([str(root / "bin/redapp")], env=env, stdout=log, stderr=log)
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise AssertionError("服务提前退出，日志保留在临时目录")
            try:
                with urllib.request.urlopen(base + "/health/ready", timeout=1) as response:
                    assert response.status == 200
                break
            except (OSError, urllib.error.URLError):
                time.sleep(0.05)
        else:
            raise AssertionError("服务未就绪")
        subprocess.run([str(root / "bin/redapp"), "healthcheck"], env=env, check=True)
        log.flush()
        log.seek(0)
        text = log.read()
        password_match = re.search(r"Initial admin password: ([0-9a-f]+)", text)
        assert password_match, "未输出初始密码"
        password = password_match.group(1)
        for path in ["/", "/apps/codex", "/apps/claude-code"]:
            with urllib.request.urlopen(base + path) as response:
                assert response.status == 200 and "text/html" in response.headers["Content-Type"]
                assert "script-src 'self'" in response.headers["Content-Security-Policy"]
                assert response.headers["Cache-Control"] == "no-store"
                assert "/admin/assets/" in response.read().decode()
        with urllib.request.urlopen(base + "/api/info") as response:
            public_info = json.load(response)
            assert set(public_info) == {"version", "os", "arch", "site"}
            assert public_info["version"] == version_output.split()[1]
        with urllib.request.urlopen(base + "/api/apps") as response:
            applications = json.load(response)
            assert [app["id"] for app in applications] == ["codex", "claude-code"]
            assert all(set(app) == {"id", "name", "summary", "origin", "icon"} for app in applications)
            assert applications[0]["origin"] == base
            assert set(applications[0]) == {"id", "name", "summary", "origin", "icon"}
        with urllib.request.urlopen(base + applications[0]["icon"]) as response:
            assert response.headers["Content-Type"] == "image/svg+xml; charset=utf-8"
            assert response.headers["Content-Security-Policy"] == "sandbox; default-src 'none'"
            assert response.read() == (root / "internal/apps/codex/assets/openai-symbol.svg").read_bytes()
        try:
            urllib.request.urlopen(base + "/admin/api/status")
            raise AssertionError("匿名页面开放了管理 API")
        except urllib.error.HTTPError as error:
            assert error.code == 401
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        login = urllib.request.Request(base + "/admin/api/login", data=json.dumps({"password": password}).encode(), headers={"Content-Type": "application/json"})
        with opener.open(login) as response:
            csrf = json.load(response)["csrf"]
        with opener.open(base + "/admin/api/status") as response:
            status = json.load(response)
            assert status["name"] == "RedApp" and status["disk"]["free_bytes"] > 0
            assert len(status["metrics"]) == 43
        for window, resolution in [("24h", 60), ("7d", 3600), ("30d", 3600)]:
            history = urllib.request.Request(base + "/admin/api/history", headers={"X-History-Metric": "disk.cache_bytes", "X-History-Range": window})
            with opener.open(history) as response:
                series = json.load(response)
                assert series["resolution_seconds"] == resolution and series["points"][-1]["partial"]
        with opener.open(base + "/admin/") as response:
            html = response.read().decode()
            assert '<html lang="en">' in html and 'http-equiv' not in html
            assert "default-src 'self'" in response.headers["Content-Security-Policy"]
            assets = re.findall(r'(?:src|href)="(/admin/assets/[^" ]+)"', html)
            assert len(assets) >= 2
        assets = sorted(set(assets) | {"/admin/assets/" + path.name for path in (root / "internal/httpserver/web/assets").iterdir()})
        for asset in assets:
            with opener.open(base + asset) as response:
                assert response.status == 200 and response.read()
                assert "text/javascript" in response.headers["Content-Type"] or "text/css" in response.headers["Content-Type"]
        settings = urllib.request.Request(base + "/admin/api/settings", data=b'{"latest_ttl_seconds":120}', headers={"Content-Type": "application/json", "X-CSRF-Token": csrf})
        with opener.open(settings) as response:
            assert json.load(response)["ok"]
        with opener.open(base + "/admin/api/settings") as response:
            assert json.load(response)["latest_ttl_seconds"] == 120
        claude_settings = urllib.request.Request(base + "/admin/api/settings", data=b'{"latest_ttl_seconds":180}', headers={"Content-Type": "application/json", "X-CSRF-Token": csrf, "X-RedApp-Application": "claude-code"})
        with opener.open(claude_settings) as response:
            assert json.load(response)["ok"]
        for app, expected in [("codex", 120), ("claude-code", 180)]:
            request = urllib.request.Request(base + "/admin/api/settings", headers={"X-RedApp-Application": app})
            with opener.open(request) as response:
                assert json.load(response)["latest_ttl_seconds"] == expected
            request = urllib.request.Request(base + "/admin/api/cleanup/preview", data=b'{"minimum_version":"2.1.285"}', headers={"Content-Type":"application/json", "X-CSRF-Token":csrf, "X-RedApp-Application":app})
            with opener.open(request) as response:
                assert response.status == 200
        try:
            opener.open(urllib.request.Request(base + "/admin/api/settings", headers={"X-RedApp-Application":"unknown"}))
            raise AssertionError("unknown application accepted")
        except urllib.error.HTTPError as error:
            assert error.code == 400
        site_settings = public_info["site"]
        assert site_settings["subtitle"]["en"] == "Application Redistribution Platform"
        assert site_settings["subtitle"]["zh-CN"] == "应用再分发平台"
        site_settings["title"]["en"] = "Fixture tools"
        site_settings["disclaimer"]["zh-CN"] = "测试声明 <script>文本</script>"
        site_request = urllib.request.Request(base + "/admin/api/site", data=json.dumps(site_settings).encode(), headers={"Content-Type": "application/json", "X-CSRF-Token": csrf})
        with opener.open(site_request) as response:
            assert json.load(response) == site_settings
        with urllib.request.urlopen(base + "/api/info") as response:
            assert json.load(response)["site"] == site_settings
        with opener.open(base + "/admin/api/proxy") as response:
            assert json.load(response)["server"] == ""
        proxy_body = {"server": "http://127.0.0.1:3128", "username": "fixture-user", "password": "fixture-only-password", "password_action": "replace"}
        proxy_request = urllib.request.Request(base + "/admin/api/proxy", data=json.dumps(proxy_body).encode(), headers={"Content-Type": "application/json", "X-CSRF-Token": csrf})
        with opener.open(proxy_request) as response:
            saved = response.read().decode()
            assert "fixture-user" not in saved and "fixture-only-password" not in saved
            assert json.loads(saved)["has_credentials"]
        proxy_body = {"server": "", "password_action": "clear"}
        proxy_request = urllib.request.Request(base + "/admin/api/proxy", data=json.dumps(proxy_body).encode(), headers={"Content-Type": "application/json", "X-CSRF-Token": csrf})
        with opener.open(proxy_request) as response:
            assert not json.load(response)["has_credentials"]
        with opener.open(base + "/install.sh") as response:
            script = response.read().decode()
            assert base in script and "https://github.com" not in script
        for name in ["install.sh", "install.ps1"]:
            with opener.open(base + "/claude-code/" + name) as response:
                script = response.read().decode()
                assert base + "/claude-code" in script and "@REDAPP_BASE_URL@" not in script
                assert "https://downloads.claude.ai" not in script
        process.terminate()
        assert process.wait(timeout=20) == 0, "正常停止失败"
        log.close()
        log = (directory / "restart.log").open("w+")
        env["REDAPP_PUBLIC_URL"] = ""
        process = subprocess.Popen([str(root / "bin/redapp")], env=env, stdout=log, stderr=log)
        for _ in range(100):
            try:
                with urllib.request.urlopen(base + "/health/ready", timeout=1):
                    break
            except OSError:
                time.sleep(0.05)
        subprocess.run([str(root / "bin/redapp"), "healthcheck"], env=env, check=True)
        with urllib.request.urlopen(base + "/install.sh") as response:
            assert base in response.read().decode(), "自动 origin 未写入安装器"
        with urllib.request.urlopen(base + "/api/info") as response:
            assert json.load(response)["site"] == site_settings, "站点设置未持久化"
        process.terminate()
        assert process.wait(timeout=20) == 0
        log.flush()
        log.seek(0)
        assert "Initial admin password" not in log.read(), "重启再次输出密码"
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        log.close()
print("真实 CLI：启动、健康检查、登录、CSRF 设置、状态 API、企业脚本、SIGTERM、重启密码不重复通过。")
