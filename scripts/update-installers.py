#!/usr/bin/env python3
"""维护命令：固定源码身份 → 严格 patch → 离线测试 → 原子替换本地基线。"""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from datetime import datetime, timezone
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
PIN = "ff6aec96948b70d94983af2641a6b67c94faeff5"

def run(args, env=None):
    result = subprocess.run(args, text=True, capture_output=True, check=True, timeout=180, env=env)
    return result.stdout + result.stderr

def audit(directory):
    shell = (directory / "install.sh").read_text()
    ps = (directory / "install.ps1").read_text()
    for name, text in [("install.sh", shell), ("install.ps1", ps)]:
        if re.search(r"https?://|Invoke-RestMethod", text):
            raise ValueError(f"{name}: 出现未授权网络入口")
        if text.count("@REDAPP_BASE_URL@") != 1:
            raise ValueError(f"{name}: enterprise origin 占位符异常")
    if 'rm -f "$AUTO_UPDATE_VERSION"' not in shell or 'AUTO_UPDATE_VERSION.tmp' in shell:
        raise ValueError("shell 自动更新策略异常")
    if 'WriteAllText($tempMarker' in ps or 'Remove-Item -LiteralPath $autoUpdateVersion' not in ps:
        raise ValueError("PowerShell 自动更新策略异常")
    run(["sh", "-n", str(directory / "install.sh")])
    if shutil.which("pwsh"):
        code = "$e=$null;$t=$null;[System.Management.Automation.Language.Parser]::ParseFile($env:REDAPP_INSTALLER_SYNTAX_PATH,[ref]$t,[ref]$e)|Out-Null;if($e.Count){exit 1}"
        run(["pwsh", "-NoProfile", "-NonInteractive", "-Command", code], env={**os.environ, "REDAPP_INSTALLER_SYNTAX_PATH": str(directory / "install.ps1")})

def exchange(a, b):
    # Linux renameat2 atomically swaps the entire installer tree, including provenance.
    libc = ctypes.CDLL(None, use_errno=True)
    fn = libc.renameat2
    fn.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    fn.restype = ctypes.c_int
    if fn(-100, os.fsencode(a), -100, os.fsencode(b), 2):
        raise OSError(ctypes.get_errno(), "安装器目录原子交换失败")
    fd = os.open(a.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--commit", default=PIN, help="不可变官方 commit，禁止 main/tag")
    parser.add_argument("--source", type=Path, help="离线官方原文目录；省略则获取固定官方来源")
    parser.add_argument("--shell-sha256", help="新 shell 基线预期哈希")
    parser.add_argument("--powershell-sha256", help="新 PowerShell 基线预期哈希")
    parser.add_argument("--apply", action="store_true", help="测试通过后替换本地安装器目录")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.commit):
        parser.error("必须提供不可变 40 位 commit")
    target = ROOT / "installers/codex"
    manifest = json.loads((target / "provenance.json").read_text())
    expected = {k: v["sha256"] for k, v in manifest["files"].items()}
    if args.commit != manifest["commit"] and not (args.shell_sha256 and args.powershell_sha256):
        parser.error("更换基线需显式提供已核验的两份脚本 SHA256")
    for name, digest in [("install.sh", args.shell_sha256), ("install.ps1", args.powershell_sha256)]:
        if digest:
            if not re.fullmatch(r"[0-9a-f]{64}", digest):
                parser.error("SHA256 格式无效")
            expected[name] = digest
    with tempfile.TemporaryDirectory(prefix=".codex-stage-", dir=target.parent) as work:
        stage = Path(work) / "codex"
        shutil.copytree(target, stage)
        files = {}
        for name in ["install.sh", "install.ps1", "LICENSE", "NOTICE"]:
            if args.source:
                data = (args.source / name).read_bytes()
            else:
                suffix = "scripts/install/" + name if name.startswith("install.") else name
                url = f"https://raw.githubusercontent.com/openai/codex/{args.commit}/{suffix}"
                with urllib.request.urlopen(url, timeout=30) as response:
                    if response.url != url:
                        raise ValueError("来源发生重定向，请先核验")
                    data = response.read(256 * 1024 + 1)
            digest = hashlib.sha256(data).hexdigest()
            if not data or len(data) > 256 * 1024:
                raise ValueError(f"{name}: 源码为空或超限")
            if (args.commit == manifest["commit"] or name.startswith("install.")) and digest != expected[name]:
                raise ValueError(f"{name}: 官方源码身份不匹配")
            (stage / "upstream" / name).write_bytes(data)
            files[name] = {"bytes": len(data), "sha256": digest}
        for name in ["install.sh", "install.ps1"]:
            generated = stage / "generated" / name
            shutil.copyfile(stage / "upstream" / name, generated)
            output = run(["patch", "--batch", "--forward", "--fuzz=0", str(generated), str(stage / "patches" / (name + ".patch"))])
            if re.search(r"offset|fuzz|FAILED|Reversed", output, re.I):
                raise ValueError("补丁上下文变化，停止自动更新")
        audit(stage / "generated")
        run(["python3", str(ROOT / "scripts/test-installers.py"), "--directory", str(stage / "generated")])
        manifest.update(commit=args.commit, files=files, retrieved_at=datetime.now(timezone.utc).isoformat())
        (stage / "provenance.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
        if args.apply:
            for path in stage.rglob("*"):
                if path.is_file():
                    with path.open("rb") as file:
                        os.fsync(file.fileno())
            exchange(stage, target)
            print("安装器已通过离线检查并原子替换；请审阅 diff 后重新构建。")
        else:
            print("固定身份、严格补丁与离线测试通过；检查模式未修改安装器。")

if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        raise SystemExit("安装器更新失败，已发布目录保持不变：" + str(error))
