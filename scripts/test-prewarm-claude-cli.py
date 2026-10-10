#!/usr/bin/env python3
"""Network integration (`make network-test`): official signed Claude manifest and one binary.

This test contacts downloads.claude.ai and downloads a real binary of more
than 200 MB. It is not part of the offline gates.
"""
import json
import sqlite3
import time

from cli_test_support import ROOT, ServerTestCase, main

APP = "/admin/api/apps/signed/claude"
TOTAL_TIMEOUT_SECONDS = 1800
STALL_TIMEOUT_SECONDS = 120


class ClaudeNetworkPrewarmTest(ServerTestCase):
    """Prewarm verifies the official manifest signature and the downloaded digest."""

    def wait(self, admin, job):
        """Wait for a job; slow egress is fine, a stalled download is a failure."""
        deadline = time.monotonic() + TOTAL_TIMEOUT_SECONDS
        progress = -1
        last_progress = time.monotonic()
        while time.monotonic() < deadline:
            result = admin.request(f"{APP}/prewarm/{job['id']}")
            if result["state"] != "running":
                return result
            if result["bytes"] != progress:
                progress = result["bytes"]
                last_progress = time.monotonic()
            elif time.monotonic() - last_progress > STALL_TIMEOUT_SECONDS:
                self.fail(f"official Claude download made no progress for two minutes at {progress} bytes")
            time.sleep(0.2)
        self.fail("official Claude download exceeded the 30-minute integration bound")

    def test_official_signed_release_prewarm_and_cache_hit(self):
        server = self.start_server()
        admin = server.admin()
        manifest = json.loads((ROOT / "internal/apps/claude/testdata/manifest.json").read_text())
        platform = min(manifest["platforms"], key=lambda key: manifest["platforms"][key]["size"])
        admin.request(
            "/admin/api/vendors",
            {"id": "signed", "name": {"en": "Signed fixture", "zh-CN": "签名夹具"}, "enabled": True},
            method="POST",
            expect=201,
        )
        admin.request(
            "/admin/api/vendors/signed/apps",
            {
                "id": "claude",
                "provider": "claude-code",
                "name": {"en": "Claude fixture", "zh-CN": "Claude 夹具"},
                "base_url": "https://downloads.claude.ai/claude-code-releases",
                "cache_ttl_seconds": 60,
                "enabled": True,
            },
            method="POST",
            expect=201,
        )
        job_input = {"request_id": "a" * 32, "target": manifest["version"], "platforms": [platform]}
        result = self.wait(admin, admin.request(f"{APP}/prewarm/start", job_input, method="POST"))
        self.assertEqual(result["state"], "completed", result)
        self.assertEqual(result["succeeded"], 1, result)
        with sqlite3.connect(server.state_database()) as db:
            phase = db.execute("SELECT phase FROM generations WHERE is_current=1").fetchone()[0]
            self.assertEqual(phase, "complete")
            complete = db.execute(
                "SELECT count(*) FROM generations WHERE phase='complete' AND is_current=1"
            ).fetchone()[0]
            self.assertEqual(complete, 1)
        cached_input = {**job_input, "request_id": "b" * 32}
        cached = self.wait(admin, admin.request(f"{APP}/prewarm/start", cached_input, method="POST"))
        self.assertEqual(cached["state"], "completed", cached)
        self.assertEqual(cached["bytes"], 0, "complete cache hit downloaded again")
        print(f"platform {platform}: {result['bytes']} bytes verified, repeated prewarm was a zero-read cache hit")


if __name__ == "__main__":
    main()
