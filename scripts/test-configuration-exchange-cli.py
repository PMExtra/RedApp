#!/usr/bin/env python3
"""Native HTTP instances A→B: YAML/ZIP portability, omissions, copy and receipt restart."""
import hashlib
import io
import json
import sqlite3
import uuid
import zipfile

from cli_test_support import ServerTestCase, main

NAME = {"en": "Portable", "zh-CN": "可移植"}
PRIVATE_PROXY = "http://private-user:private-password@127.0.0.1:3128"
NOTES_SENTINEL = "private-notes-sentinel"


def package_files(package):
    """Return the ZIP members; every member must live under ``presets/``."""
    with zipfile.ZipFile(io.BytesIO(package)) as archive:
        files = {name: archive.read(name) for name in archive.namelist()}
    outside = [name for name in files if not name.startswith("presets/")]
    if outside:
        raise AssertionError(f"package members outside presets/: {outside}")
    return files


def count(database, sql, *parameters):
    """Return the single integer result of ``sql``."""
    with sqlite3.connect(database) as db:
        return db.execute(sql, parameters).fetchone()[0]


class Instance:
    """One server plus its administrator session and exchange helpers."""

    def __init__(self, server):
        self.server = server
        self.admin = server.admin()

    def request(self, path, body=None, **options):
        """Administrator request returning decoded JSON or bytes."""
        return self.admin.request(path, body, **options)

    def configuration(self, key):
        """Configuration document of application ``vendor/app``."""
        return self.request(f"/admin/api/apps/{key}/configuration")

    def patch_configuration(self, path, revision, values, **extra):
        """PATCH a sparse configuration change."""
        body = {"revision": revision, "set": values, "unset": [], **extra}
        return self.request(path, body, method="PATCH")

    def preview(self, package, choices=None):
        """Upload a package as multipart form data and return the import preview."""
        boundary = "redapp-" + uuid.uuid4().hex
        raw = (
            f"--{boundary}\r\n"
            'Content-Disposition: form-data; name="file"; filename="ignored-name.zip"\r\n'
            "Content-Type: application/octet-stream\r\n\r\n"
        ).encode()
        raw += package
        raw += (
            f"\r\n--{boundary}\r\n"
            'Content-Disposition: form-data; name="choices"\r\n\r\n'
        ).encode()
        raw += json.dumps(choices or []).encode()
        raw += f"\r\n--{boundary}--\r\n".encode()
        return self.request(
            "/admin/api/configuration/import/preview",
            raw=raw,
            method="POST",
            content_type=f"multipart/form-data; boundary={boundary}",
        )

    def execute(self, preview, trust=True, expect=200):
        """Execute a previewed import."""
        return self.request(
            f"/admin/api/configuration/import/{preview['id']}/execute",
            {"confirm": True, "trust_instructions": trust},
            method="POST",
            expect=expect,
        )

    def database(self):
        """Path to this instance's state database."""
        return self.server.state_database()


