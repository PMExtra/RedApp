#!/usr/bin/env python3
"""Exercise the same exact-bundle Windows entry used before updater draft publication."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import zipfile
from installer_manifest import ROOT, inventory
from installer_test_support import sha


def bundle(path, baseline, broken=False):
    rows, files = [], {}
    for item in inventory():
        app, name = item['application'], item['name']
        base = ROOT/'installers'/app
        original = (base/'upstream'/name).read_bytes()
        changed = name == 'install.ps1'
        raw = original + b'\n# Candidate contract fixture\n' if changed else original
        rows.append({**item,'status':'changed' if changed else 'unchanged',
                     'baseline_sha256':sha(original),'current_sha256':sha(raw)})
        if changed:
            generated = (base/'generated'/name).read_bytes() + b'\n# Candidate contract fixture\n'
            if broken and app == 'openai/codex': generated = b'param(\n'
            files[f'installers/{app}/upstream/{name}'] = raw
            files[f'installers/{app}/generated/{name}'] = generated
            provenance = json.loads((base/'provenance.json').read_text())
            provenance['files'][name].update(bytes=len(raw),sha256=sha(raw))
            files[f'installers/{app}/provenance.json'] = json.dumps(provenance).encode()
    payload = dict(baseline=baseline,rows=rows,files={name:sha(data) for name,data in files.items()})
    with zipfile.ZipFile(path,'w') as archive:
        for name,data in files.items(): archive.writestr(name,data)
        archive.writestr('update.json',json.dumps(payload))
    return sha(path.read_bytes())


def main():
    if os.name != 'nt': raise SystemExit('Candidate PowerShell contracts require Windows')
    baseline = subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
    before = {ROOT/'installers'/i['application']/'generated'/i['name']:None for i in inventory()}
    before = {path:path.read_bytes() for path in before}
    with tempfile.TemporaryDirectory(prefix='redapp-candidate-contract-') as temp:
        path = Path(temp)/'candidate.zip'
        digest = bundle(path,baseline)
        command = [sys.executable,str(ROOT/'scripts/test-installers.py'),'--platform','windows',
                   '--bundle',str(path),'--baseline',baseline,'--sha256',digest]
        subprocess.run(command,check=True,timeout=900)
        digest = bundle(path,baseline,broken=True)
        command[-1] = digest
        for engine in ('pwsh','powershell.exe'):
            rejected = subprocess.run(command+['--shell',engine],capture_output=True,text=True,timeout=120)
            assert rejected.returncode != 0 and 'REDAPP_PS_PARSE_FAILED' in rejected.stderr,rejected.stdout+rejected.stderr
            print(engine+': rejected broken candidate while baseline stayed valid',flush=True)
    assert all(path.read_bytes()==data for path,data in before.items()),'candidate validation changed baseline'


if __name__=='__main__':main()
