#!/usr/bin/env python3
"""Check the exact runtime image's ELF and default serve on a CPU without AVX."""
import argparse
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid

from release_checks import require


def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT)


def check(image, version, revision, qemu):
    info = json.loads(run('docker', 'image', 'inspect', image))[0]
    require((info['Os'], info['Architecture']) == ('linux', 'amd64'), 'Image platform is not linux/amd64')
    require(info['Config'].get('Entrypoint') == ['/redapp'], 'Unexpected image entrypoint')
    require(info['Config'].get('Cmd') == ['serve'], 'Unexpected image default command')
    with tempfile.TemporaryDirectory(prefix='redapp-cpu-') as directory:
        root = Path(directory)
        binary = root / 'redapp'
        container = 'redapp-cpu-' + uuid.uuid4().hex
        run('docker', 'create', '--name', container, image)
        try:
            run('docker', 'cp', container + ':/redapp', str(binary))
        finally:
            run('docker', 'rm', '-v', container)
        require(os.access(binary, os.X_OK), 'Image binary is not executable')
        require('Advanced Micro Devices X86-64' in run('readelf', '-h', str(binary)), 'Image binary is not x86-64')
        require('INTERP' not in run('readelf', '-l', str(binary)), 'Expected a static binary')
        notes = run('readelf', '-n', str(binary))
        isa = [line.strip() for line in notes.splitlines() if 'x86 ISA needed:' in line]
        require(isa == ['Properties: x86 ISA needed: x86-64-baseline'], f'Unexpected x86 ISA requirement: {isa}')
        command = [qemu, '-cpu', 'Nehalem', str(binary)]
        expected = f'RedApp {version} (commit {revision})\n'
        require(run(*command, 'version') == expected, 'Unexpected version output under Nehalem')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        env = os.environ.copy()
        # Do not inherit deployment overrides or read the host's optional config.
        env = {key: value for key, value in env.items() if not key.startswith('REDAPP_')}
        config = root / 'empty.json'
        config.write_text('{}')
        env.update(REDAPP_CONFIG=str(config), REDAPP_DATA=str(root / 'data'),
                   REDAPP_LISTEN=f'127.0.0.1:{port}')
        # No command override: use the actual image's default Cmd.
        with (root / 'serve.log').open('wb') as log:
            process = subprocess.Popen(command + info['Config']['Cmd'], env=env,
                                       stdout=log, stderr=log)
            try:
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                deadline = time.monotonic() + 25
                while True:
                    require(process.poll() is None, f'Serve exited with {process.returncode}')
                    try:
                        with opener.open(f'http://127.0.0.1:{port}/health/ready', timeout=1) as response:
                            require(response.status == 200, f'Readiness returned HTTP {response.status}')
                        break
                    except (OSError, urllib.error.URLError):
                        require(time.monotonic() < deadline, 'Serve did not become healthy')
                        time.sleep(0.2)
                healthcheck = subprocess.run(command + ['healthcheck'], env=env, capture_output=True, timeout=10)
                require(healthcheck.returncode == 0, 'Healthcheck failed')
                process.terminate()
                require(process.wait(timeout=10) == 0, 'Serve did not shut down cleanly')
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
        require('RedApp started:' in (root / 'serve.log').read_text(), 'Serve log lacks startup line')
    print('Exact amd64 image: ELF baseline, Nehalem/no-AVX version, default serve, health and clean shutdown passed')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--revision', required=True)
    parser.add_argument('--qemu', default=shutil.which('qemu-x86_64'))
    args = parser.parse_args()
    require(args.qemu, 'qemu-x86_64 is required')
    check(args.image, args.version, args.revision, args.qemu)
