#!/usr/bin/env python3
"""Both providers share check/apply/digest/context contracts in disposable copies."""
from pathlib import Path
import json
import shutil
import subprocess
import tempfile
from installer_manifest import ROOT, applications
from installer_test_support import file_state, require, sha
from installer_maintenance import SIGNATURE_BEGIN, SIGNATURE_END, unsigned

for app in applications():
    with tempfile.TemporaryDirectory(prefix='installer-updater-test-') as tmp:
        fixture = Path(tmp)
        for name in ('installers', 'scripts'):
            shutil.copytree(ROOT / name, fixture / name, ignore=shutil.ignore_patterns('__pycache__'))
        manifest = fixture / '.generated'
        manifest.mkdir(parents=True)
        shutil.copyfile(
            ROOT / '.generated/installer-inventory.json', manifest / 'installer-inventory.json'
        )
        target = fixture / 'installers' / app['id']
        before = file_state(target)
        command = ['python3', str(fixture / 'scripts/update-installers.py'), '--application', app['id']]

        def execute(options):
            """Run update-installers.py in the fixture copy with extra options."""
            return subprocess.run(command + options, capture_output=True, text=True, timeout=180)

        checked = execute(['--source', str(target / 'upstream')])
        require(checked.returncode == 0, checked.stderr)
        require(file_state(target) == before, 'check mode changed files')
        signed = [
            name for name in ('install.ps1', 'install.sh')
            if SIGNATURE_BEGIN in (target / 'upstream' / name).read_bytes()
        ]
        if signed:
            # Re-signing with a new block is not a change; code hidden beside a block is.
            resigned = fixture / 'resigned'
            hidden = fixture / 'hidden'
            shutil.copytree(target / 'upstream', resigned)
            shutil.copytree(target / 'upstream', hidden)
            for name in signed:
                body = unsigned((target / 'upstream' / name).read_bytes())
                block = b'\r\n' + SIGNATURE_BEGIN + b'\r\n# UkVTSUdORUQ=\r\n' + SIGNATURE_END + b'\r\n'
                (resigned / name).write_bytes(body + block)
                (hidden / name).write_bytes(body + block.rstrip() + b'\r\nWrite-Host hidden' + block)
            for mode in ([], ['--apply']):
                ok = execute(['--source', str(resigned)] + mode)
                require(ok.returncode == 0, ok.stderr)
                require(
                    {k: v for k, v in file_state(target).items() if k != 'provenance.json'}
                    == {k: v for k, v in before.items() if k != 'provenance.json'},
                    'signature-only change rewrote installers',
                )
            bad = execute(['--source', str(hidden)])
            require(bad.returncode != 0 and 'maintainer review' in bad.stderr, bad.stderr)
            before = file_state(target)
        source = fixture / 'changed'
        shutil.copytree(target / 'upstream', source)
        file = source / 'install.sh'
        file.write_bytes(file.read_bytes().replace(b'BASE_URL=', b'CHANGED_BASE_URL=', 1))
        for options in [[], ['--shell-sha256', sha(file.read_bytes())]]:
            failed = execute(['--source', str(source), '--apply'] + options)
            require(failed.returncode != 0, 'digest/patch context failure accepted')
            require(file_state(target) == before, 'failure changed existing installer tree')
        applied = execute(['--source', str(target / 'upstream'), '--apply'])
        require(applied.returncode == 0, applied.stderr)
        after = file_state(target)
        require(
            {k: v for k, v in after.items() if k != 'provenance.json'}
            == {k: v for k, v in before.items() if k != 'provenance.json'},
        )
        require(
            json.loads(after['provenance.json'])['retrieved_at']
            != json.loads(before['provenance.json'])['retrieved_at'],
        )
        print(app['id'] + ': check-only, digest/context protection and isolated atomic apply PASS')
