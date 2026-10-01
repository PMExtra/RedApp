#!/usr/bin/env python3
"""验证真实 CLI 的数据目录默认值、覆盖优先级和权限失败；不输出初始化密码。"""
import os
from pathlib import Path
import re
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
binary = str(root / "bin/redapp")
env = os.environ.copy()
env.pop("REDAPP_DATA", None)

def invoke(args, directory, overrides=None):
    return subprocess.run([binary, *args], cwd=directory,
                          env={**env, **(overrides or {})}, capture_output=True,
                          text=True, timeout=10)

with tempfile.TemporaryDirectory(prefix="redapp-data-cli-") as temp:
    directory = Path(temp)
    for value in [None, ""]:
        result = invoke(["--help"], directory, {} if value is None else {"REDAPP_DATA": value})
        assert result.returncode == 0
        assert re.search(r'-data string\s+.*\(default "/var/lib/redapp"\)', result.stderr)
    selected = directory / "environment"
    # An invalid listen address ends startup after database initialization, without binding a port.
    result = invoke(["--listen", "invalid"], directory, {"REDAPP_DATA": str(selected)})
    assert result.returncode == 1 and (selected / "state.sqlite").is_file()
    selected = directory / "explicit"
    ignored = directory / "ignored"
    result = invoke(["--data", str(selected), "--listen", "invalid"], directory,
                    {"REDAPP_DATA": str(ignored)})
    assert result.returncode == 1 and (selected / "state.sqlite").is_file()
    assert not ignored.exists()
    result = invoke(["--data", "./data", "--listen", "invalid"], directory)
    assert result.returncode == 1 and (directory / "data/state.sqlite").is_file()
    # A read-only Linux filesystem denies writes even when the test is run as root.
    blocked = "/sys/redapp-permission-test"
    for args, overrides in [([], {"REDAPP_DATA": blocked}),
                            (["--data", blocked], {"REDAPP_DATA": str(ignored)})]:
        result = invoke(args, directory, overrides)
        assert result.returncode == 1 and blocked in result.stderr
        assert "read-only file system" in result.stderr.lower() or "permission denied" in result.stderr.lower()
        assert not ignored.exists()
    # Verify no new fallback directory is created after a failing start.
    fallback = directory / "fallback"
    fallback.mkdir()
    result = invoke(["--data", blocked], fallback)
    assert result.returncode == 1 and list(fallback.iterdir()) == []
print("真实 CLI：默认目录、空环境默认值、环境/参数覆盖优先级、显式开发目录、权限失败不回退通过。")
