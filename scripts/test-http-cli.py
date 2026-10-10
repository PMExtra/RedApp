#!/usr/bin/env python3
"""Real server: Host/proxy boundaries, routes, sessions, CAS, dynamic entries and restart."""
import json
import re
import subprocess
import urllib.parse

from cli_test_support import BINARY, ROOT, ServerTestCase, main, run_cli

PUBLIC_URL = "https://environment.example.test"
BUILTIN_TEMPLATES = [("openai", "codex"), ("anthropic", "claude-code")]


def environment_config(server, port):
    """File-free start: no arguments, every setting from the environment."""
    environment = {
        "REDAPP_PUBLIC_URL": PUBLIC_URL,
        "REDAPP_DATA": str(server.data_dir),
        "REDAPP_LISTEN": f"127.0.0.1:{port}",
        "REDAPP_MAX_ARTIFACT_BYTES": "4gb",
        "REDAPP_MAX_WRITERS": "20",
        "REDAPP_MAX_READERS": "600",
    }
    return [], environment


def yaml_config(server, port):
    """Restart of the same data selected only through REDAPP_CONFIG (YAML)."""
    config = server.directory / "restart.yaml"
    config.write_text(
        "# YAML-selected restart of the same data\n"
        f"listen: '127.0.0.1:{port}'\n"
        f"data_dir: {json.dumps(str(server.data_dir))}\n"
        "download_limits: {max_writers: 20, max_readers: 600, max_artifact_bytes: '4GiB'}\n"
    )
    return [], {"REDAPP_PUBLIC_URL": "", "REDAPP_CONFIG": str(config)}


