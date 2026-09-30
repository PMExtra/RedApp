#!/usr/bin/env python3
"""证明源码摘要/补丁冲突不会覆盖已发布安装器。"""
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
target = root / "installers/codex"
before = {p.relative_to(target): hashlib.sha256(p.read_bytes()).digest() for p in target.rglob("*") if p.is_file()}
with tempfile.TemporaryDirectory(prefix="redapp-updater-test-") as temp:
    source = Path(temp)
    for file in (target / "upstream").iterdir():
        shutil.copyfile(file, source / file.name)
    # Published assets must exactly equal the current baseline plus strict patches.
    for name in ["install.sh", "install.ps1"]:
        generated = source / (name + ".generated")
        shutil.copyfile(source / name, generated)
        result = subprocess.run(["patch", "--batch", "--forward", "--fuzz=0", str(generated), str(target / "patches" / (name + ".patch"))], capture_output=True, text=True)
        assert result.returncode == 0 and "offset" not in result.stdout and "fuzz" not in result.stdout, result.stdout
        assert generated.read_bytes() == (target / "generated" / name).read_bytes(), "generated assets drifted from patches"
    result = subprocess.run(["python3", str(root / "scripts/update-installers.py"), "--source", str(source)], capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
    shell = source / "install.sh"
    shell.write_bytes(shell.read_bytes().replace(b"RELEASES_BASE_URL=", b"CHANGED_RELEASES_BASE_URL=", 1))
    # Hash failure must stop even before patch application.
    result = subprocess.run(["python3", str(root / "scripts/update-installers.py"), "--source", str(source), "--apply"], capture_output=True, text=True)
    assert result.returncode != 0, "bad source digest accepted"
    # Explicitly trusted changed digest still cannot bypass strict patch context.
    result = subprocess.run(["python3", str(root / "scripts/update-installers.py"), "--source", str(source), "--shell-sha256", hashlib.sha256(shell.read_bytes()).hexdigest(), "--apply"], capture_output=True, text=True)
    assert result.returncode != 0, "patch conflict ignored"
after = {p.relative_to(target): hashlib.sha256(p.read_bytes()).digest() for p in target.rglob("*") if p.is_file()}
assert before == after, "failed updater changed published installer tree"
print("更新器检查模式、源码摘要拒绝、严格补丁冲突保留旧目录：通过")
