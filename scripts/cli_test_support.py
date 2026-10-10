"""Shared harness for integration tests that drive a real ``bin/redapp`` process.

The tests built on this module start the compiled server on a loopback port,
talk to it over HTTP like the browser does (cookie session, CSRF token, Origin
header) and stop it again. The harness guarantees cleanup of every process and
fixture server it starts, and prints the private server log (with the initial
administrator password redacted) when a test fails.

Port selection: ``redapp serve`` neither accepts port 0 usefully (it does not
report the bound port) nor inherits a socket, so a free port is probed and then
released. Another process may grab it in between; the harness detects the
resulting ``address already in use`` exit and retries with a new port.
"""

import email.message
import http.cookiejar
import http.server
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
BINARY = ROOT / "bin" / "redapp"

READY_TIMEOUT_SECONDS = 20.0
STOP_TIMEOUT_SECONDS = 20.0
REQUEST_TIMEOUT_SECONDS = 15.0
PORT_ATTEMPTS = 5

_PASSWORD_LINE = re.compile(r"Initial admin password: ([^;\s]+);")
_ADDRESS_IN_USE = re.compile(r"address already in use", re.IGNORECASE)


class ServerError(RuntimeError):
    """The server process could not be started, became unready or misbehaved."""


def clean_environment(**overrides):
    """Return the current environment without inherited REDAPP_* settings."""
    environment = {
        key: value for key, value in os.environ.items() if not key.startswith("REDAPP_")
    }
    environment.update(overrides)
    return environment


def run_cli(args, cwd, env=None, timeout=10):
    """Run one short-lived ``bin/redapp`` command and capture its output."""
    return subprocess.run(
        [str(BINARY), *args],
        cwd=cwd,
        env=clean_environment(**(env or {})),
        capture_output=True,
        text=True,
        timeout=timeout,
    )


