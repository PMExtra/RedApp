"""Harmless artifacts and loopback transport shared by both installer protocols/hosts."""
import hashlib
import http.server
import io
import json
import tarfile
import threading


def sha(data):
    return hashlib.sha256(data).hexdigest()


def codex_fixture(target, npm, legacy=False, version='0.159.2', body=None, failure=''):
    windows = 'windows' in target
    suffix = '.exe' if windows else ''
    binary = body if body is not None else f"#!/bin/sh\necho 'codex-cli {version}'\n".encode()
    helper = body if body is not None else b'#!/bin/sh\nexit 0\n'
    files = {f'bin/codex{suffix}': binary, f'bin/codex-code-mode-host{suffix}': helper,
             f'codex-path/rg{suffix}': helper, 'codex-package.json': b'{}'}
    resources = ['codex-command-runner.exe', 'codex-windows-sandbox-setup.exe'] if windows else ['bwrap']
    files.update({'codex-resources/' + name: helper for name in resources})
    if legacy:
        prefix = f'package/vendor/{target}/'
        files = {prefix + f'codex/codex{suffix}': binary, prefix + f'path/rg{suffix}': helper}
        files.update({prefix + ('codex/' if windows else 'codex-resources/') + name: helper for name in resources})
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w:gz') as tar:
        for name, data in files.items():
            entry = tarfile.TarInfo(name)
            entry.size, entry.mode = len(data), 0o755
            tar.addfile(entry, io.BytesIO(data))
    archive = stream.getvalue()
    name = f'codex-npm-{npm}-{version}.tgz' if legacy else f'codex-package-{target}.tar.gz'
    manifest = f'{sha(archive)}  {name if failure != "manifest-entry" else "unrelated.tar.gz"}\n'.encode()
    metadata = {'tag_name': 'rust-v' + ('0.159.3' if failure == 'tag' else version), 'assets': [
        {'name': name, 'digest': 'sha256:' + sha(archive), 'browser_download_url': 'https://attacker.example/injected'}]}
    if not legacy:
        metadata['assets'].append({'name': 'codex-package_SHA256SUMS', 'digest': 'sha256:' + sha(manifest),
                                   'browser_download_url': 'https://attacker.example/manifest'})
    files = {name: archive, 'codex-package_SHA256SUMS': manifest}
    if failure == 'missing-digest': metadata['assets'][0]['digest'] = 'invalid'
    if failure == 'missing-package': metadata['assets'] = metadata['assets'][1:]
    if failure == 'manifest-hash': files['codex-package_SHA256SUMS'] += b'corruption'
    if failure == 'archive-hash': files[name] += b'corruption'
    if failure == 'truncated': files[name] = archive[:len(archive) // 2]
    return dict(metadata=metadata, files=files, failure=failure, version=version, asset=name)


class InstallerServer:
    """Real HTTP client behavior; redirects stay local so a regression cannot escape."""
    def __init__(self, application, provider):
        self.config, self.seen = {}, []
        fixture = self
        prefix = '/' + application

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_): pass

            def do_GET(self):
                if not self.path.startswith(prefix + '/'):
                    fixture.seen.append(self.path)
                    self.send_error(404)
                    return
                path = self.path[len(prefix):]
                fixture.seen.append(path)
                status, body, headers = fixture.response(provider, path)
                self.send_response(status)
                content_type = 'application/json' if path.endswith('.json') or (provider == 'codex' and path.endswith('latest')) else 'text/plain'
                self.send_header('Content-Type', content_type)
                for key, value in headers.items(): self.send_header(key, value)
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.base = f'http://127.0.0.1:{self.server.server_port}' + prefix

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def response(self, provider, path):
        config = self.config
        failure, version = config.get('failure', ''), config['version']
        if provider == 'codex':
            stage = 'metadata' if path.endswith(('latest', 'release.json')) else (
                'manifest' if path.endswith('SHA256SUMS') else 'artifact')
            if failure == 'redirect-' + stage:
                return 302, b'', {'Location': '/escaped'}
            body = (b'not json' if failure == 'metadata' else json.dumps(config['metadata']).encode()) if stage == 'metadata' else config['files'].get(path.rsplit('/', 1)[-1])
        else:
            binary = config.get('binary', 'claude')
            artifact = f'/{version}/{config["platform"]}/{binary}'
            stage = { '/latest': 'channel', '/stable': 'channel', f'/{version}/manifest.json': 'manifest', artifact: 'artifact' }.get(path)
            if failure == 'redirect-' + str(stage):
                return 302, b'', {'Location': '/escaped'}
            if stage == 'channel':
                body = b'2.1.285/../../escape' if failure == 'channel' else version.encode()
            elif stage == 'manifest':
                body = json.dumps({'version': version, 'platforms': {config['platform']: {
                    'binary': binary, 'checksum': sha(config['body']), 'size': len(config['body'])}}}).encode()
                if failure == 'manifest': body = b'<html>Error</html>'
            elif stage == 'artifact':
                if failure == 'download': return 503, b'', {}
                body = config['body'] + (b'tampered' if failure == 'hash' else b'')
            else: body = None
        return (404, b'', {}) if body is None else (200, body, {})


def file_state(directory):
    return {str(p.relative_to(directory)): p.read_bytes() for p in directory.rglob('*') if p.is_file()}
