#!/usr/bin/env python3
"""Local trusted Codex fixture: cached binaries, retention execution and restart receipts."""
import hashlib
import json
import sqlite3
import urllib.parse

from cli_test_support import QuietHandler, ServerTestCase, main

APP = "/admin/api/apps/retention/binary"


class RetentionFixture(QuietHandler):
    """Codex-style release metadata and binaries for any requested version."""

    def do_GET(self):
        """Serve release JSON for channel/release paths and binaries otherwise."""
        path = urllib.parse.urlsplit(self.path).path
        pieces = path.strip("/").split("/")
        version = pieces[1] if pieces[0] == "releases" else "2.0.0"
        body = ("binary:" + version).encode()
        if pieces[0] == "channels" or path.endswith("/release.json"):
            release = {
                "tag_name": "rust-v" + version,
                "assets": [
                    {
                        "name": "asset.tgz",
                        "digest": "sha256:" + hashlib.sha256(body).hexdigest(),
                        "browser_download_url": f"http://retention.example/releases/{version}/asset.tgz",
                    }
                ],
            }
            body = json.dumps(release).encode()
        self.send_body(body)


def count(database, sql, *parameters):
    """Return the single integer result of ``sql``."""
    with sqlite3.connect(database) as db:
        return db.execute(sql, parameters).fetchone()[0]


class RetentionCLITest(ServerTestCase):
    """Retention keeps the newest versions, records a receipt and is idempotent after restart."""

    def patch_retention(self, admin, enabled):
        """Set retention to keep one version, enabled or not; return the new revision."""
        values = {"retention": {"enabled": enabled, "keep_latest": 1}}
        return admin.patch_app_configuration("retention/binary", values)["revision"]

    def test_retention_execution_receipt_survives_restart(self):
        fixture = self.start_fixture(RetentionFixture)
        server = self.start_server()
        admin = server.admin()
        proxy = admin.request("/admin/api/settings/proxy")
        admin.request(
            "/admin/api/settings/proxy",
            {"mode": "url", "url": fixture.url},
            method="PUT",
            if_match=proxy["revision"],
        )
        admin.create_vendor("retention", {"en": "Retention", "zh-CN": "保留"})
        admin.create_app("retention", "binary", "codex", "http://retention.example")
        for version in ["1.0.0", "2.0.0", "10.0.0"]:
            body = admin.request(f"/retention/binary/releases/{version}/asset.tgz")
            self.assertEqual(body, ("binary:" + version).encode(), version)

        self.assertIsNone(admin.request(APP + "/retention/status")["last_run"])
        revision = self.patch_retention(admin, enabled=False)
        admin.fetch(APP + "/retention/preview", method="POST", if_match=revision - 1, expect=409)
        preview = admin.request(APP + "/retention/preview", method="POST", if_match=revision, expect=201)
        self.assertEqual(preview["selected_versions"], 1, preview)
        self.assertIsNone(preview["result"], preview)
        items = admin.request(f"{APP}/retention/{preview['id']}/items")
        self.assertEqual([item["version"] for item in items["items"]], ["10.0.0", "2.0.0", "1.0.0"])
        self.assertEqual([item["selected"] for item in items["items"]], [False, False, True])
        result = admin.request(f"{APP}/retention/{preview['id']}/execute", method="POST")
        self.assertEqual(result["result"]["retired_versions"], 1, result)
        self.assertEqual(result["result"]["logical_bytes"], len("binary:1.0.0"), result)
        self.assertEqual(admin.request(APP + "/retention/status")["last_run"]["outcome"], "success")

        database = server.state_database()
        current_complete = "SELECT count(*) FROM generations WHERE phase='complete' AND is_current=1"
        self.assertEqual(count(database, current_complete), 2)
        retired_requests = "SELECT sum(artifact_requests) FROM app_versions WHERE version='1.0.0'"
        self.assertEqual(count(database, retired_requests), 1, "statistics of the retired version were lost")
        receipt_sql = "SELECT result_json FROM previews WHERE id=?"
        receipt = count(database, receipt_sql, preview["id"])
        # Enable automatic retention; startup must not run an immediate cleanup.
        self.patch_retention(admin, enabled=True)

        server.restart()
        admin.login()
        repeated = admin.request(f"{APP}/retention/{preview['id']}/execute", method="POST")
        self.assertEqual(repeated, result, "repeated execution returned a different receipt")
        self.assertEqual(admin.request(f"{APP}/retention/{preview['id']}"), result)
        self.assertEqual(admin.request(APP + "/retention/status")["last_run"]["outcome"], "success")
        self.assertEqual(count(database, receipt_sql, preview["id"]), receipt)
        self.assertEqual(count(database, "SELECT count(*) FROM previews"), 1, "startup ran an immediate cleanup")
        self.assertEqual(count(database, current_complete), 2)


if __name__ == "__main__":
    main()
