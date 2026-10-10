#!/usr/bin/env python3
"""Native CLI: multi-category save/rename/cleanup, free tags, public counts/privacy, restart."""
import json

from cli_test_support import ServerTestCase, main

NAME = {"en": "Tools", "zh-CN": "工具"}
CODEX = "/admin/api/apps/openai/codex/configuration"
OWN = "/admin/api/apps/taxonomy/one/configuration"
PRIVATE_FIELDS = ["defaults", "overrides", "proxy_effective", "source_epoch", "base_url", "tags", "related"]


class TaxonomyCLITest(ServerTestCase):
    """Categories and tags through application saves, public catalog and restarts."""

    def setUp(self):
        super().setUp()
        self.server = self.start_server()
        self.admin = self.server.admin()

    def save(self, app, body, expect=200):
        """PATCH an application configuration at its current revision (If-Match)."""
        path = f"/admin/api/apps/taxonomy/{app}/configuration"
        revision = self.admin.request(path)["revision"]
        return self.admin.request(path, {"unset": [], **body}, method="PATCH", if_match=revision, expect=expect)

    def categories(self):
        """Admin category list keyed by ID."""
        return {item["id"]: item for item in self.admin.request("/admin/api/categories?limit=100")["items"]}

    def restart(self):
        """Restart the server and sign in again."""
        self.server.restart()
        self.admin.login()

    def create_fixture_apps(self):
        """Create a vendor with two enabled and one disabled info app."""
        self.admin.request(
            "/admin/api/vendors", {"id": "taxonomy", "name": NAME, "enabled": True}, method="POST", expect=201
        )
        for app in ["one", "two", "disabled"]:
            self.admin.request(
                "/admin/api/apps",
                {"vendor": "taxonomy", "id": app, "name": NAME, "provider": "info", "enabled": app != "disabled"},
                method="POST",
                expect=201,
            )

    def test_categories_tags_public_catalog_and_cleanup(self):
        self.create_fixture_apps()
        # Typed categories are created with the app save; tags are free text without a dictionary.
        one = self.save(
            "one",
            {"set": {"categories": [], "tags": [" #CLI ", "cli", "命令行"]}, "new_categories": ["Tools", "效率工具"]},
        )
        chinese = next(id for id in one["effective"]["categories"] if id != "tools")
        self.assertEqual(one["effective"]["categories"], sorted(["tools", chinese]))
        self.assertEqual(one["effective"]["tags"], ["CLI", "命令行"], "tags are not trimmed and deduplicated")
        self.save("two", {"set": {"categories": ["tools"]}})
        self.save("disabled", {"set": {"categories": ["tools"]}, "new_categories": ["Private only"]})
        # Re-requesting an existing category under another spelling reuses it.
        self.save("two", {"set": {"categories": ["tools"]}, "new_categories": ["tools ", "TOOLS"]})
        self.assertEqual(set(self.categories()), {"tools", chinese, "private-only"})
        self.assertEqual(self.categories()["tools"]["applications"], 3)

        # Renaming a category changes the public revision but not the app configuration revision.
        configuration = self.admin.request(OWN)
        public_revision = self.admin.request("/api/bootstrap")["revision"]
        tools = self.categories()["tools"]
        stale = self.admin.request(
            "/admin/api/categories/tools", {"set": {"name.en": "Renamed"}}, method="PATCH", if_match=tools["revision"]
        )
        self.assertEqual(stale["revision"], tools["revision"] + 1)
        self.admin.fetch(
            "/admin/api/categories/tools", {"set": {"name.en": "Again"}}, method="PATCH", if_match=tools["revision"], expect=409
        )
        self.assertEqual(self.admin.request("/admin/api/categories/tools")["name"]["en"], "Renamed")
        self.assertEqual(self.admin.request(OWN)["revision"], configuration["revision"])
        self.assertNotEqual(self.admin.request("/api/bootstrap")["revision"], public_revision)

        catalog = self.admin.request("/api/catalog?category=tools&q=two&limit=1")
        self.assertEqual(catalog["total"], 1)
        self.assertEqual(catalog["items"][0]["key"], "taxonomy/two")
        expected_categories = sorted(
            [
                {"id": "tools", "name": {"en": "Renamed", "zh-CN": "Tools"}, "count": 2},
                {"id": chinese, "name": {"en": "效率工具", "zh-CN": "效率工具"}, "count": 1},
            ],
            key=lambda item: item["id"],
        )
        self.assertEqual(catalog["categories"], expected_categories, "disabled apps were counted")
        tagged = self.admin.request("/api/catalog?q=%23cli")
        self.assertEqual([item["key"] for item in tagged["items"]], ["taxonomy/one"])
        self.assertEqual(tagged["categories"], catalog["categories"])
        public_documents = [catalog, tagged, self.admin.request("/api/bootstrap"), self.admin.request("/api/search?q=cli")]
        for document in public_documents:
            text = json.dumps(document, ensure_ascii=False)
            for key in PRIVATE_FIELDS:
                self.assertNotIn(f'"{key}"', text, "private field in public response")
            for private in ["private-only", "taxonomy/disabled", "命令行"]:
                self.assertNotIn(private, text, "private data in public response")
        self.admin.fetch("/api/apps/taxonomy/one/related", expect=404)
        created = self.admin.fetch("/admin/api/categories", {"id": "x"}, method="POST", expect=None)
        self.assertIn(created.status, (404, 405), "categories are only created through applications")

        # Removing the last reference deletes the category; restart keeps the rest.
        self.save("one", {"set": {"categories": ["tools"]}})
        self.assertNotIn(chinese, self.categories())
        self.restart()
        self.assertEqual(set(self.categories()), {"tools", "private-only"})
        self.assertEqual(self.admin.request(OWN)["effective"]["tags"], ["CLI", "命令行"])
        for app in ["one", "two", "disabled"]:
            self.save(app, {"set": {"categories": [], "tags": []}})
        self.restart()
        self.assertEqual(self.admin.request("/admin/api/categories")["total"], 0)
        self.assertEqual(self.admin.request(OWN)["effective"]["tags"], [])

    def test_stale_session_cannot_overwrite_and_field_reset_survives_restart(self):
        # Two administrator sessions: the stale form is rejected instead of overwriting the newer save.
        second = self.server.admin()
        stale = second.request(CODEX)
        first = self.admin.request(CODEX)
        self.admin.request(
            CODEX,
            {"set": {"name.en": "First session", "tags": ["first"]}, "unset": []},
            method="PATCH",
            if_match=first["revision"],
        )
        conflict = second.fetch(
            CODEX,
            {"set": {"name.zh-CN": "第二会话"}, "unset": []},
            method="PATCH",
            if_match=stale["revision"],
            expect=409,
        )
        self.assertEqual(conflict.value()["error"]["code"], "REVISION_CONFLICT")
        current = self.admin.request(CODEX)
        self.assertEqual(current["effective"]["name"]["en"], "First session")
        self.assertEqual(current["fields"]["name.zh-CN"]["source"], "inherited")
        # Field reset is an unset: after restart the field follows the template, others keep overrides.
        self.admin.request(CODEX, {"set": {}, "unset": ["name.en"]}, method="PATCH", if_match=current["revision"])
        self.restart()
        restarted = self.admin.request(CODEX)
        self.assertEqual(restarted["fields"]["name.en"]["source"], "inherited")
        self.assertEqual(restarted["effective"]["name"]["en"], restarted["defaults"]["name"]["en"])
        self.assertEqual(restarted["fields"]["tags"]["source"], "custom")
        self.assertEqual(restarted["effective"]["tags"], ["first"])


if __name__ == "__main__":
    main()