class ConfigurationExchangeCLITest(ServerTestCase):
    """Export from instance A, import into instance B, copy within B and replay receipts."""

    def prepare_source(self, a):
        """Create a vendor with a credentialed proxy and a linked Codex copy with overrides on A."""
        a.request(
            "/admin/api/vendors",
            {"id": "portable", "name": NAME, "icon": "/assets/presets/builtin/openai.svg"},
            method="POST",
            expect=201,
        )
        vendor = a.request("/admin/api/vendors/portable/configuration")
        a.patch_configuration(
            "/admin/api/vendors/portable/configuration",
            vendor["revision"],
            {"proxy": {"mode": "url", "url": PRIVATE_PROXY}},
        )
        original = a.request("/admin/api/apps/openai/codex")["app"]
        source = a.request(
            "/admin/api/apps/openai/codex/copy",
            {
                "source_uid": original["uid"],
                "source_revision": original["revision"],
                "target_vendor": "portable",
                "target_id": "source",
                "mode": "linked",
            },
            method="POST",
            expect=201,
        )["app"]
        configuration = a.configuration("portable/source")
        a.patch_configuration(
            "/admin/api/apps/portable/source/configuration",
            configuration["revision"],
            {
                "name.en": configuration["effective"]["name"]["en"],
                "description.zh-CN": "",
                "instructions.en": '## Portable\n\n<script>fetch("https://must-not-fetch.invalid")</script>\n',
                "categories": [],
                "tags": ["CLI", "命令行"],
                "prewarm": {"enabled": False, "channels": [], "platforms": []},
                "retention": {"enabled": True, "keep_latest": 2},
                "proxy": {"mode": "inherit"},
            },
            new_categories=["Tools"],
        )
        configuration = a.configuration("portable/source")
        self.assertEqual(configuration["effective"]["categories"], ["tools"])
        a.request(
            "/admin/api/apps/portable/source/admin-notes",
            {"revision": 0, "text": NOTES_SENTINEL},
            method="PUT",
        )
        return source, configuration

    def export(self, a, mode, **options):
        """Export the portable/source application as a ZIP package."""
        body = {
            "selection": [{"kind": "App", "key": "portable/source"}],
            "mode": mode,
            "include_notes": False,
            "include_proxy_credentials": False,
            **options,
        }
        return a.request("/admin/api/configuration/export", body, method="POST")

    def test_export_import_copy_and_receipt_replay(self):
        a = Instance(self.start_server("a"))
        source, source_configuration = self.prepare_source(a)
        linked = self.export(a, "linked")
        independent = self.export(a, "independent")
        sensitive = self.export(a, "linked", include_notes=True, include_proxy_credentials=True)

        files = package_files(linked)
        for name in ["presets/portable.yaml", "presets/portable/source.yaml", "presets/_taxonomy.yaml"]:
            self.assertIn(name, files)
        self.assertTrue(any(name.startswith("presets/assets/") for name in files), "icon asset not exported")
        for name, value in files.items():
            for secret in [b"private-user", b"private-password", NOTES_SENTINEL.encode()]:
                self.assertNotIn(secret, value, f"default export leaked {secret!r} in {name}")
        self.assertIn(b"omitted_fields:", files["presets/portable.yaml"])
        self.assertIn(b"|", files["presets/portable/source.yaml"], "instructions not exported as a block scalar")
        self.assertIn(b"template:", files["presets/portable/source.yaml"])
        sensitive_files = package_files(sensitive)
        self.assertIn(b"private-password", sensitive_files["presets/portable.yaml"])
        self.assertIn(NOTES_SENTINEL.encode(), sensitive_files["presets/portable/source.yaml"])
        source_icon = a.request(source_configuration["effective"]["icon"])
        a.server.stop()

        b = Instance(self.start_server("b"))
        # The omitted vendor proxy must be resolved, and instructions explicitly trusted.
        unresolved = b.preview(linked)
        self.assertFalse(unresolved["preview"]["ready"])
        b.execute(unresolved, expect=400)
        preview = b.preview(linked, [{"kind": "Vendor", "key": "portable", "proxy": {"mode": "direct"}}])
        self.assertTrue(preview["preview"]["ready"])
        b.execute(preview, trust=False, expect=400)
        result = b.execute(preview)
        self.assertEqual(b.execute(preview, trust=False), result, "replayed receipt differs")

        destination = b.request("/admin/api/apps/portable/source")["app"]
        self.assertFalse(destination["enabled"])
        self.assertNotEqual(destination["uid"], source["uid"])
        self.assertEqual(destination["source_epoch"], 1)
        destination_configuration = b.configuration("portable/source")
        self.assertEqual(destination_configuration["template_ref"], "openai/codex")
        self.assertEqual(destination_configuration["overrides"], source_configuration["overrides"])
        self.assertEqual(destination_configuration["effective"], source_configuration["effective"])
        categories = [(c["id"], c["name"]["en"]) for c in b.request("/admin/api/categories")["items"]]
        self.assertEqual(categories, [("tools", "Tools")])
        vendor = b.request("/admin/api/vendors/portable")["vendor"]
        self.assertIn(hashlib.sha256(b.request(vendor["icon"])).hexdigest(), vendor["icon"])

        independent_preview = b.preview(
            independent, [{"kind": "App", "key": "portable/source", "target_id": "independent"}]
        )
        self.assertTrue(independent_preview["preview"]["ready"])
        b.execute(independent_preview)
        independent_configuration = b.configuration("portable/independent")
        self.assertIsNone(independent_configuration["template_ref"])
        expected = dict(source_configuration["effective"])
        actual = dict(independent_configuration["effective"])
        expected.pop("icon")
        actual.pop("icon")
        self.assertEqual(actual, expected)
        self.assertEqual(b.request(independent_configuration["effective"]["icon"]), source_icon)

        # Cross-vendor copies inherit the target vendor proxy and start without runtime state.
        b.request("/admin/api/vendors", {"id": "target", "name": NAME}, method="POST", expect=201)
        target = b.request("/admin/api/vendors/target/configuration")
        b.patch_configuration(
            "/admin/api/vendors/target/configuration",
            target["revision"],
            {"proxy": {"mode": "url", "url": "http://127.0.0.1:3129"}},
        )
        note = b.request("/admin/api/apps/portable/source/admin-notes")
        b.request(
            "/admin/api/apps/portable/source/admin-notes",
            {"revision": note["revision"], "text": "copy-note"},
            method="PUT",
        )
        note = b.request("/admin/api/apps/portable/source/admin-notes")
        for mode in ["linked", "independent"]:
            with self.subTest(copy_mode=mode):
                copied = b.request(
                    "/admin/api/apps/portable/source/copy",
                    {
                        "source_uid": destination["uid"],
                        "source_revision": destination_configuration["revision"],
                        "target_vendor": "target",
                        "target_id": mode,
                        "mode": mode,
                        "include_notes": mode == "independent",
                        "notes_revision": note["revision"],
                    },
                    method="POST",
                    expect=201,
                )["app"]
                self.assertNotEqual(copied["uid"], destination["uid"])
                self.assertFalse(copied["enabled"])
                self.assertEqual(copied["source_epoch"], 1)
                copied_configuration = b.configuration("target/" + mode)
                self.assertEqual(copied_configuration["effective"]["proxy"], {"mode": "inherit"})
                self.assertEqual(copied_configuration["proxy_effective"]["source_id"], "target")
                self.assertEqual(copied_configuration["template_ref"] is not None, mode == "linked")
                self.assertEqual(copied_configuration["effective"]["categories"], ["tools"])
                self.assertEqual(copied_configuration["effective"]["tags"], ["CLI", "命令行"])
                copied_note = b.request(f"/admin/api/apps/target/{mode}/admin-notes")
                self.assertEqual(copied_note["text"], "copy-note" if mode == "independent" else "")
                for table in ["prewarm_jobs", "download_sketches", "hosted_files"]:
                    rows = count(b.database(), f"SELECT count(*) FROM {table} WHERE app_uid=?", copied["uid"])
                    self.assertEqual(rows, 0, f"copy carried runtime state in {table}")

        # A preview created before restart is stale afterwards; executed receipts replay unchanged.
        pending = b.preview(linked)
        b.server.restart()
        b.admin.login()
        b.execute(pending, expect=409)
        current = b.configuration("portable/source")
        applications = count(b.database(), "SELECT count(*) FROM applications")
        self.assertEqual(b.execute(preview, trust=False), result)
        self.assertEqual(b.configuration("portable/source"), current, "receipt replay changed configuration")
        self.assertEqual(count(b.database(), "SELECT count(*) FROM applications"), applications)

        # Receipts are still protected by CSRF and the session.
        csrf = b.admin.csrf
        b.admin.csrf = ""
        b.execute(preview, expect=403)
        b.admin.csrf = csrf
        b.admin.logout()
        b.execute(preview, expect=401)
        b.admin.login()
        self.assertEqual(b.execute(preview, trust=False), result)
        receipts = count(
            b.database(), "SELECT count(*) FROM configuration_import_receipts WHERE id=?", preview["id"]
        )
        self.assertEqual(receipts, 1)
        public = json.dumps(b.request("/api/bootstrap"))
        self.assertNotIn("private-password", public)
        self.assertNotIn("copy-note", public)


if __name__ == "__main__":
    main()
