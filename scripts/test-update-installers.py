#!/usr/bin/env python3
"""Both providers share check/apply/digest/context contracts in disposable copies."""
from pathlib import Path
import json
import shutil
import subprocess
import tempfile
from installer_manifest import ROOT, applications
from installer_test_support import sha, file_state

for app in applications():
    with tempfile.TemporaryDirectory(prefix='installer-updater-test-') as tmp:
        fixture=Path(tmp)
        for name in ('installers','scripts'):
            shutil.copytree(ROOT/name,fixture/name,ignore=shutil.ignore_patterns('__pycache__'))
        manifest=fixture/'internal/apps/builtin';manifest.mkdir(parents=True)
        shutil.copyfile(ROOT/'internal/apps/builtin/manifest.json',manifest/'manifest.json')
        target=fixture/'installers'/app['id']
        before=file_state(target)
        command=['python3',str(fixture/'scripts/update-installers.py'),'--application',app['id']]
        def execute(options):
            return subprocess.run(command+options,capture_output=True,text=True,timeout=180)
        checked=execute(['--source',str(target/'upstream')])
        assert checked.returncode==0,checked.stderr
        assert file_state(target)==before,'check mode changed files'
        source=fixture/'changed';shutil.copytree(target/'upstream',source)
        file=source/'install.sh';file.write_bytes(file.read_bytes().replace(b'BASE_URL=',b'CHANGED_BASE_URL=',1))
        for options in [[],['--shell-sha256',sha(file.read_bytes())]]:
            failed=execute(['--source',str(source),'--apply']+options)
            assert failed.returncode!=0,'digest/patch context failure accepted'
            assert file_state(target)==before,'failure changed existing installer tree'
        applied=execute(['--source',str(target/'upstream'),'--apply'])
        assert applied.returncode==0,applied.stderr
        after=file_state(target)
        assert {k:v for k,v in after.items() if k!='provenance.json'}=={k:v for k,v in before.items() if k!='provenance.json'}
        assert json.loads(after['provenance.json'])['retrieved_at']!=json.loads(before['provenance.json'])['retrieved_at']
        print(app['id']+': check-only, digest/context protection and isolated atomic apply PASS')
