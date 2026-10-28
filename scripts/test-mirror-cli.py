#!/usr/bin/env python3
"""Native CLI: a chain of three RedApp processes mirroring one application."""
import json
import threading

from cli_test_support import QuietHandler, ServerTestCase, main

FILE = b"payload of a.bin"


class Origin(QuietHandler):
    """Origin of A's HTTP cache: one file and an nginx autoindex listing."""

    hits = {}
    lock = threading.Lock()

    def do_GET(self):
        with self.lock:
            self.hits[self.path] = self.hits.get(self.path, 0) + 1
        if self.path == "/files/":
            body = json.dumps([{"name": "a.bin", "type": "file", "size": len(FILE)}]).encode()
            self.send_body(body, headers={"Content-Type": "application/json"})
        elif self.path == "/files/a.bin":
            self.send_body(FILE, headers={"Content-Type": "application/octet-stream", "ETag": '"a1"'})
        else:
            self.send_body(b"not found", status=404)


class MirrorCLITest(ServerTestCase):
    """A serves from an origin; B mirrors A; C mirrors B."""

    def mirror(self, admin, upstream_url, remote_key, vendor, app, expect=201):
        """Add the upstream and mirror remote_key as vendor/app; return the upstream."""
        upstream = admin.request("/admin/api/upstreams", {"name": "up", "origin": upstream_url}, method="POST", expect=201)
        admin.request(
            f"/admin/api/upstreams/{upstream['uid']}/mirrors",
            {"remote_key": remote_key, "vendor": vendor, "app": app},
            method="POST",
            expect=expect,
        )
        return upstream

    def test_three_level_chain(self):
        origin = self.start_fixture(Origin)
        a, b, c = self.start_server("a"), self.start_server("b"), self.start_server("c")
        a_admin, b_admin, c_admin = a.admin(), b.admin(), c.admin()
        ids = {name: s.session().request("/api/instance")["instance_id"] for name, s in (("a", a), ("b", b), ("c", c))}

        a_admin.create_vendor("acme", {"en": "Acme", "zh-CN": "示例"})
        a_admin.create_app("acme", "files", "http-cache", origin.url + "/files")
        a_admin.patch_app_configuration("acme/files", {"auto_index": True, "description.en": "Origin files"})
        self.mirror(b_admin, a.base_url, "acme/files", "mirror", "files")
        c_upstream = self.mirror(c_admin, b.base_url, "mirror/files", "chain", "files")

        # The file passes through the whole chain once; later reads are cached.
        visitor = c.session()
        for _ in range(3):
            self.assertEqual(visitor.fetch("/chain/files/a.bin").body, FILE)
        self.assertEqual(Origin.hits.get("/files/a.bin"), 1, "the origin was asked more than once")
        listing = visitor.request("/chain/files/", headers={"Accept": "application/json"})
        self.assertEqual([entry["name"] for entry in listing["entries"]], ["a.bin"])
        description = visitor.request("/api/apps/chain/files/mirror")
        self.assertEqual(description["upstream_chain"], [ids["b"], ids["a"]])
        self.assertEqual(description["description"]["en"], "Origin files")

        # A mirroring C's mirror of A would close the loop.
        self.mirror(a_admin, c.base_url, "chain/files", "loop", "files", expect=508)

        # Bindings survive a restart, and a check follows A's change. A
        # restarted server listens on a new port, so C moves its upstream.
        b.restart()
        b_admin.login()
        c_upstream = c_admin.request(
            f"/admin/api/upstreams/{c_upstream['uid']}",
            {"name": "up", "origin": b.base_url},
            method="PATCH",
            if_match=c_upstream["revision"],
        )
        a_admin.patch_app_configuration("acme/files", {"description.en": "Renamed origin"})
        b_admin.request("/admin/api/apps/mirror/files/mirror/sync", method="POST")
        state = c_admin.request("/admin/api/apps/chain/files/mirror/sync", method="POST")
        self.assertIsNone(state["error"], state)
        self.assertEqual(state["upstream"]["origin"], b.base_url)
        self.assertEqual(c_admin.request("/admin/api/apps/chain/files")["description"]["en"], "Renamed origin")

        # Without A, cached files are still served downstream; others fail.
        a.stop()
        self.assertEqual(visitor.fetch("/chain/files/a.bin").body, FILE)
        visitor.fetch("/chain/files/missing.bin", expect=502)
        state = c_admin.request("/admin/api/apps/chain/files/mirror")
        self.assertTrue(state["available"])
        c_admin.request(f"/admin/api/upstreams/{c_upstream['uid']}", if_match=c_upstream["revision"], method="DELETE", expect=409)


if __name__ == "__main__":
    main()
