#!/usr/bin/env python3
"""CLI 离线安装器回归：真实 shell 执行，四种 shell 平台分支模拟，禁止任何公网请求。"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from installer_test_support import InstallerServer, codex_fixture


def test_shell(directory, application):
    fixture = InstallerServer(application, 'codex')
    fixture.__enter__()
    config, seen, base = fixture.config, fixture.seen, fixture.base
    curl = shutil.which("curl")
    if not curl:
        raise SystemExit("需要 curl 运行离线安装器测试")
    variants = [("Linux", "x86_64", "x86_64-unknown-linux-musl", "linux-x64"),
                ("Linux", "aarch64", "aarch64-unknown-linux-musl", "linux-arm64"),
                ("Darwin", "x86_64", "x86_64-apple-darwin", "darwin-x64"),
                ("Darwin", "arm64", "aarch64-apple-darwin", "darwin-arm64")]
    count = 0
    try:
        with tempfile.TemporaryDirectory(prefix="redapp-install-test-") as temp:
            root = Path(temp)
            for variant in variants:
                run_case(root, directory, base, curl, config, seen, *variant, False, "latest", "")
                count += 1
            for legacy, requested in [(True, "latest"), (False, "0.159.2")]:
                run_case(root, directory, base, curl, config, seen, *variants[0], legacy, requested, "")
                count += 1
            for failure in ["metadata", "tag", "missing-digest", "missing-package", "manifest-hash", "manifest-entry", "archive-hash", "truncated", "redirect-metadata", "redirect-manifest", "redirect-artifact"]:
                run_case(root, directory, base, curl, config, seen, *variants[0], False, "latest", failure)
                count += 1
    finally:
        fixture.__exit__()
    print(f'Codex Shell: {count} offline cases PASS; platform selection simulated')


def run_case(root, directory, base, curl, config, seen, osname, arch, target, npm, legacy, requested, failure):
    case = root / (str(len(list(root.iterdir()))) + "-case")
    wrapper = case / "tools"
    bindir = case / "bin"
    wrapper.mkdir(parents=True)
    bindir.mkdir()
    log = case / "network.jsonl"
    (wrapper / "curl").write_text("#!/usr/bin/python3\nimport sys,json,subprocess\nfrom pathlib import Path\nurls=[a for a in sys.argv[1:] if a.startswith(('http://','https://'))]\nwith open(" + repr(str(log)) + ", 'a') as f: f.write(json.dumps(urls)+'\\n')\nif len(urls)!=1 or not urls[0].startswith(" + repr(base + "/") + "): sys.exit(77)\nsys.exit(subprocess.call([" + repr(curl) + "]+sys.argv[1:]))\n")
    (wrapper / "curl").chmod(0o755)
    (wrapper / "uname").write_text(f"#!/bin/sh\ncase \"$1\" in -s) echo {osname};; -m) echo {arch};; *) exit 1;; esac\n")
    (wrapper / "uname").chmod(0o755)
    (wrapper / "sysctl").write_text("#!/bin/sh\necho 0\n")
    (wrapper / "sysctl").chmod(0o755)
    for command in ["npm", "bun", "brew", "codex"]:
        # Never launch an existing CLI or package manager while testing an installer.
        if command == "codex":
            continue
        (wrapper / command).write_text("#!/bin/sh\nexit 77\n")
        (wrapper / command).chmod(0o755)
    if failure == 'tag': requested = '0.159.2'
    config.clear()
    config.update(codex_fixture(target, npm, legacy, failure=failure))
    seen.clear()
    script = case / "install.sh"
    script.write_text((directory / "install.sh").read_text().replace("@REDAPP_BASE_URL@", base))
    env = os.environ.copy()
    env.update(CODEX_HOME=str(case / "codex-home"), CODEX_INSTALL_DIR=str(bindir), CODEX_RELEASE=requested,
               CODEX_NON_INTERACTIVE="1", CODEX_INSTALLER_USE_RELEASES_OPENAI_COM="0",
               PATH=str(wrapper) + ":" + str(bindir) + ":/usr/bin:/bin")
    result = subprocess.run(["sh", str(script)], env=env, capture_output=True, text=True, timeout=30)
    installed = bindir / "codex"
    if failure:
        if result.returncode == 0 or installed.exists():
            raise AssertionError(f"失败场景仍安装成功: {failure}\n{result.stdout}\n{result.stderr}")
    elif result.returncode != 0 or not installed.exists():
        raise AssertionError(f"正常场景失败: {target}/{legacy}/{requested}\n{result.stdout}\n{result.stderr}")
    assert "/escaped" not in seen, "redirect followed"
    marker = case / "codex-home/packages/standalone/auto-update-version"
    if marker.exists():
        raise AssertionError("自动更新标记未抑制")
    if not log.exists():
        raise AssertionError("没有网络记录")
    for line in log.read_text().splitlines():
        urls = json.loads(line)
        if len(urls) != 1 or not urls[0].startswith(base + "/"):
            raise AssertionError("安装器尝试访问公网: " + repr(urls))
