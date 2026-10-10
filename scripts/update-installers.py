#!/usr/bin/env python3
"""Unified installer maintenance: audited sources, conflict-free patches, Shell contracts, atomic local apply.

PowerShell validation belongs to the Windows job; local apply is not release approval.
"""
import argparse
import ctypes
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from installer_manifest import ROOT, applications
from installer_maintenance import digest, download, script_shape, apply_patch, validate_shell


def exchange(a, b):
    # Linux renameat2 atomically swaps the entire installer tree, including provenance.
    libc = ctypes.CDLL(None, use_errno=True)
    fn = libc.renameat2
    fn.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    fn.restype = ctypes.c_int
    if fn(-100, os.fsencode(a), -100, os.fsencode(b), 2):
        raise OSError(ctypes.get_errno(), "安装器目录原子交换失败")
    for parent in {a.parent, b.parent}:
        fd = os.open(parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--application',required=True,choices=[a['id'] for a in applications()])
    parser.add_argument('--source',type=Path,help='Offline originals; otherwise use reviewed manifest HTTPS sources')
    parser.add_argument('--shell-sha256');parser.add_argument('--powershell-sha256')
    parser.add_argument('--apply',action='store_true')
    args=parser.parse_args()
    descriptor=next(a for a in applications() if a['id']==args.application)
    target=ROOT/'installers'/args.application
    manifest=json.loads((target/'provenance.json').read_text())
    with tempfile.TemporaryDirectory(prefix='.installer-stage-',dir=target.parent) as tmp:
        stage=Path(tmp)/target.name;shutil.copytree(target,stage)
        for installer in descriptor['installers']:
            name=installer['file']
            raw=(args.source/name).read_bytes() if args.source else download(installer['source'])
            script_shape(name,raw)
            explicit=args.powershell_sha256 if installer['shell']=='powershell' else args.shell_sha256
            expected=explicit or manifest['files'][name]['sha256']
            if not re.fullmatch('[0-9a-f]{64}',expected) or digest(raw)!=expected:
                raise ValueError(name+': audited digest differs; review and provide the expected SHA256')
            (stage/'upstream'/name).write_bytes(raw)
            (stage/'generated'/name).write_bytes(apply_patch(raw,stage/'patches'/(name+'.patch')))
            manifest['files'][name].update(bytes=len(raw),sha256=digest(raw),source=installer['source'])
        validate_shell(stage/'generated',descriptor)
        if args.apply:
            manifest['retrieved_at']=datetime.now(timezone.utc).isoformat()
            (stage/'provenance.json').write_text(json.dumps(manifest,indent=2,ensure_ascii=False)+'\n')
            for file in stage.rglob('*'):
                if file.is_file():
                    with file.open('rb') as f:os.fsync(f.fileno())
            exchange(stage,target)
        print(args.application+(': local atomic apply PASS' if args.apply else ': check-only PASS')+'; Windows PS7/5.1 candidate validation is still required')


if __name__=='__main__':
    try:main()
    except (OSError,ValueError,subprocess.SubprocessError) as error:raise SystemExit('Installer update failed: '+str(error))
