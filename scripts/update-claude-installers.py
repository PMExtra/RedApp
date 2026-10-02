#!/usr/bin/env python3
"""Claude 安装器：核验原文、严格应用 patch、离线测试后原子替换；默认仅检查。"""
import argparse
from datetime import datetime, timezone
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from installer_maintenance import ROOT,audit_claude,digest,download,inventory,run,script_shape,strict_patch

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source',type=Path)
    parser.add_argument('--shell-sha256');parser.add_argument('--powershell-sha256')
    parser.add_argument('--apply',action='store_true');args=parser.parse_args()
    target=ROOT/'installers/anthropic/claude-code';manifest=json.loads((target/'provenance.json').read_text())
    items={x['name']:x for x in inventory() if x['application']=='anthropic/claude-code'}
    with tempfile.TemporaryDirectory(prefix='.claude-stage-',dir=target.parent) as tmp:
        stage=Path(tmp)/'claude-code';shutil.copytree(target,stage)
        for name,explicit in [('install.sh',args.shell_sha256),('install.ps1',args.powershell_sha256)]:
            raw=(args.source/name).read_bytes() if args.source else download(items[name]['url'])
            script_shape(name,raw)
            expected=explicit or manifest['files'][name]['sha256']
            if digest(raw)!=expected:raise ValueError(name+': official baseline digest differs; review and provide an explicit expected digest')
            (stage/'upstream'/name).write_bytes(raw)
            (stage/'generated'/name).write_bytes(strict_patch(raw,stage/'patches'/(name+'.patch')))
            manifest['files'][name].update(bytes=len(raw),sha256=digest(raw))
        audit_claude(stage/'generated')
        print(run(['python3',str(ROOT/'scripts/test-claude-installers.py'),'--directory',str(stage/'generated')]),end='')
        if args.apply:
            manifest['retrieved_at']=datetime.now(timezone.utc).isoformat()
            (stage/'provenance.json').write_text(json.dumps(manifest,indent=2,ensure_ascii=False)+'\n')
            spec=importlib.util.spec_from_file_location('codex_updater',ROOT/'scripts/update-installers.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
            for file in stage.rglob('*'):
                if file.is_file():
                    with file.open('rb') as f: os.fsync(f.fileno())
            module.exchange(stage,target)
            print('Claude 安装器已原子替换；请审阅 diff 后重新构建。')
        else:print('Claude 原文、严格 patch 与离线测试通过；检查模式未修改安装器。')

if __name__=='__main__':
    try:main()
    except (OSError,ValueError,subprocess.SubprocessError) as error:raise SystemExit('Claude 更新失败，请核对目录与 provenance：'+str(error))
