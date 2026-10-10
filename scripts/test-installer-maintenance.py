#!/usr/bin/env python3
"""Deterministic maintenance failures and local fast-forward draft publication; no upstream calls."""
import copy
import contextlib
import io
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import http.server
import ssl
import threading
import socket
import unittest
from unittest.mock import patch
import zipfile
import installer_maintenance as m

spec = importlib.util.spec_from_file_location(
    'publisher', Path(__file__).with_name('publish-installer-update.py')
)
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)


class CheckTests(unittest.TestCase):
    """Upstream check, redirect, patch and packaging failure behavior."""

    def original(self, url):
        """Return the committed upstream bytes for an inventory URL."""
        item = next(x for x in m.inventory() if x['url'] == url)
        return (m.ROOT / 'installers' / item['application'] / 'upstream' / item['name']).read_bytes()

    def test_unchanged_and_one_changed(self):
        """Identical, changed and re-signed upstream scripts get the right status."""
        rows = m.inspect(fetch=self.original)
        self.assertEqual([r['status'] for r in rows], ['unchanged'] * len(m.inventory()))

        def changed(url):
            """Append a comment to the Claude Code install.sh only."""
            return self.original(url) + (
                b'\n# new official comment\n'
                if url.endswith('/install.sh') and 'claude.ai' in url
                else b''
            )

        rows = m.inspect(fetch=changed)
        self.assertEqual(sum(r['status'] == 'changed' for r in rows), 1)

        def resigned(url):
            """Replace the signature block of PowerShell scripts."""
            body = m.unsigned(self.original(url))
            return (
                body
                + b'\r\n# SIG # Begin signature block\r\n# UkVTSUdORUQ=\r\n# SIG # End signature block\r\n'
                if url.endswith('.ps1')
                else body
            )

        rows = m.inspect(fetch=resigned)
        self.assertEqual([r['status'] for r in rows], ['unchanged'] * len(m.inventory()))
        self.assertIn('unchanged (signature only)', m.report(rows, '0' * 40))

    def test_manifest_extends_inventory_and_allowlist_without_protocol_code(self):
        """New manifest applications extend inventory but cannot name new validators."""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            target = root / '.generated/installer-inventory.json'
            target.parent.mkdir(parents=True)
            manifest = json.loads((m.ROOT / '.generated/installer-inventory.json').read_text())
            extra = copy.deepcopy(manifest['applications'][0])
            extra['id'] = 'example/third-app'
            manifest['applications'].append(extra)
            target.write_text(json.dumps(manifest))
            self.assertEqual(len(m.inventory(root)), len(m.inventory()) + len(extra['installers']))
            allowed = p.allowed_paths(root)
            self.assertIn('installers/example/third-app/generated/install.sh', allowed)
            for protected in (
                'patches/install.sh.patch', 'upstream/LICENSE', 'upstream/claude-code.asc'
            ):
                self.assertNotIn('installers/example/third-app/' + protected, allowed)
            extra['installer_validator'] = 'arbitrary-shell-command'
            target.write_text(json.dumps(manifest))
            with self.assertRaisesRegex(ValueError, 'reviewed implementation'):
                m.inventory(root)

    def test_all_errors_are_aggregated(self):
        """Every installer is checked even when earlier fetches fail."""
        calls = []

        def fail(url):
            """Fail each fetch in a different way."""
            calls.append(url)
            if len(calls) == 1:
                raise OSError('fixture network failure')
            if len(calls) == 2:
                return b'<html>temporary server failure</html>'
            if len(calls) == 3:
                raise ValueError('Unreviewed upstream redirect')
            return b''

        rows = m.inspect(fetch=fail)
        self.assertEqual(len(calls), len(m.inventory()))
        self.assertTrue(all(r['status'] == 'error' for r in rows))
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                m.prepare(Path(tmp), fetch=fail)
            self.assertFalse((Path(tmp) / 'sources').exists())

    def test_baseline_integrity_failure_is_not_a_no_change_result(self):
        """A locally modified baseline is an error, not an unchanged result."""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            shutil.copytree(m.ROOT / 'installers', root / 'installers')
            target = root / '.generated'
            target.mkdir(parents=True)
            shutil.copyfile(
                m.ROOT / '.generated/installer-inventory.json', target / 'installer-inventory.json'
            )
            (root / 'installers/openai/codex/upstream/install.sh').write_bytes(
                b'#!/bin/sh\nmodified locally\n'
            )
            rows = m.inspect(root=root, fetch=self.original)
            modified = next(
                r for r in rows if r['application'] == 'openai/codex' and r['name'] == 'install.sh'
            )
            self.assertEqual(modified['status'], 'error')
            self.assertIn('audited digest', modified['error'])
            self.assertEqual(len(rows), len(m.inventory()))

    def test_redirects_and_shape_fail_closed(self):
        """HTTP redirects and malformed script responses are rejected."""
        with self.assertRaises(ValueError):
            m.HTTPSRedirect('https://official.example/install.sh').redirect_request(
                m.urllib.request.Request('https://official.example/install.sh'),
                None, 302, 'Found', {}, 'http://example.org/install.sh',
            )
        for name, raw in [
            ('install.sh', b'x' * (m.MAX_SCRIPT + 1)),
            ('install.sh', b'#! /bin/sh\0'),
            ('install.ps1', b'<html>failure</html>'),
        ]:
            with self.assertRaises(ValueError):
                m.script_shape(name, raw)

    def test_https_redirects_allow_cross_host_and_reject_unsafe_destinations(self):
        """Public HTTPS redirects are followed up to the limit; unsafe targets fail."""
        source = 'https://official.example/install.sh'
        public = [(socket.AF_INET, socket.SOCK_STREAM, 6, '', ('93.184.215.14', 443))]
        with patch.object(m.socket, 'getaddrinfo', return_value=public):
            handler = m.HTTPSRedirect(source)
            request = m.urllib.request.Request(source)
            for i in range(m.MAX_REDIRECTS):
                target = f'https://cdn{i}.example/install.sh'
                request = handler.redirect_request(request, None, 302, 'Found', {}, target)
                self.assertEqual(request.full_url, target)
            with self.assertRaisesRegex(ValueError, 'limit or loop'):
                handler.redirect_request(
                    request, None, 302, 'Found', {}, 'https://another.example/file'
                )
            with self.assertRaises(ValueError):
                m.HTTPSRedirect(source).redirect_request(request, None, 302, 'Found', {}, source)
            for target in (
                'http://cdn.example/file',
                'file:///tmp/file',
                'ftp://cdn.example/file',
                'https://user:password@cdn.example/file',
                'https://cdn.example/file#fragment',
            ):
                with self.assertRaises(ValueError):
                    m.validate_https_url(target)
        for address in (
            '127.0.0.1', '169.254.169.254', '10.0.0.1', '192.168.1.1',
            '100.64.0.1', '::1', 'fe80::1', 'fc00::1',
        ):
            with patch.object(
                m.socket, 'getaddrinfo',
                return_value=[(socket.AF_INET, socket.SOCK_STREAM, 6, '', (address, 443))],
            ), self.assertRaisesRegex(ValueError, 'non-public'):
                m.validate_https_url('https://unexpected.example/install.sh')

    def test_real_tls_redirect_chain_and_bad_certificate(self):
        """Real TLS downloads follow redirects and reject bad certificates and peers."""
        # Test-only loopback allowance keeps this deterministic without weakening production address checks.
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            cert = temp / 'cert.pem'
            key = temp / 'key.pem'
            subprocess.run(
                [
                    'openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                    '-keyout', str(key), '-out', str(cert), '-days', '1',
                    '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1',
                ],
                check=True, capture_output=True,
            )

            class Handler(http.server.BaseHTTPRequestHandler):
                """Serve a redirect chain and a small script over TLS."""

                def log_message(self, *_):
                    """Silence request logging."""
                    pass

                def do_GET(self):
                    """Redirect known paths or return the fixture script."""
                    redirects = {
                        '/start': '/middle',
                        '/middle': '/script',
                        '/loop': '/loop',
                        '/downgrade': 'http://127.0.0.1/no-tls',
                    }
                    if self.path in redirects:
                        self.send_response(302)
                        self.send_header('Location', redirects[self.path])
                        self.end_headers()
                        return
                    body = b'#!/bin/sh\necho fixture\n'
                    self.send_response(200)
                    self.send_header('Content-Length', str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)

            server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
            server_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            server_context.load_cert_chain(cert, key)
            server.socket = server_context.wrap_socket(server.socket, server_side=True)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            base = f'https://127.0.0.1:{server.server_port}'
            trusted = ssl.create_default_context(cafile=str(cert))

            def allow_fixture(url):
                """Allow only URLs on the loopback fixture server."""
                if not url.startswith(base + '/'):
                    raise ValueError('Fixture rejected non-HTTPS/non-loopback destination')

            try:
                with patch.object(m, 'validate_https_url', side_effect=allow_fixture):
                    # A DNS allow decision cannot authorize the actual private peer.
                    with self.assertRaisesRegex(ValueError, 'non-public'):
                        m.download(base + '/start')
                with patch.object(
                    m, 'validate_https_url', side_effect=allow_fixture
                ), patch.object(m, 'validate_address'):
                    with self.assertRaises(m.urllib.error.URLError) as caught:
                        m.download(base + '/start')
                    self.assertIsInstance(caught.exception.reason, ssl.SSLCertVerificationError)
                    with patch.object(m.ssl, 'create_default_context', return_value=trusted):
                        self.assertEqual(m.download(base + '/start'), b'#!/bin/sh\necho fixture\n')
                        for path in ('/loop', '/downgrade'):
                            with self.assertRaises(ValueError):
                                m.download(base + path)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()

    def test_patch_generation_offset_and_conflict(self):
        """Patches apply with line offsets and fail on conflicting context."""
        for item in m.inventory():
            app, name = item['application'], item['name']
            root = m.ROOT / 'installers' / app
            diff = root / 'patches' / (name + '.patch')
            raw = (root / 'upstream' / name).read_bytes()
            generated = (root / 'generated' / name).read_bytes()
            self.assertEqual(m.apply_patch(raw, diff), generated)
            newline = b'\r\n' if b'\r\n' in raw else b'\n'
            # Shift every hunk after the first; GNU patch anchors hunks that touch the file start.
            start = int(re.findall(rb'^@@ -(\d+)', diff.read_bytes(), re.M)[1]) - 1
            lines = raw.splitlines(keepends=True)
            probe = b'# redapp offset probe' + newline
            shifted = b''.join(lines[:start] + [probe] + lines[start:])
            self.assertEqual(m.apply_patch(shifted, diff).replace(probe, b'', 1), generated)
            removed = next(
                l[1:] for l in diff.read_bytes().splitlines(keepends=True)
                if l.startswith(b'-') and not l.startswith(b'---') and raw.count(l[1:]) == 1
            )
            with self.assertRaises((ValueError, subprocess.SubprocessError)):
                m.apply_patch(raw.replace(removed, b'# redapp conflict' + newline, 1), diff)

    def test_trailing_authenticode_block_is_removed_before_patch(self):
        """A trailing signature block is stripped before patching; others stop."""
        root = m.ROOT / 'installers/openai/codex'
        diff = root / 'patches/install.ps1.patch'
        raw = (root / 'upstream/install.ps1').read_bytes()
        body = m.unsigned(raw)
        self.assertNotIn(b'# SIG #', body)
        self.assertEqual(m.unsigned(body), body)
        generated = (root / 'generated/install.ps1').read_bytes()
        # Re-signing may change the block content and its trailing newline.
        for block in (b'# QUJD\r\n# REVG+/=\r\n', b'# any future layout\r\n'):
            for end in (b'\r\n', b''):
                resigned = (
                    body + b'\r\n# SIG # Begin signature block\r\n' + block
                    + b'# SIG # End signature block' + end
                )
                self.assertEqual(m.apply_patch(resigned, diff), generated)
        # A block that is not trailing is kept and stops for review.
        with self.assertRaises(ValueError):
            m.apply_patch(resigned + b'\r\nWrite-Host injected\r\n', diff)
        shell = (m.ROOT / 'installers/openai/codex/upstream/install.sh').read_bytes()
        self.assertEqual(m.unsigned(shell), shell)

    def test_only_one_trailing_authenticode_block_is_stripped(self):
        """Code hidden around signature blocks is never silently dropped."""
        for nl in (b'\r\n', b'\n'):
            code = b'Write-Host one' + nl + b'Write-Host two' + nl

            def block(body=b'# QUJD'):
                """Build a signature block with the current newline style."""
                return nl.join([b'', m.SIGNATURE_BEGIN, body, m.SIGNATURE_END]) + nl

            self.assertEqual(m.unsigned(code), code)
            self.assertEqual(m.unsigned(code.rstrip() + block()), code.rstrip())
            # Code between two blocks or a marker earlier in code must never be silently dropped.
            for tampered in (
                code.rstrip() + block() + b'Write-Host hidden' + block(),
                b'$m="' + m.SIGNATURE_BEGIN + b'"' + nl + code.rstrip() + block(),
                code.rstrip() + block(b'# a' + nl + m.SIGNATURE_END + nl + b'Write-Host hidden'),
                code.rstrip() + block() + b'Write-Host hidden' + nl,
            ):
                with self.assertRaisesRegex(ValueError, 'maintainer review'):
                    m.unsigned(tampered)

        def appended(url):
            """Append hidden code and a new block to signed scripts."""
            body = self.original(url)
            return (
                body + b'\r\nWrite-Host hidden\r\n' + m.SIGNATURE_BEGIN + b'\r\n# QUJD\r\n'
                + m.SIGNATURE_END + b'\r\n'
                if m.SIGNATURE_BEGIN in body
                else body
            )

        rows = m.inspect(fetch=appended)
        self.assertEqual(
            [
                r['status'] for r in rows
                if r['application'] == 'openai/codex' and r['name'] == 'install.ps1'
            ],
            ['error'],
        )

    def test_isolated_test_failure_does_not_output(self):
        """A failing isolated test produces no validated output."""
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            prepared = tmp / 'prepared'
            m.prepare(prepared, fetch=self.original)
            original_run = m.run

            def failing(args, **kwargs):
                """Fail the Python test harness invocation only."""
                if args[0] == 'python3':
                    raise subprocess.CalledProcessError(1, args)
                return original_run(args, **kwargs)

            with patch.object(
                m, 'run', side_effect=failing
            ), self.assertRaises(subprocess.CalledProcessError):
                m.validate(prepared, tmp / 'out')
            self.assertFalse(list((tmp / 'out').glob('*')))

    def test_readonly_package_rejects_changed_validation_output(self):
        """Packaging rejects validated output that differs from source plus patch."""
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            prepared = tmp / 'prepared'
            m.prepare(prepared, fetch=self.original)
            for descriptor in m.applications():
                app = descriptor['id']
                shutil.copytree(m.ROOT / 'installers' / app / 'generated', tmp / 'validated' / app)
            (tmp / 'validated/openai/codex/install.sh').write_text('tampered output')
            with self.assertRaises(ValueError):
                m.package(prepared, tmp / 'validated', tmp / 'bundle.zip')
            self.assertFalse((tmp / 'bundle.zip').exists())


