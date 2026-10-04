#!/usr/bin/env python3
"""Smoke-test the real server, explicit routes, persistent settings and restart."""
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
import urllib.parse
import urllib.request

root = Path(__file__).resolve().parents[1]
binary = str(root / "bin/redapp")
version_output = subprocess.check_output([binary, "version"], text=True)
assert re.fullmatch(r"RedApp [^\s]+ \(commit [^\s]+\)\n", version_output)

with tempfile.TemporaryDirectory(prefix="redapp-http-cli-") as temp:
    directory = Path(temp)
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    env = {key: value for key, value in os.environ.items() if not key.startswith("REDAPP_")}
    env["REDAPP_PUBLIC_URL"] = "https://environment.example.test"
    env["REDAPP_DATA"] = str(directory / "data")
    env["REDAPP_LISTEN"] = f"127.0.0.1:{port}"
    env["REDAPP_MAX_ARTIFACT_BYTES"] = "4gb"
    env["REDAPP_MAX_WRITERS"] = "20"
    env["REDAPP_MAX_READERS"] = "600"
    args = [binary]  # No arguments and no configuration file.
    log = (directory / "server.log").open("w+")
    process = subprocess.Popen(args, env=env, stdout=log, stderr=log)
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    csrf = ""

    def request(path, body=None, method=None, revision=None):
        headers = {"Origin": base}
        if body is not None:
            headers.update({"Content-Type": "application/json", "X-CSRF-Token": csrf})
        if revision is not None:
            headers["If-Match"] = f'"{revision}"'
        return opener.open(urllib.request.Request(base + path,
                           data=None if body is None else json.dumps(body).encode(),
                           headers=headers, method=method), timeout=5)

    def read(path):
        with request(path) as response:
            return json.load(response)

    def write(path, body, revision):
        with request(path, body, "PUT", revision) as response:
            result = json.load(response)
            assert response.headers["ETag"] == f'"{result["revision"]}"'
            return result

    def ready():
        for _ in range(100):
            if process.poll() is not None:
                raise AssertionError("server exited before readiness; inspect its private temporary log")
            try:
                with urllib.request.urlopen(base + "/health/ready", timeout=1) as response:
                    assert response.status == 200
                return
            except OSError:
                time.sleep(0.05)
        raise AssertionError("server did not become ready")

    def reject(path, expected, body=None, method=None, revision=None):
        try:
            request(path, body, method, revision)
            raise AssertionError(f"unexpected success: {path}")
        except urllib.error.HTTPError as error:
            assert error.code == expected, (path, error.code)

    try:
        ready()
        # Healthcheck must work even though publication origin differs from listener.
        subprocess.run([binary, "healthcheck"], env=env, check=True)
        # Valid incoming authorities do not require a deployment allowlist.
        # PUBLIC_URL only selects generated links; authentication still applies.
        for host in ["remote.example:9443", "environment.example.test", "[2001:db8::1]:8080"]:
            with opener.open(urllib.request.Request(base + "/api/bootstrap", headers={"Host": host}), timeout=5) as response:
                assert json.load(response)["public_origin"] == env["REDAPP_PUBLIC_URL"]
            try:
                opener.open(urllib.request.Request(base + "/admin/api/status", headers={"Host": host}), timeout=5)
                raise AssertionError("custom Host bypassed authentication")
            except urllib.error.HTTPError as error:
                assert error.code == 401, error.code
        for host in ["bad_host", "example.com:0", "example.com:65536"]:
            try:
                opener.open(urllib.request.Request(base + "/health/ready", headers={"Host": host}), timeout=5)
                raise AssertionError("invalid Host accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 400, error.code
        log.flush()
        log.seek(0)
        match = re.search(r"Initial admin password: ([0-9a-f]+)", log.read())
        assert match, "initial password missing"
        bootstrap = read("/api/bootstrap")
        assert bootstrap["version"] == version_output.split()[1]
        assert bootstrap["public_origin"] == env["REDAPP_PUBLIC_URL"]
        apps = bootstrap["apps"]
        assert apps == [], "fresh templates must not be publicly enabled"
        for path in ["/api/info", "/apps/codex", "/install.sh"]:
            reject(path, 404)
        reject("/admin/api/status", 401)
        with request("/admin/api/login", {"password": match.group(1)}, "POST") as response:
            csrf = json.load(response)["csrf"]
        assert read("/admin/api/session")["csrf"] == csrf
        assert read("/admin/api/vendors?page=1&limit=12")["total"] == 2
        for vendor, app in [("openai", "codex"), ("anthropic", "claude-code")]:
            v = read(f"/admin/api/vendors/{vendor}")["vendor"]
            a = read(f"/admin/api/apps/{vendor}/{app}")["app"]
            assert not v["enabled"] and not a["enabled"] and a["builtin_template"]
            with request(f"/admin/api/vendors/{vendor}", {"enabled": True}, "PATCH", v["revision"]) as response:
                assert response.status == 200
            with request(f"/admin/api/apps/{vendor}/{app}", {"enabled": True}, "PATCH", a["revision"]) as response:
                assert response.status == 200
        for path in ["/", "/openai/codex", "/anthropic/claude-code", "/admin/settings/site",
                     "/admin/vendors/openai/apps/codex/settings"]:
            with request(path) as response:
                assert response.status == 200 and "text/html" in response.headers["Content-Type"]
                assert "script-src 'self'" in response.headers["Content-Security-Policy"]
                assert response.headers["Cache-Control"] == "no-store"
                assert "/assets/" in response.read().decode()
        # The dynamic directory is independent of public catalog ordering. These
        # management-only fixtures never contact the configured upstream.
        assert {provider["key"] for provider in read("/admin/api/providers")["providers"]} == {"info", "hosted", "http-cache", "codex", "claude-code"}
        with request("/admin/api/vendors", {"id": "cli-example", "name": {"en": "CLI fixture", "zh-CN": "CLI 测试"}}, "POST") as response:
            vendor = json.load(response)["vendor"]
        assert vendor["revision"] == 1 and vendor["enabled"]
        app_create = "/admin/api/vendors/cli-example/apps"
        app_input = {"id": "files", "name": {"en": "Files", "zh-CN": "文件"}, "provider": "http-cache"}
        reject(app_create, 400, app_input, "POST")  # HTTP Cache has no implicit BaseUrl.
        app_input["base_url"] = "http://127.0.0.1:9/files"
        with request(app_create, app_input, "POST") as response:
            dynamic_app = json.load(response)["app"]
        app_path = "/admin/api/apps/cli-example/files"
        assert dynamic_app["source_epoch"] == 1 and dynamic_app["cache_ttl_seconds"] == 300
        assert "cli-example/files" in {app["id"] for app in read("/api/bootstrap")["apps"]}
        assert read(app_path + "/cache") == {"items": []}
        reject(app_path, 400, {"id": "renamed"}, "PATCH", dynamic_app["revision"])
        reject(app_path, 400, {"provider": "codex"}, "PATCH", dynamic_app["revision"])
        with request(app_path, {"base_url": "http://127.0.0.1:9/replacement"}, "PATCH", dynamic_app["revision"]) as response:
            dynamic_app = json.load(response)["app"]
        assert dynamic_app["source_epoch"] == 2
        reject(app_path, 409, {"enabled": False}, "PATCH", dynamic_app["revision"] - 1)
        sources = read(app_path + "/sources")["sources"]
        assert {source["epoch"] for source in sources} == {1, 2}
        assert [source["epoch"] for source in sources if source["current"]] == [2]
        vendor_path = "/admin/api/vendors/cli-example"
        reject(vendor_path, 409, {"confirm_key": "cli-example"}, "DELETE", vendor["revision"])
        with request(vendor_path, {"enabled": False}, "PATCH", vendor["revision"]) as response:
            vendor = json.load(response)["vendor"]
        assert "cli-example/files" not in {app["id"] for app in read("/api/bootstrap")["apps"]}
        assert read(app_path)["app"]["enabled"], "vendor disable overwrote application state"
        reject("/cli-example/files", 404)
        with request(vendor_path, {"enabled": True}, "PATCH", vendor["revision"]) as response:
            vendor = json.load(response)["vendor"]
        assert "cli-example/files" in {app["id"] for app in read("/api/bootstrap")["apps"]}
        with request(app_path, {"confirm_key": "cli-example/files", "confirm_uid": dynamic_app["uid"]}, "DELETE", dynamic_app["revision"]) as response:
            assert json.load(response)["deleted"]
        reject(app_path + "/sources", 404)
        with request(vendor_path, {"confirm_key": "cli-example"}, "DELETE", vendor["revision"]) as response:
            assert json.load(response)["deleted"]
        status = read("/admin/api/status")
        assert status["name"] == "RedApp" and status["disk"]["free_bytes"] > 0
        assert len(status["metrics"]) == 41
        assert not {"resources", "versions", "application_versions", "events"}.intersection(status)
        for path in ["/admin/api/events", "/admin/api/apps/openai/codex/versions",
                     "/admin/api/apps/anthropic/claude-code/resources"]:
            assert read(path + "?limit=50") == {"items": [], "next_cursor": None}
            reject(path + "?limit=101", 400)
        for window, resolution in [("24h", 60), ("7d", 3600), ("30d", 3600)]:
            query = urllib.parse.urlencode({"scope": "global", "metric": "disk.cache_bytes", "range": window})
            series = read("/admin/api/history?" + query)
            assert series["resolution_seconds"] == resolution
        for app, ttl in [("openai/codex", 120), ("anthropic/claude-code", 180)]:
            path = "/admin/api/apps/" + app + "/settings"
            previous = read(path)
            saved = write(path, {"channel_ttl_seconds": ttl}, previous["revision"])
            assert saved["channel_ttl_seconds"] == ttl
            reject(path, 409, {"channel_ttl_seconds": 300}, "PUT", previous["revision"])
        assert read("/admin/api/apps/openai/codex/settings")["channel_ttl_seconds"] == 120
        reject("/admin/api/settings", 404)
        reject("/admin/api/apps/unknown/tool/settings", 404)
        site_path = "/admin/api/settings/site"
        site = read(site_path)
        site_revision = site.pop("revision")
        site["title"]["en"] = "Fixture tools"
        saved = write(site_path, site, site_revision)
        assert saved["title"]["en"] == "Fixture tools"
        public_path = "/admin/api/settings/public-url"
        public = read(public_path)
        assert public["source"] == "environment"
        public = write(public_path, {"override_url": "https://published.example.test"}, public["revision"])
        assert public["effective_url"] == "https://published.example.test" and public["source"] == "override"
        assert read("/api/bootstrap")["public_origin"] == public["effective_url"]
        for app in ["openai/codex", "anthropic/claude-code"]:
            for name in ["install.sh", "install.ps1"]:
                with request("/" + app + "/" + name) as response:
                    script = response.read().decode()
                    assert "https://published.example.test/" + app in script
                    assert "@REDAPP_BASE_URL@" not in script
                    assert response.headers["Cache-Control"] == "no-store"
        public = write(public_path, {"override_url": None}, public["revision"])
        assert public["source"] == "environment" and public["override_url"] is None
        proxy_path = "/admin/api/settings/proxy"
        proxy = read(proxy_path)
        proxy_url = "http://fixture-user:fixture-only-password@127.0.0.1:3128"
        proxy = write(proxy_path, {"server": proxy_url}, proxy["revision"])
        assert proxy["server"] == proxy_url
        assert "fixture-only-password" not in json.dumps(read("/api/bootstrap"))
        proxy = write(proxy_path, {"server": ""}, proxy["revision"])
        assert proxy["server"] == ""
        with request("/admin/overview") as response:
            assets = re.findall(r'(?:src|href)="(/assets/[^" ]+)"', response.read().decode())
        assert assets
        for asset in assets:
            with request(asset) as response:
                assert response.read()
        process.terminate()
        assert process.wait(timeout=20) == 0
        log.close()
        log = (directory / "restart.log").open("w+")
        env["REDAPP_PUBLIC_URL"] = ""
        config_path = directory / "restart.yaml"
        config_path.write_text(f"# YAML-selected restart of the same data\nlisten: '127.0.0.1:{port}'\ndata_dir: {json.dumps(str(directory / 'data'))}\ndownload_limits: {{max_writers: 20, max_readers: 600, max_artifact_bytes: '4GiB'}}\n")
        env["REDAPP_CONFIG"] = str(config_path)
        del env["REDAPP_DATA"], env["REDAPP_LISTEN"], env["REDAPP_MAX_ARTIFACT_BYTES"], env["REDAPP_MAX_WRITERS"], env["REDAPP_MAX_READERS"]
        process = subprocess.Popen(args, env=env, stdout=log, stderr=log)
        ready()
        bootstrap = read("/api/bootstrap")
        assert bootstrap["site"] == site and bootstrap["public_origin"] == base
        assert {app["id"] for app in bootstrap["apps"]} == {"openai/codex", "anthropic/claude-code"}, "restart reseeded deleted dynamic entries"
        with request("/admin/api/login", {"password": match.group(1)}, "POST") as response:
            csrf = json.load(response)["csrf"]
        reject("/admin/api/apps/cli-example/files", 404)
        reject("/admin/api/apps/cli-example/files/sources", 404)
        for app, ttl in [("openai/codex", 120), ("anthropic/claude-code", 180)]:
            assert read("/admin/api/apps/" + app + "/settings")["channel_ttl_seconds"] == ttl
        subprocess.run([binary, "healthcheck"], env=env, check=True)
        with opener.open(urllib.request.Request(base + "/api/bootstrap", headers={"Forwarded": "proto=https;host=untrusted.example"}), timeout=5) as response:
            assert json.load(response)["public_origin"] == base, "untrusted proxy header affected origin"
        for host in ["remote.example:9443", "[2001:db8::1]:8080"]:
            with opener.open(urllib.request.Request(base + "/api/bootstrap", headers={"Host": host}), timeout=5) as response:
                assert json.load(response)["public_origin"] == "http://" + host
        process.terminate()
        assert process.wait(timeout=20) == 0
        log.flush()
        log.seek(0)
        assert "Initial admin password" not in log.read(), "restart regenerated admin credentials"
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        log.close()
print("CLI file-free environment startup, Host/proxy boundaries, routes/session/CAS, dynamic provider/vendor/app lifecycle and source epochs, PUBLIC_URL/installer updates, YAML-selected persistent restart and shared healthcheck configuration passed.")
