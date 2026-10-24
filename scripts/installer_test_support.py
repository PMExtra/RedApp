"""Harmless artifacts and loopback transport shared by both installer protocols/hosts."""
import hashlib
import http.server
import io
import json
from pathlib import Path
import tarfile
import threading


def require(condition, detail=None):
    """Fail a contract unless condition holds; unlike assert, python -O cannot disable it."""
    if not condition:
        raise AssertionError(*([] if detail is None else [detail]))


def sha(data):
    """Return the SHA-256 hex digest of data."""
    return hashlib.sha256(data).hexdigest()


def codex_fixture(target, npm, legacy=False, version='0.159.2', body=None, failure=''):
    """Build harmless Codex release metadata and artifacts for one test case."""
    windows = 'windows' in target
    suffix = '.exe' if windows else ''
    binary = body if body is not None else f"#!/bin/sh\necho 'codex-cli {version}'\n".encode()
    helper = body if body is not None else b'#!/bin/sh\nexit 0\n'
    files = {
        f'bin/codex{suffix}': binary,
        f'bin/codex-code-mode-host{suffix}': helper,
        f'codex-path/rg{suffix}': helper,
        'codex-package.json': b'{}',
    }
    resources = (
        ['codex-command-runner.exe', 'codex-windows-sandbox-setup.exe'] if windows else ['bwrap']
    )
    files.update({'codex-resources/' + name: helper for name in resources})
    if legacy:
        prefix = f'package/vendor/{target}/'
        files = {prefix + f'codex/codex{suffix}': binary, prefix + f'path/rg{suffix}': helper}
        files.update({
            prefix + ('codex/' if windows else 'codex-resources/') + name: helper
            for name in resources
        })
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w:gz') as tar:
        for name, data in files.items():
            entry = tarfile.TarInfo(name)
            entry.size, entry.mode = len(data), 0o755
            tar.addfile(entry, io.BytesIO(data))
    archive = stream.getvalue()
    name = f'codex-npm-{npm}-{version}.tgz' if legacy else f'codex-package-{target}.tar.gz'
    manifest = (
        f'{sha(archive)}  {name if failure != "manifest-entry" else "unrelated.tar.gz"}\n'
    ).encode()
    metadata = {
        'tag_name': 'rust-v' + ('0.159.3' if failure == 'tag' else version),
        'assets': [
            {
                'name': name,
                'digest': 'sha256:' + sha(archive),
                'browser_download_url': 'https://attacker.example/injected',
            }
        ],
    }
    if not legacy:
        metadata['assets'].append({
            'name': 'codex-package_SHA256SUMS',
            'digest': 'sha256:' + sha(manifest),
            'browser_download_url': 'https://attacker.example/manifest',
        })
    files = {name: archive, 'codex-package_SHA256SUMS': manifest}
    if failure == 'missing-digest':
        metadata['assets'][0]['digest'] = 'invalid'
    if failure == 'missing-package':
        metadata['assets'] = metadata['assets'][1:]
    if failure == 'manifest-hash':
        files['codex-package_SHA256SUMS'] += b'corruption'
    if failure == 'archive-hash':
        files[name] += b'corruption'
    if failure == 'truncated':
        files[name] = archive[:len(archive) // 2]
    return dict(metadata=metadata, files=files, failure=failure, version=version, asset=name)


class InstallerServer:
    """Real HTTP client behavior; redirects stay local so a regression cannot escape."""

    def __init__(self, application, provider):
        """Create a loopback server that serves fixtures under the application prefix."""
        self.config, self.seen = {}, []
        fixture = self
        prefix = '/' + application

        class Handler(http.server.BaseHTTPRequestHandler):
            """Serve configured fixture responses and record requested paths."""

            def log_message(self, *_):
                """Silence request logging."""
                pass

            def do_GET(self):
                """Serve one fixture response, optionally via a local redirect."""
                if not self.path.startswith(prefix + '/'):
                    fixture.seen.append(self.path)
                    self.send_error(404)
                    return
                path = self.path[len(prefix):]
                fixture.seen.append(path)
                if fixture.config.get('redirect'):
                    if not path.startswith('/redirected/'):
                        self.send_response(302)
                        self.send_header('Location', prefix + '/redirected' + path)
                        self.end_headers()
                        return
                    path = path[len('/redirected'):]
                status, body, headers = fixture.response(provider, path)
                self.send_response(status)
                content_type = (
                    'application/json'
                    if path.endswith('.json') or (provider == 'codex' and path.endswith('latest'))
                    else 'text/plain'
                )
                self.send_header('Content-Type', content_type)
                for key, value in headers.items():
                    self.send_header(key, value)
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.base = f'http://127.0.0.1:{self.server.server_port}' + prefix

    def __enter__(self):
        """Start serving in a background thread."""
        self.thread.start()
        return self

    def __exit__(self, *_):
        """Stop the server and join its thread."""
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def response(self, provider, path):
        """Return (status, body, headers) for a request path under the current config."""
        config = self.config
        failure, version = config.get('failure', ''), config['version']
        if provider == 'codex':
            stage = 'metadata' if path.endswith(('latest', 'release.json')) else (
                'manifest' if path.endswith('SHA256SUMS') else 'artifact')
            body = (
                (b'not json' if failure == 'metadata' else json.dumps(config['metadata']).encode())
                if stage == 'metadata'
                else config['files'].get(path.rsplit('/', 1)[-1])
            )
        else:
            binary = config.get('binary', 'claude')
            artifact = f'/{version}/{config["platform"]}/{binary}'
            stage = {
                '/latest': 'channel',
                '/stable': 'channel',
                f'/{version}/manifest.json': 'manifest',
                artifact: 'artifact',
            }.get(path)
            if stage == 'channel':
                body = b'2.1.285/../../escape' if failure == 'channel' else version.encode()
            elif stage == 'manifest':
                body = json.dumps({
                    'version': version,
                    'platforms': {
                        config['platform']: {
                            'binary': binary,
                            'checksum': sha(config['body']),
                            'size': len(config['body']),
                        }
                    },
                }).encode()
                if failure == 'manifest':
                    body = b'<html>Error</html>'
            elif stage == 'artifact':
                if failure == 'download':
                    return 503, b'', {}
                body = config['body'] + (b'tampered' if failure == 'hash' else b'')
            else:
                body = None
        return (404, b'', {}) if body is None else (200, body, {})


def file_state(directory):
    """Snapshot every file under directory as a relative-path to bytes mapping."""
    return {
        str(p.relative_to(directory)): p.read_bytes()
        for p in directory.rglob('*')
        if p.is_file()
    }


CODEX_DAEMON_CASES = (
    'absent', 'existing', 'legacy', 'matching', 'stopped', 'downgrade',
    'failed', 'unsupported', 'malformed', 'external', 'daemon-only', 'deferred', 'background',
    'latest-newer', 'latest-matching', 'latest-stopped',
)


def prepare_codex_daemon_case(home, env, scenario):
    """Create only inert daemon state; lifecycle commands run in fixture binaries."""
    daemon_root = home / 'codex-home/packages/app-server-daemon'
    state = home / 'codex-home/app-server-daemon'
    if scenario in ('legacy', 'daemon-only', 'deferred'):
        state.mkdir(parents=True)
        name = 'app-server.stderr.log' if scenario == 'legacy' else 'daemon.stderr.log'
        (state / name).write_text('previous daemon launch')
    elif scenario != 'absent':
        (daemon_root / 'current').mkdir(parents=True)
    version = '0.159.2' if scenario in ('matching', 'malformed', 'external', 'latest-matching') else (
        '0.159.3' if scenario in ('downgrade', 'latest-newer') else '0.159.1'
    )
    status = {'backend': 'external' if scenario == 'external' else 'pid',
              'managedCodexVersion': version}
    if scenario not in ('stopped', 'latest-stopped'):
        status['appServerVersion'] = version
    if scenario not in ('absent', 'daemon-only', 'deferred', 'background'):
        daemon_root.mkdir(parents=True, exist_ok=True)
        (daemon_root / 'auto-update-version').write_text('eligible')
    env.update(
        REDAPP_DAEMON_LOG=str(home / 'daemon-calls.txt'),
        REDAPP_DAEMON_STATUS=json.dumps(status)[:-1] if scenario == 'malformed' else json.dumps(status),
        REDAPP_DAEMON_RESULT='42' if scenario in ('failed', 'unsupported') else '0',
        REDAPP_DAEMON_PROBE_RESULT='2' if scenario == 'unsupported' else '0',
    )
    if scenario in ('daemon-only', 'deferred'):
        env['CODEX_INSTALL_DAEMON_ONLY'] = '1'
    if scenario == 'deferred':
        env['CODEX_INSTALL_DEFER_SELECTION'] = '1'
    if scenario == 'background':
        env['CODEX_INSTALL_IF_LATEST'] = '1'


def check_codex_daemon_case(home, result, scenario, executable):
    """Verify exact lifecycle argv, the installed executable, and partial completion."""
    log = home / 'daemon-calls.txt'
    calls = log.read_text().splitlines() if log.exists() else []
    skipped = scenario in ('absent', 'daemon-only', 'deferred', 'background')
    update = not skipped and scenario not in ('matching', 'latest-matching', 'latest-newer')
    expected = [] if skipped else [str(executable), 'app-server daemon version']
    if update:
        expected += [str(executable), 'app-server daemon update --from-cli --yes']
    detail = (scenario, calls, expected, result.stdout, result.stderr)
    require(len(calls) == len(expected) and calls[1::2] == expected[1::2], detail)
    for command in calls[::2]:
        # Windows may spell the same file using an 8.3 or long directory name.
        require(Path(command).is_absolute() and Path(command).samefile(executable), detail)
    partial = scenario in ('failed', 'unsupported')
    require((result.returncode != 0) == partial, (scenario, result.stdout, result.stderr))
    output = result.stdout + result.stderr
    if partial:
        require('installation partially completed' in output, output)
        require('installed successfully' not in output, output)
    if update:
        require('active sessions may be interrupted' in output, output)
    if not skipped:
        require(not (home / 'codex-home/packages/app-server-daemon/auto-update-version').exists())
        require(executable.exists(), 'CLI must remain installed after daemon sync')


CODEX_CONFIG_CASES = {
    'missing': (None, b'check_for_update_on_startup = false\n'),
    'table': (b'[profiles.work]\ncheck_for_update_on_startup = true',
              b'check_for_update_on_startup = false\n[profiles.work]\ncheck_for_update_on_startup = true'),
    'root': (b'"check_for_update_on_startup" = true # keep\r\n[profile]\r\nmodel="x"',
             b'"check_for_update_on_startup" = false # keep\r\n[profile]\r\nmodel="x"'),
    'false': (b'check_for_update_on_startup=false # = true',
              b'check_for_update_on_startup=false # = true'),
    'multiline': (b'note = """\n[fake]\ncheck_for_update_on_startup = true\n"""\ncheck_for_update_on_startup=true\n',
                  b'note = """\n[fake]\ncheck_for_update_on_startup = true\n"""\ncheck_for_update_on_startup=false\n'),
    'escaped': (b'"\\u0063heck_for_update_on_startup"=true\n', None),
    'dotted': (b'check_for_update_on_startup.child=true\n', None),
}


def prepare_codex_config_case(home, scenario):
    """Seed user TOML including table and multiline-string lookalikes."""
    path = home / 'codex-home/config.toml'
    path.parent.mkdir(parents=True, exist_ok=True)
    before, _ = CODEX_CONFIG_CASES[scenario]
    if before is not None:
        path.write_bytes(before)


def check_codex_config_case(home, result, scenario):
    """Require exact preservation outside the root setting, including final newline."""
    before, after = CODEX_CONFIG_CASES[scenario]
    expected = before if after is None else after
    require((home / 'codex-home/config.toml').read_bytes() == expected, (scenario, result.stdout, result.stderr))
    require((result.returncode == 0) == (after is not None), (scenario, result.stdout, result.stderr))
    if after is None:
        require('installation partially completed' in result.stdout + result.stderr)