class FakeAPI:
    """In-memory stand-in for the GitHub pull request API."""

    repository = 'PMExtra/RedApp'

    def __init__(self):
        """Start with no pull requests."""
        self.prs = {}
        self.created = 0
        self.comments = {}
        self.fail_create = False

    def open_prs(self):
        """Return copies of the open pull requests."""
        return [copy.deepcopy(pr) for pr in self.prs.values() if pr['state'] == 'open']

    def create(self, app, branch, body):
        """Record a new managed draft pull request."""
        if self.fail_create:
            raise ValueError('GitHub API POST failed with HTTP 422; no settings were changed')
        self.created += 1
        number = self.created
        self.prs[number] = {
            'number': number,
            'state': 'open',
            'draft': True,
            'user': {'login': p.BOT},
            'head': {
                'ref': branch,
                'sha': p.MARKER.search(body)[1],
                'repo': {'full_name': self.repository},
            },
            'base': {'ref': 'main'},
            'body': body,
            'html_url': f'https://example.invalid/draft/{number}',
        }
        return self.prs[number]

    def close(self, number, comment):
        """Record a comment and close the pull request."""
        self.comments[number] = comment
        self.prs[number]['state'] = 'closed'
        return self.prs[number]

    def open_for(self, app):
        """Return open pull requests for one application's update branches."""
        return [pr for pr in self.open_prs() if pr['head']['ref'].startswith(p.PREFIX + app + '/')]


