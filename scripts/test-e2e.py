#!/usr/bin/env python3
"""Playwright end-to-end tests of the embedded frontend against a real ``bin/redapp``.

Starts the current binary (it is not rebuilt) with a fresh data directory on a
loopback port, reads the initial administrator password from the first-start
log, publishes one ``info`` application so the public pages have content
without contacting any upstream, and runs ``npm run e2e`` in ``frontend/``
with ``REDAPP_E2E_URL``, ``REDAPP_E2E_PASSWORD`` and ``REDAPP_E2E_INFO_APP``.
Extra arguments are passed to Playwright (for example a spec file).

Needs ``frontend/node_modules`` and the Playwright Chromium browser
(``cd frontend && npx playwright install --with-deps chromium``).
"""

import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

from cli_test_support import BINARY, ROOT, RedAppServer, redact

FIXTURE_VENDOR = {"id": "e2e", "name": {"en": "E2E Fixtures", "zh-CN": "端到端测试"}, "enabled": True}
FIXTURE_APP = {
    "vendor": "e2e",
    "id": "guide",
    "provider": "info",
    "name": {"en": "E2E Guide", "zh-CN": "端到端指南"},
    "enabled": True,
}


def main(arguments):
    if not BINARY.is_file():
        print(f"{BINARY} is missing; run `make binary` or `make build` first", file=sys.stderr)
        return 1
    directory = Path(tempfile.mkdtemp(prefix="redapp-e2e-"))
    server = RedAppServer(directory)
    try:
        server.start()
        admin = server.admin()
        admin.request("/admin/api/vendors", FIXTURE_VENDOR, method="POST", expect=201)
        admin.request("/admin/api/apps", FIXTURE_APP, method="POST", expect=201)
        admin.logout()
        environment = dict(
            os.environ,
            REDAPP_E2E_URL=server.base_url,
            REDAPP_E2E_PASSWORD=server.initial_password(),
            REDAPP_E2E_INFO_APP=f"{FIXTURE_APP['vendor']}/{FIXTURE_APP['id']}",
        )
        result = subprocess.run(["npm", "run", "e2e", "--", *arguments], cwd=ROOT / "frontend", env=environment)
        if result.returncode != 0:
            print("--- server log ---", file=sys.stderr)
            print(redact(server.log_text()), file=sys.stderr)
        return result.returncode
    finally:
        try:
            server.stop(check=False)
        finally:
            server.close()
            shutil.rmtree(directory, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