def probe_free_port():
    """Return a loopback port that was free a moment ago."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


def redact(text):
    """Hide the generated administrator password in log output."""
    return _PASSWORD_LINE.sub("Initial admin password: <redacted>;", text)


def _direct_opener(*handlers):
    """Build a urllib opener that never uses proxies from the environment."""
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), *handlers)


class Response:
    """A fully read HTTP response."""

    def __init__(self, method, url, status, headers, body):
        self.method = method
        self.url = url
        self.status = status
        self.headers = headers
        self.body = body

    @property
    def is_json(self):
        """Whether the response declares a JSON body."""
        return self.headers.get("Content-Type", "").startswith("application/json")

    def json(self):
        """Decode the body as JSON."""
        return json.loads(self.body)

    def text(self):
        """Decode the body as UTF-8 text."""
        return self.body.decode()

    def value(self):
        """Return decoded JSON for JSON responses and raw bytes otherwise."""
        return self.json() if self.is_json else self.body

    def describe(self, limit=500):
        """Summarize the response for an assertion message."""
        if self.is_json:
            body = self.text()
        elif self.headers.get("Content-Type", "").startswith("text/"):
            body = self.text()
        else:
            body = f"<{len(self.body)} bytes {self.headers.get('Content-Type', '')}>"
        if len(body) > limit:
            body = body[:limit] + "..."
        return f"{self.method} {self.url} -> {self.status}: {body}"


class Session:
    """A browser-like client: cookie jar, Origin header and CSRF token.

    The session reads ``server.base_url`` on every request, so it keeps working
    after the server was restarted on a different port. Cookies are kept, but
    the server forgets sessions on restart, so call :meth:`login` again.
    """

    def __init__(self, server):
        self.server = server
        self.csrf = ""
        self.opener = _direct_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
        )

    def fetch(
        self,
        path,
        body=None,
        *,
        method=None,
        raw=None,
        content_type=None,
        headers=None,
        if_match=None,
        expect=200,
    ):
        """Send one request and assert the exact response status.

        ``body`` is JSON-encoded; ``raw`` sends bytes as-is with ``content_type``.
        ``headers`` entries override the defaults, and a ``None`` value removes a
        default header (for example ``{"Origin": None}``). ``expect=None``
        disables the status assertion.
        """
        base = self.server.base_url
        data = raw
        if body is not None:
            data = json.dumps(body).encode()
            content_type = content_type or "application/json"
        merged = {"Origin": base}
        if self.csrf:
            merged["X-CSRF-Token"] = self.csrf
        if content_type:
            merged["Content-Type"] = content_type
        if if_match is not None:
            merged["If-Match"] = f'"{if_match}"'
        merged.update(headers or {})
        merged = {key: value for key, value in merged.items() if value is not None}
        request = urllib.request.Request(base + path, data=data, headers=merged, method=method)
        try:
            raw_response = self.opener.open(request, timeout=REQUEST_TIMEOUT_SECONDS)
        except urllib.error.HTTPError as error:
            raw_response = error
        with raw_response:
            response = Response(
                request.get_method(),
                path,
                raw_response.status,
                raw_response.headers if raw_response.headers is not None else email.message.Message(),
                raw_response.read(),
            )
        if expect is not None and response.status != expect:
            raise AssertionError(f"expected HTTP {expect}, got {response.describe()}")
        return response

    def request(self, path, body=None, **options):
        """Like :meth:`fetch` but return decoded JSON (or raw bytes)."""
        return self.fetch(path, body, **options).value()

    def login(self, password=None):
        """Sign in as administrator and remember the CSRF token."""
        password = password if password is not None else self.server.initial_password()
        self.csrf = self.request("/admin/api/login", {"password": password}, method="POST")["csrf"]
        return self.csrf

    def logout(self):
        """Sign out; the CSRF token is kept so the request itself is accepted."""
        self.request("/admin/api/logout", {}, method="POST")


class RedAppServer:
    """One ``bin/redapp`` server process with a private working directory.

    ``configure(server, port)`` returns ``(arguments, environment)`` for a start
    on ``port``; the default writes ``config.json`` with ``data_dir`` and
    ``listen`` and runs ``serve --config``. Every start probes a new port, so
    ``base_url`` changes across restarts.
    """

    def __init__(self, directory, configure=None):
        self.directory = Path(directory)
        self.directory.mkdir(parents=True, exist_ok=True)
        self.data_dir = self.directory / "data"
        self.log_path = self.directory / "server.log"
        self.configure = configure or RedAppServer.json_config
        self.process = None
        self.port = None
        self.environment = None
        self._log_stream = None
        self._log_offset = 0
        self._cached_log = ""

    @staticmethod
    def json_config(server, port):
        """Default configuration: a JSON file with the data directory and listener."""
        config = server.directory / "config.json"
        config.write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "data_dir": str(server.data_dir),
                    "listen": f"127.0.0.1:{port}",
                }
            )
        )
        return ["serve", "--config", str(config)], {}

    @property
    def base_url(self):
        """Loopback URL of the current listener."""
        if self.port is None:
            raise ServerError("server has not been started")
        return f"http://127.0.0.1:{self.port}"

    @property
    def running(self):
        """Whether the process is alive."""
        return self.process is not None and self.process.poll() is None

    def start(self):
        """Start the server and wait until ``/health/ready`` answers 200."""
        if self.running:
            raise ServerError("server is already running")
        for attempt in range(1, PORT_ATTEMPTS + 1):
            port = probe_free_port()
            arguments, environment = self.configure(self, port)
            self.environment = clean_environment(**environment)
            self._log_offset = self.log_path.stat().st_size if self.log_path.exists() else 0
            self._log_stream = self.log_path.open("ab")
            self.port = port
            self.process = subprocess.Popen(
                [str(BINARY), *arguments],
                cwd=self.directory,
                env=self.environment,
                stdin=subprocess.DEVNULL,
                stdout=self._log_stream,
                stderr=subprocess.STDOUT,
            )
            if self._wait_ready():
                return self
            self._close_log()
            if attempt < PORT_ATTEMPTS and _ADDRESS_IN_USE.search(self.current_log()):
                continue
            raise ServerError(
                f"server exited with {self.process.returncode} before readiness "
                f"(attempt {attempt}); log:\n{redact(self.current_log())}"
            )
        raise AssertionError("unreachable")

    def _wait_ready(self):
        """Poll readiness; return False if the process exited first."""
        opener = _direct_opener()
        deadline = time.monotonic() + READY_TIMEOUT_SECONDS
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                return False
            try:
                with opener.open(self.base_url + "/health/ready", timeout=1) as response:
                    if response.status == 200 and self.process.poll() is None:
                        return True
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.05)
        self.kill()
        raise ServerError(
            f"server was not ready within {READY_TIMEOUT_SECONDS:.0f}s; log:\n"
            f"{redact(self.current_log())}"
        )

    def stop(self, check=True):
        """Terminate gracefully; with ``check`` require exit status 0."""
        if self.process is None:
            return None
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=STOP_TIMEOUT_SECONDS)
            except subprocess.TimeoutExpired:
                self.kill()
                raise ServerError("server did not stop within the shutdown timeout")
        self._close_log()
        code = self.process.returncode
        if check and code != 0:
            raise ServerError(f"server exited with {code}; log:\n{redact(self.current_log())}")
        return code

    def kill(self):
        """Kill the process unconditionally and reap it."""
        if self.process is not None and self.process.poll() is None:
            self.process.kill()
            self.process.wait()
        self._close_log()

    def restart(self):
        """Stop cleanly and start again on a new port."""
        self.stop()
        return self.start()

    def close(self):
        """Release everything and keep the log text in memory for failure reports."""
        try:
            self.kill()
        finally:
            self._cached_log = self.log_text()

    def _close_log(self):
        """Close the log stream handed to the process."""
        if self._log_stream is not None:
            self._log_stream.close()
            self._log_stream = None

    def log_text(self):
        """Complete server output across all starts."""
        if self.log_path.exists():
            return self.log_path.read_text(errors="replace")
        return self._cached_log

    def current_log(self):
        """Server output since the most recent start."""
        if self._log_stream is not None:
            self._log_stream.flush()
        return self.log_text()[self._log_offset:] if self.log_path.exists() else ""

    def initial_password(self):
        """The administrator password generated when the data directory was created."""
        match = _PASSWORD_LINE.search(self.log_text())
        if match is None:
            raise ServerError("no initial admin password in the server log")
        return match.group(1)

    def session(self):
        """A new anonymous browser-like session."""
        return Session(self)

    def admin(self):
        """A new session signed in with the initial administrator password."""
        session = self.session()
        session.login()
        return session

    def state_database(self):
        """Path to the SQLite state database."""
        return self.data_dir / "state.sqlite"


class FixtureUpstream:
    """A loopback HTTP server with a test-provided request handler class."""

    def __init__(self, handler):
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    @property
    def url(self):
        """Base URL of the fixture."""
        return f"http://127.0.0.1:{self.server.server_port}"

    def close(self):
        """Stop serving and release the socket."""
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)


class QuietHandler(http.server.BaseHTTPRequestHandler):
    """Request handler base that does not write access logs to stderr."""

    def log_message(self, *args):
        """Discard access log lines."""

    def send_body(self, body, status=200, headers=None):
        """Send a complete response with Content-Length."""
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        for name, value in (headers or {}).items():
            self.send_header(name, value)
        self.end_headers()
        self.wfile.write(body)


class ServerTestCase(unittest.TestCase):
    """Test case with a private temporary directory and managed servers."""

    def setUp(self):
        super().setUp()
        if not BINARY.is_file():
            self.fail(f"{BINARY} is missing; run `make binary` first")
        self.directory = Path(tempfile.mkdtemp(prefix=f"redapp-{type(self).__name__}-"))
        self.addCleanup(shutil.rmtree, self.directory, ignore_errors=True)
        self.servers = {}

    def start_server(self, name="server", configure=None):
        """Create, register for cleanup and start a server in ``<tmp>/<name>``."""
        server = RedAppServer(self.directory / name, configure)
        self.servers[name] = server
        self.addCleanup(server.close)
        return server.start()

    def start_fixture(self, handler):
        """Start a fixture upstream that is shut down after the test."""
        fixture = FixtureUpstream(handler)
        self.addCleanup(fixture.close)
        return fixture

    def server_logs(self):
        """Redacted logs of every server this test started."""
        return {name: redact(server.log_text()) for name, server in self.servers.items()}


class _LogReportingResult(unittest.TextTestResult):
    """Prints the redacted server logs of a test when it fails or errors."""

    def _report_logs(self, test):
        """Write server logs collected by a ServerTestCase."""
        logs = getattr(test, "server_logs", None)
        if logs is None:
            return
        for name, text in logs().items():
            self.stream.writeln(f"----- {test.id()} [{name}] server log -----")
            self.stream.writeln(text.rstrip() or "<empty>")
            self.stream.writeln(f"----- end of {name} log -----")

    def addFailure(self, test, err):
        """Record a failure and print the logs."""
        super().addFailure(test, err)
        self._report_logs(test)

    def addError(self, test, err):
        """Record an error and print the logs."""
        super().addError(test, err)
        self._report_logs(test)

    def addSubTest(self, test, subtest, err):
        """Record a subtest outcome and print the logs when it failed."""
        super().addSubTest(test, subtest, err)
        if err is not None:
            self._report_logs(test)


def main():
    """Run the calling module's tests; failures include server logs."""
    runner = unittest.TextTestRunner(
        stream=sys.stderr, verbosity=2, resultclass=_LogReportingResult
    )
    unittest.main(module="__main__", testRunner=runner)