class PublishTests(unittest.TestCase):
    """Draft publication against a local bare remote and a fake API."""

    def setUp(self):
        """Create a local repository, bare remote and one changed Claude installer."""
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'repo'
        self.root.mkdir()
        self.remote = Path(self.temp.name) / 'remote.git'
        p.git(self.root, 'init', '-b', 'main')
        p.git(self.root, 'config', 'user.name', 'Fixture')
        p.git(self.root, 'config', 'user.email', 'fixture@example.invalid')
        target = self.root / '.generated'
        target.mkdir(parents=True)
        shutil.copyfile(m.ROOT / '.generated/installer-inventory.json', target / 'installer-inventory.json')
        for name in sorted(p.allowed_paths(self.root)):
            file = self.root / name
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_text('{}\n' if name.endswith('.json') else 'old\n')
        p.git(self.root, 'add', '.')
        p.git(self.root, 'commit', '-m', 'baseline')
        p.git(self.root, 'init', '--bare', str(self.remote))
        p.git(self.root, 'remote', 'add', 'origin', str(self.remote))
        p.git(self.root, 'push', 'origin', 'main')
        self.baseline = p.git(self.root, 'rev-parse', 'HEAD').decode().strip()
        self.api = FakeAPI()
        self.payload = {'baseline': self.baseline, 'rows': []}
        for item in m.inventory():
            changed = item['application'] == 'anthropic/claude-code' and item['name'] == 'install.sh'
            self.payload['rows'].append({
                **item,
                'status': 'changed' if changed else 'unchanged',
                'baseline_sha256': m.digest(b'old\n'),
                'current_sha256': m.digest(b'new\n' if changed else b'old\n'),
            })
        self.files = {
            'installers/anthropic/claude-code/upstream/install.sh': b'new\n',
            'installers/anthropic/claude-code/generated/install.sh': b'patched\n',
            'installers/anthropic/claude-code/provenance.json':
                b'{"script_baseline":{"checked_at":"first"}}\n',
        }

    def publish(self):
        """Publish the current payload and files to the fixture remote."""
        return p.publish(
            self.root, self.payload, self.files, self.api, 'fixture-token',
            'https://example.invalid/run',
        )

    def bundle(self, extras=None):
        """Write the current files as a bundle and return its path and digest."""
        path = Path(self.temp.name) / 'bundle.zip'
        payload = {**self.payload, 'files': {name: m.digest(data) for name, data in self.files.items()}}
        with zipfile.ZipFile(path, 'w') as z:
            for name, data in self.files.items():
                z.writestr(name, data)
            z.writestr('update.json', json.dumps(payload))
            for name, data in (extras or {}).items():
                z.writestr(name, data)
        return path, m.digest(path.read_bytes())

    def test_bundle_allowlist_and_digests(self):
        """Bundles with wrong digests or unapproved paths are rejected."""
        path, sha = self.bundle()
        payload, files = p.load_bundle(path, sha, self.baseline)
        self.assertEqual(files, self.files)
        with self.assertRaises(ValueError):
            p.load_bundle(path, '0' * 64, self.baseline)
        path, sha = self.bundle({'../../script.py': b'no execution'})
        with self.assertRaises(ValueError):
            p.load_bundle(path, sha, self.baseline)
        self.files['installers/anthropic/claude-code/patches/install.sh.patch'] = b'unapproved'
        path, sha = self.bundle()
        with self.assertRaises(ValueError):
            p.load_bundle(path, sha, self.baseline)

    def test_exact_candidate_materialization_and_baseline_preservation(self):
        """Materialization overlays exactly the bundle and leaves the baseline intact."""
        from installer_bundle import materialize
        path, sha = self.bundle()
        before = {
            p.relative_to(self.root): p.read_bytes()
            for p in (self.root / 'installers').rglob('*')
            if p.is_file()
        }
        destination = Path(self.temp.name) / 'candidate'
        materialize(path, sha, self.baseline, destination, self.root)
        for name, data in self.files.items():
            self.assertEqual((destination / Path(name).relative_to('installers')).read_bytes(), data)
        for name, data in before.items():
            self.assertEqual((self.root / name).read_bytes(), data)
            if str(name) not in self.files:
                self.assertEqual((destination / name.relative_to('installers')).read_bytes(), data)
        with self.assertRaises(ValueError):
            materialize(path, sha, '0' * 40, Path(self.temp.name) / 'bad', self.root)
        self.assertFalse((Path(self.temp.name) / 'bad').exists())

    def remote_heads(self):
        """Return the fixture remote's refs as a ref-to-commit mapping."""
        return {
            line.split()[1]: line.split()[0]
            for line in p.git(self.root, 'ls-remote', 'origin').decode().splitlines()
        }

    def change(self, app, body):
        """Mark an application's install.sh as changed to body."""
        for row in self.payload['rows']:
            if row['application'] == app and row['name'] == 'install.sh':
                row.update(status='changed', current_sha256=m.digest(body))
        self.files.update({
            f'installers/{app}/upstream/install.sh': body,
            f'installers/{app}/generated/install.sh': b'patched ' + body,
            f'installers/{app}/provenance.json': b'{}\n',
        })

    def test_one_latest_draft_per_application(self):
        """Each application keeps exactly one latest draft; older ones are closed."""
        first = self.publish()[0]
        self.assertTrue(first['draft'])
        self.assertTrue(first['head']['ref'].startswith(p.PREFIX + 'anthropic/claude-code/'))
        self.assertEqual(
            p.git(self.root, 'rev-parse', first['head']['sha'] + '^').decode().strip(), self.baseline
        )
        # Only the provenance check time differs: same branch, no new PR.
        self.files['installers/anthropic/claude-code/provenance.json'] = (
            b'{"script_baseline":{"checked_at":"tomorrow"}}\n'
        )
        self.publish()
        self.assertEqual(self.api.created, 1)
        self.change('anthropic/claude-code', b'newer\n')
        second = self.publish()[0]
        self.assertEqual(
            [pr['number'] for pr in self.api.open_for('anthropic/claude-code')], [second['number']]
        )
        self.assertIn(f"#{second['number']}", self.api.comments[first['number']])
        self.assertNotIn('refs/heads/' + first['head']['ref'], self.remote_heads())
        # Codex is handled independently and does not close the Claude draft.
        self.change('openai/codex', b'codex new\n')
        self.publish()
        self.assertEqual(len(self.api.open_for('anthropic/claude-code')), 1)
        self.assertEqual(len(self.api.open_for('openai/codex')), 1)
        self.assertEqual(self.api.created, 3)
        self.assertEqual(self.remote_heads()['refs/heads/main'], self.baseline)

    def test_pushed_branch_without_pr_is_reused(self):
        """A branch pushed by a run that failed to open its PR is reused."""
        self.api.fail_create = True
        with self.assertRaises(ValueError):
            self.publish()
        pushed = {
            k: v for k, v in self.remote_heads().items() if k != 'refs/heads/main' and k != 'HEAD'
        }
        self.api.fail_create = False
        result = self.publish()[0]
        self.assertEqual({'refs/heads/' + result['head']['ref']: result['head']['sha']}, pushed)

    def test_manual_head_change_is_not_closed(self):
        """A draft whose branch was changed manually is left open and stops publication."""
        old = self.publish()[0]
        tree = p.git(self.root, 'rev-parse', old['head']['sha'] + '^{tree}').decode().strip()
        manual = p.git(
            self.root, 'commit-tree', tree, '-p', old['head']['sha'], input=b'manual change\n'
        ).decode().strip()
        p.git(self.root, 'push', 'origin', manual + ':refs/heads/' + old['head']['ref'])
        self.api.prs[old['number']]['head']['sha'] = manual
        self.change('anthropic/claude-code', b'newer\n')
        with self.assertRaisesRegex(ValueError, 'changed outside'):
            self.publish()
        self.assertEqual(self.api.created, 1)
        self.assertEqual(self.api.prs[old['number']]['state'], 'open')

    def test_unowned_nondraft_and_main_advance_stop(self):
        """Non-draft or foreign PRs and an advanced main stop publication."""
        old = self.publish()[0]
        self.change('anthropic/claude-code', b'newer\n')
        self.api.prs[old['number']]['draft'] = False
        with self.assertRaises(ValueError):
            self.publish()
        self.api.prs[old['number']].update(draft=True, user={'login': 'someone'})
        with self.assertRaises(ValueError):
            self.publish()
        self.assertEqual(self.api.created, 1)
        # A fork PR with the managed branch name is ignored, not closed.
        self.api.prs[old['number']].update(
            user={'login': p.BOT}, head={**old['head'], 'repo': {'full_name': 'someone/RedApp'}}
        )
        fresh = self.publish()[0]
        self.assertEqual(self.api.prs[old['number']]['state'], 'open')
        self.assertNotEqual(fresh['number'], old['number'])
        p.git(self.root, 'commit', '--allow-empty', '-m', 'main advanced')
        p.git(self.root, 'push', 'origin', 'main')
        with self.assertRaisesRegex(ValueError, 'main advanced'):
            self.publish()

    def test_branch_with_code_change_stops(self):
        """A managed branch containing non-installer changes stops publication."""
        old = self.publish()[0]
        head = old['head']['sha']
        p.git(self.root, 'checkout', '-b', 'hosted', head)
        (self.root / 'not-allowed.py').write_text('do not publish')
        p.git(self.root, 'add', '.')
        p.git(self.root, 'commit', '-m', 'extra code')
        manual = p.git(self.root, 'rev-parse', 'HEAD').decode().strip()
        p.git(self.root, 'push', 'origin', manual + ':refs/heads/' + old['head']['ref'])
        pr = self.api.prs[old['number']]
        pr['head']['sha'] = manual
        pr['body'] = pr['body'].replace(head, manual)
        self.change('anthropic/claude-code', b'newer\n')
        with self.assertRaisesRegex(ValueError, 'non-installer'):
            self.publish()
        self.assertEqual(pr['state'], 'open')


if __name__ == '__main__':
    for name in ('GITHUB_OUTPUT', 'GITHUB_STEP_SUMMARY'):
        os.environ.pop(name, None)
    with contextlib.redirect_stdout(io.StringIO()):
        unittest.main(verbosity=2)