class HTTPServerCLITest(ServerTestCase):
    """End-to-end HTTP behaviour of a server started without a configuration file."""

    def setUp(self):
        super().setUp()
        self.server = self.start_server(configure=environment_config)
        self.client = self.server.session()

    def read(self, path):
        """GET a JSON document with status 200."""
        return self.client.request(path)

    def write(self, path, body, revision):
        """PUT settings with If-Match and check that ETag matches the new revision."""
        response = self.client.fetch(path, body, method="PUT", if_match=revision)
        result = response.json()
        self.assertEqual(response.headers["ETag"], f'"{result["revision"]}"', path)
        return result

    def enable_builtin_templates(self):
        """Enable the built-in vendors and apps, which start disabled."""
        for vendor, app in BUILTIN_TEMPLATES:
            v = self.read(f"/admin/api/vendors/{vendor}")
            a = self.read(f"/admin/api/apps/{vendor}/{app}")
            self.assertFalse(v["enabled"])
            self.assertFalse(a["enabled"])
            self.assertTrue(a["builtin_template"])
            self.client.fetch(f"/admin/api/vendors/{vendor}", {"enabled": True}, method="PATCH", if_match=v["revision"])
            self.client.fetch(f"/admin/api/apps/{vendor}/{app}", {"enabled": True}, method="PATCH", if_match=a["revision"])

    def public_app_ids(self):
        """Keys of the published applications."""
        return {app["key"] for app in self.read("/api/catalog?limit=100")["items"]}

    def test_version_output_format(self):
        output = subprocess.check_output([str(BINARY), "version"], text=True)
        self.assertRegex(output, r"\ARedApp [^\s]+ \(commit [^\s]+\)\n\Z")

    def test_incoming_authority_and_public_url_boundaries(self):
        # Healthcheck uses the listener even though the published origin differs.
        healthcheck = run_cli(["healthcheck"], self.server.directory, self.server.environment)
        self.assertEqual(healthcheck.returncode, 0, healthcheck.stderr)
        # Valid authorities need no allowlist; PUBLIC_URL only selects generated
        # links, and authentication still applies under any Host.
        for host in ["remote.example:9443", "environment.example.test", "[2001:db8::1]:8080"]:
            with self.subTest(host=host):
                headers = {"Host": host, "Origin": None}
                bootstrap = self.client.request("/api/bootstrap", headers=headers)
                self.assertEqual(bootstrap["public_url"], PUBLIC_URL)
                self.client.fetch("/admin/api/status", headers=headers, expect=401)
        for host in ["bad_host", "example.com:0", "example.com:65536"]:
            with self.subTest(host=host):
                self.client.fetch("/health/ready", headers={"Host": host, "Origin": None}, expect=400)

    def test_fresh_instance_routes_session_and_templates(self):
        version = subprocess.check_output([str(BINARY), "version"], text=True).split()[1]
        bootstrap = self.read("/api/bootstrap")
        self.assertEqual(bootstrap["version"], version)
        self.assertEqual(bootstrap["public_url"], PUBLIC_URL)
        self.assertNotIn("apps", bootstrap)
        self.assertEqual(self.public_app_ids(), set(), "fresh templates must not be publicly enabled")
        for path in ["/api/info", "/apps/codex", "/install.sh"]:
            self.client.fetch(path, expect=404)
        self.client.fetch("/admin/api/status", expect=401)
        csrf = self.client.login()
        self.assertEqual(self.read("/admin/api/session")["csrf_token"], csrf)
        self.assertEqual(self.read("/admin/api/vendors?page=1&limit=12")["total"], 2)
        self.enable_builtin_templates()
        spa_paths = ["/", "/openai/codex", "/anthropic/claude-code"]
        # Admin pages use the separate admin entry; a bundle without it (the
        # pre-rewrite frontend) must fail instead of serving the public entry.
        has_admin_entry = (ROOT / "internal" / "httpserver" / "web" / "admin.html").exists()
        if has_admin_entry:
            spa_paths += ["/admin/overview", "/admin/settings/site", "/admin/vendors/openai/apps/codex/settings"]
        else:
            self.client.fetch("/admin/overview", expect=500)
        for path in spa_paths:
            with self.subTest(path=path):
                response = self.client.fetch(path)
                self.assertIn("text/html", response.headers["Content-Type"])
                self.assertIn("script-src 'self'", response.headers["Content-Security-Policy"])
                self.assertEqual(response.headers["Cache-Control"], "no-store")
                self.assertIn("/assets/", response.text())
        page = self.client.fetch("/").text()
        if has_admin_entry:
            page += self.client.fetch("/admin/overview").text()
        assets = re.findall(r'(?:src|href)="(/assets/[^" ]+)"', page)
        self.assertTrue(assets, "SPA shell references no assets")
        for asset in assets:
            self.assertTrue(self.client.fetch(asset).body, asset)

    def test_dynamic_vendor_and_app_lifecycle(self):
        # Management-only fixtures; they never contact the configured upstream.
        self.client.login()
        providers = {provider["key"] for provider in self.read("/admin/api/providers")["items"]}
        self.assertEqual(providers, {"info", "hosted", "http-cache", "codex", "claude-code"})
        created = self.client.fetch(
            "/admin/api/vendors",
            {"id": "cli-example", "name": {"en": "CLI fixture", "zh-CN": "CLI 测试"}, "enabled": True},
            method="POST",
            expect=201,
        )
        vendor = created.json()
        self.assertEqual(vendor["revision"], 1)
        self.assertEqual(created.headers["ETag"], '"1"')
        self.assertEqual(created.headers["Location"], "/admin/api/vendors/cli-example")
        self.assertTrue(vendor["enabled"])
        app_create = "/admin/api/apps"
        app_input = {
            "vendor": "cli-example",
            "id": "files",
            "name": {"en": "Files", "zh-CN": "文件"},
            "provider": "http-cache",
            "enabled": True,
        }
        # HTTP Cache has no implicit base URL.
        error = self.client.request(app_create, app_input, method="POST", expect=400)
        self.assertEqual(error["error"]["code"], "VALIDATION_FAILED")
        app_input["base_urls"] = ["http://127.0.0.1:9/files"]
        app = self.client.request(app_create, app_input, method="POST", expect=201)
        app_path = "/admin/api/apps/cli-example/files"
        self.assertEqual(app["source_epoch"], 1)
        self.assertEqual(app["cache_ttl_seconds"], 300)
        self.assertIn("cli-example/files", self.public_app_ids())
        self.assertEqual(self.read(app_path + "/cache/entries"), {"items": [], "next_cursor": None})
        # The entity only toggles its state; everything else is configuration.
        self.client.fetch(app_path, {"id": "renamed"}, method="PATCH", if_match=app["revision"], expect=400)
        self.client.fetch(app_path, {"enabled": False}, method="PATCH", expect=400)
        configuration = self.client.request(
            app_path + "/configuration",
            {"set": {"base_urls": ["http://127.0.0.1:9/replacement"]}},
            method="PATCH",
            if_match=app["revision"],
        )
        app = self.read(app_path)
        self.assertEqual(app["source_epoch"], 2)
        self.assertEqual(configuration["revision"], app["revision"])
        self.client.fetch(app_path, {"enabled": False}, method="PATCH", if_match=app["revision"] - 1, expect=409)
        sources = self.read(app_path + "/sources")["items"]
        self.assertEqual({source["epoch"] for source in sources}, {1, 2})
        self.assertEqual([source["epoch"] for source in sources if source["current"]], [2])

        vendor_path = "/admin/api/vendors/cli-example"
        not_empty = self.client.request(vendor_path, method="DELETE", if_match=vendor["revision"], expect=409)
        self.assertEqual(not_empty["error"]["code"], "VENDOR_NOT_EMPTY")
        vendor = self.client.request(vendor_path, {"enabled": False}, method="PATCH", if_match=vendor["revision"])
        self.assertNotIn("cli-example/files", self.public_app_ids())
        self.assertTrue(self.read(app_path)["enabled"], "vendor disable overwrote application state")
        self.client.fetch("/cli-example/files", expect=404)
        vendor = self.client.request(vendor_path, {"enabled": True}, method="PATCH", if_match=vendor["revision"])
        self.assertIn("cli-example/files", self.public_app_ids())
        delete_app = app_path + "?" + urllib.parse.urlencode({"confirm_uid": app["uid"]})
        self.assertEqual(self.client.request(delete_app, method="DELETE", if_match=app["revision"]), {"cleanup_pending": False})
        self.client.fetch(delete_app, method="DELETE", if_match=app["revision"], expect=404)
        self.client.fetch(app_path + "/sources", expect=404)
        self.client.fetch(vendor_path, method="DELETE", if_match=vendor["revision"], expect=204)
        self.client.fetch(vendor_path, expect=404)

    def test_status_pagination_and_history(self):
        self.client.login()
        status = self.read("/admin/api/status")
        self.assertEqual(set(status), {"sampled_at", "started_at", "metrics"})
        self.assertEqual(len(status["metrics"]), 41)
        metrics = {metric["key"]: metric for metric in status["metrics"]}
        self.assertGreater(metrics["disk.free_bytes"]["value"], 0)
        self.assertEqual(metrics["disk.free_bytes"]["group"], "disk")
        self.assertEqual(
            {metric["group"] for metric in status["metrics"]},
            {"disk", "traffic", "speed", "runtime", "resources"},
        )
        for path in [
            "/admin/api/events",
            "/admin/api/apps/openai/codex/versions",
            "/admin/api/apps/anthropic/claude-code/resources",
        ]:
            with self.subTest(path=path):
                self.assertEqual(self.read(path + "?limit=50"), {"items": [], "next_cursor": None})
                self.client.fetch(path + "?limit=101", expect=400)
        for window, resolution in [("24h", 60), ("7d", 3600), ("30d", 3600)]:
            query = urllib.parse.urlencode({"metric": "disk.cache_bytes", "range": window})
            series = self.read("/admin/api/history?" + query)
            self.assertEqual(series["resolution_seconds"], resolution)
            self.assertRegex(series["from"], r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")
            self.assertIsNone(series["app_key"])
        legacy = urllib.parse.urlencode({"scope": "global", "metric": "disk.cache_bytes", "range": "24h"})
        error = self.client.fetch("/admin/api/history?" + legacy, expect=400).json()["error"]
        self.assertEqual(error["code"], "INVALID_QUERY")

    def test_settings_persist_across_yaml_selected_restart(self):
        password = self.server.initial_password()
        self.client.login()
        self.enable_builtin_templates()
        # Deleted dynamic entries must not be reseeded on restart.
        self.client.request(
            "/admin/api/vendors",
            {"id": "cli-example", "name": {"en": "CLI fixture", "zh-CN": "CLI 测试"}, "enabled": True},
            method="POST",
            expect=201,
        )
        app = self.client.request(
            "/admin/api/apps",
            {
                "vendor": "cli-example",
                "id": "files",
                "name": {"en": "Files", "zh-CN": "文件"},
                "provider": "http-cache",
                "base_urls": ["http://127.0.0.1:9/files"],
                "enabled": True,
            },
            method="POST",
            expect=201,
        )
        self.client.request(
            "/admin/api/apps/cli-example/files?confirm_uid=" + app["uid"],
            method="DELETE",
            if_match=app["revision"],
        )
        vendor = self.read("/admin/api/vendors/cli-example")
        self.client.fetch("/admin/api/vendors/cli-example", method="DELETE", if_match=vendor["revision"], expect=204)
        # The release channel TTL is the configuration path cache_ttl_seconds.
        ttls = [("openai/codex", 120), ("anthropic/claude-code", 180)]
        for app, ttl in ttls:
            path = "/admin/api/apps/" + app + "/configuration"
            previous = self.read(path)
            response = self.client.fetch(
                path, {"set": {"cache_ttl_seconds": ttl}}, method="PATCH", if_match=previous["revision"]
            )
            saved = response.json()
            self.assertEqual(response.headers["ETag"], f'"{saved["revision"]}"')
            self.assertEqual(saved["effective"]["cache_ttl_seconds"], ttl)
            stale = {"set": {"cache_ttl_seconds": 300}}
            self.client.fetch(path, stale, method="PATCH", if_match=previous["revision"], expect=409)
        self.assertEqual(self.read("/admin/api/apps/openai/codex")["cache_ttl_seconds"], 120)
        self.client.fetch("/admin/api/settings", expect=404)
        self.client.fetch("/admin/api/apps/unknown/tool/configuration", expect=404)

        site_path = "/admin/api/settings/site"
        site = self.read(site_path)
        site_revision = site.pop("revision")
        site["title"]["en"] = "Fixture tools"
        self.assertEqual(self.write(site_path, site, site_revision)["title"]["en"], "Fixture tools")

        public_path = "/admin/api/settings/public-url"
        public = self.read(public_path)
        self.assertEqual(public["source"], "environment")
        public = self.write(public_path, {"override_url": "https://published.example.test"}, public["revision"])
        self.assertEqual(public["effective_url"], "https://published.example.test")
        self.assertEqual(public["source"], "override")
        self.assertEqual(self.read("/api/bootstrap")["public_url"], public["effective_url"])
        for app in ["openai/codex", "anthropic/claude-code"]:
            for name in ["install.sh", "install.ps1"]:
                with self.subTest(installer=f"{app}/{name}"):
                    response = self.client.fetch("/" + app + "/" + name)
                    script = response.text()
                    self.assertIn("https://published.example.test/" + app, script)
                    self.assertNotIn("@REDAPP_BASE_URL@", script)
                    self.assertEqual(response.headers["Cache-Control"], "no-store")
        public = self.write(public_path, {"override_url": None}, public["revision"])
        self.assertEqual(public["source"], "environment")
        self.assertIsNone(public["override_url"])

        proxy_path = "/admin/api/settings/proxy"
        proxy = self.read(proxy_path)
        proxy_url = "http://fixture-user:fixture-only-password@127.0.0.1:3128"
        proxy = self.write(proxy_path, {"mode": "url", "url": proxy_url}, proxy["revision"])
        self.assertEqual(proxy["url"], "http://fixture-user:****@127.0.0.1:3128")
        self.assertNotIn("server", proxy)
        self.assertNotIn("fixture-only-password", json.dumps(self.read("/api/bootstrap")))
        proxy = self.write(proxy_path, {"mode": "direct"}, proxy["revision"])
        self.assertEqual(proxy["mode"], "direct")
        self.assertNotIn("url", proxy)

        self.server.stop()
        first_start_log = self.server.log_text()
        self.server.configure = yaml_config
        self.server.start()
        base = self.server.base_url
        bootstrap = self.read("/api/bootstrap")
        self.assertEqual(bootstrap["site"], site)
        self.assertEqual(bootstrap["public_url"], base)
        self.assertEqual(
            self.public_app_ids(),
            {"openai/codex", "anthropic/claude-code"},
            "restart reseeded deleted dynamic entries",
        )
        self.client.login(password)
        self.client.fetch("/admin/api/vendors/cli-example", expect=404)
        self.client.fetch("/admin/api/apps/cli-example/files", expect=404)
        self.client.fetch("/admin/api/apps/cli-example/files/sources", expect=404)
        for app, ttl in ttls:
            self.assertEqual(self.read("/admin/api/apps/" + app)["cache_ttl_seconds"], ttl)
        healthcheck = run_cli(["healthcheck"], self.server.directory, self.server.environment)
        self.assertEqual(healthcheck.returncode, 0, healthcheck.stderr)
        forwarded = {"Forwarded": "proto=https;host=untrusted.example", "Origin": None}
        self.assertEqual(
            self.client.request("/api/bootstrap", headers=forwarded)["public_url"],
            base,
            "untrusted proxy header affected origin",
        )
        for host in ["remote.example:9443", "[2001:db8::1]:8080"]:
            bootstrap = self.client.request("/api/bootstrap", headers={"Host": host, "Origin": None})
            self.assertEqual(bootstrap["public_url"], "http://" + host)
        self.server.stop()
        restart_log = self.server.log_text()[len(first_start_log):]
        self.assertNotIn("Initial admin password", restart_log, "restart regenerated admin credentials")


if __name__ == "__main__":
    main()
