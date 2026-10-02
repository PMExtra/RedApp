#!/usr/bin/env python3
"""Claude updater digest/patch failures keep the complete installed tree unchanged."""
from pathlib import Path
import hashlib
import shutil
import subprocess
import tempfile

root=Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix='claude-updater-test-') as tmp:
    fixture=Path(tmp)
    shutil.copytree(root/'installers',fixture/'installers')
    shutil.copytree(root/'scripts',fixture/'scripts',ignore=shutil.ignore_patterns('__pycache__'))
    target=fixture/'installers/claude-code'
    def state():return {str(p.relative_to(target)):hashlib.sha256(p.read_bytes()).hexdigest() for p in target.rglob('*') if p.is_file()}
    before=state()
    command=['python3',str(fixture/'scripts/update-claude-installers.py'),'--source',str(target/'upstream')]
    checked=subprocess.run(command,capture_output=True,text=True,timeout=180)
    assert checked.returncode==0,checked.stderr
    assert state()==before,'check mode changed installer tree'
    source=fixture/'changed';shutil.copytree(target/'upstream',source)
    file=source/'install.sh';file.write_bytes(file.read_bytes().replace(b'DOWNLOAD_BASE_URL=',b'CHANGED_DOWNLOAD_BASE_URL=',1))
    command=['python3',str(fixture/'scripts/update-claude-installers.py'),'--source',str(source),'--apply']
    for options in [[],['--shell-sha256',hashlib.sha256(file.read_bytes()).hexdigest()]]:
        result=subprocess.run(command+options,capture_output=True,text=True,timeout=30)
        assert result.returncode!=0,'invalid baseline/context accepted'
        assert state()==before,'failure replaced the audited installer tree'
    valid=['python3',str(fixture/'scripts/update-claude-installers.py'),'--source',str(target/'upstream'),'--apply']
    applied=subprocess.run(valid,capture_output=True,text=True,timeout=180)
    assert applied.returncode==0,applied.stderr
    after=state()
    assert set(after)==set(before),'atomic apply lost files'
    assert {k:v for k,v in after.items() if k!='provenance.json'}=={k:v for k,v in before.items() if k!='provenance.json'},'apply changed audited source or patch output'
    assert after['provenance.json']!=before['provenance.json'],'apply did not update provenance'
print('Claude updater: check-only, digest/context failure protection and successful atomic apply in an isolated copy: PASS')
