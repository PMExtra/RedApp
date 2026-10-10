#!/usr/bin/env python3
"""Native CLI: cold index and immutable platform prewarm, then restart."""
import hashlib
import json
import sqlite3
import time
import urllib.parse

from cli_test_support import QuietHandler, ServerTestCase, main

PLATFORM_KEY = "codex-npm-linux-x64-1.0.0.tgz"
PLATFORM_BODY = b"verified platform fixture"
JOB_TIMEOUT_SECONDS = 10.0


class PrewarmFixture(QuietHandler):
    """An HTML/JSON directory index plus a Codex release with one platform asset."""

    def do_HEAD(self):
        """Report every resource as unchanged."""
        self.send_response(304)
        self.send_header("ETag", '"fixture"')
        self.end_headers()

    def do_GET(self):
        """Serve index pages, files and the release metadata."""
        path = urllib.parse.urlsplit(self.path).path
        headers = {"ETag": '"fixture"'}
        if path == "/files/":
            body = b'<a href="one">one</a><a href="sub/">sub</a><a href="../escape">ignore</a>'
        elif path == "/files/sub/":
            body = b'[{"name":"two","type":"file","size":4}]'
            headers["Content-Type"] = "application/json"
        elif path.startswith("/files/"):
            body = b"body"
        elif path.endswith("/release.json"):
            release = {
                "tag_name": "rust-v1.0.0",
                "assets": [
                    {
                        "name": PLATFORM_KEY,
                        "digest": "sha256:" + hashlib.sha256(PLATFORM_BODY).hexdigest(),
                        "browser_download_url": "http://prewarm.example/releases/1.0.0/" + PLATFORM_KEY,
                    }
                ],
            }
            body = json.dumps(release).encode()
        else:
            body = PLATFORM_BODY
        self.send_body(body, headers=headers)


def count(database, sql):
    """Return the single integer result of ``sql``."""
    with sqlite3.connect(database) as db:
        return db.execute(sql).fetchone()[0]


class PrewarmCLITest(ServerTestCase):
    """Prewarm jobs fill the HTTP cache and release cache and are idempotent across restart."""

    def await_job(self, admin, key, job):
        """Poll a prewarm job until it leaves the running state."""
        deadline = time.monotonic() + JOB_TIMEOUT_SECONDS
        while time.monotonic() < deadline:
            result = admin.request(f"/admin/api/apps/{key}/prewarm/{job['id']}")
            if result["state"] != "running":
                return result
            time.sleep(0.02)
        self.fail(f"prewarm job {job['id']} for {key} still running after {JOB_TIMEOUT_SECONDS:.0f}s")

    def start_job(self, admin, key, body):
        """Start a prewarm job and return the job description."""
        return admin.request(f"/admin/api/apps/{key}/prewarm/start", body, method="POST")

    def test_index_and_platform_prewarm_survive_restart(self):
        fixture = self.start_fixture(PrewarmFixture)
        server = self.start_server()
        admin = server.admin()
        proxy = admin.request("/admin/api/settings/proxy")
        admin.request(
            "/admin/api/settings/proxy",
            {"mode": "url", "url": fixture.url},
            method="PUT",
            if_match=proxy["revision"],
        )
        admin.request(
            "/admin/api/vendors",
            {"id": "prewarm", "name": {"en": "Prewarm", "zh-CN": "预热"}, "enabled": True},
            method="POST",
            expect=201,
        )
        apps = [
            ("http", "http-cache", "http://prewarm.example/files"),
            ("binary", "codex", "http://prewarm.example"),
        ]
        for name, provider, upstream in apps:
            source = {"base_urls": [upstream]} if provider == "http-cache" else {"base_url": upstream}
            admin.request(
                "/admin/api/apps",
                {
                    "vendor": "prewarm",
                    "id": name,
                    "provider": provider,
                    "name": {"en": name, "zh-CN": name},
                    "cache_ttl_seconds": 60,
                    "enabled": True,
                    **source,
                },
                method="POST",
                expect=201,
            )

        # Cold HTML and JSON (nginx-style) indexes; links outside the base are ignored.
        http_input = {"request_id": "a" * 32, "indexes": ["/"]}
        http_job = self.start_job(admin, "prewarm/http", http_input)
        http_result = self.await_job(admin, "prewarm/http", http_job)
        self.assertEqual(http_result["state"], "completed", http_result)
        self.assertEqual(http_result["succeeded"], 2, http_result)
        items = admin.request(f"/admin/api/apps/prewarm/http/prewarm/{http_job['id']}/items")["items"]
        self.assertEqual([item["key"] for item in items], ["/one", "/sub/two"])

        release_input = {"request_id": "b" * 32, "target": "1.0.0", "platforms": ["linux-x64"]}
        release_job = self.start_job(admin, "prewarm/binary", release_input)
        release_result = self.await_job(admin, "prewarm/binary", release_job)
        self.assertEqual(release_result["state"], "completed", release_result)
        self.assertEqual(release_result["succeeded"], 1, release_result)
        cached = self.await_job(
            admin, "prewarm/binary", self.start_job(admin, "prewarm/binary", {**release_input, "request_id": "c" * 32})
        )
        self.assertEqual(cached["state"], "completed", cached)
        self.assertEqual(cached["bytes"], 0, "complete cache hit downloaded again")

        database = server.state_database()
        self.assertEqual(count(database, "SELECT count(*) FROM http_cache_generations WHERE is_current=1"), 2)
        self.assertEqual(
            count(database, "SELECT count(*) FROM http_cache_generations WHERE last_access_bucket_s != 0"),
            0,
            "prewarm counted as a client access",
        )
        self.assertEqual(
            count(database, "SELECT count(*) FROM generations WHERE phase='complete' AND is_current=1"), 1
        )

        server.restart()
        admin.login()
        replay = self.start_job(admin, "prewarm/http", http_input)
        self.assertEqual(replay["id"], http_job["id"], "same request_id started a new job")
        release_state = admin.request(f"/admin/api/apps/prewarm/binary/prewarm/{release_job['id']}")["state"]
        self.assertEqual(release_state, "completed")
        self.assertEqual(admin.request("/prewarm/http/one"), b"body")
        self.assertEqual(admin.request("/prewarm/binary/releases/1.0.0/" + PLATFORM_KEY), PLATFORM_BODY)
        self.assertEqual(count(database, "SELECT count(*) FROM prewarm_jobs"), 3, "startup scheduled an immediate warm")


if __name__ == "__main__":
    main()
