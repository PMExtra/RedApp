#!/usr/bin/env python3
"""CLI 离线安装器回归：真实 shell 执行，四种 shell 平台分支模拟，禁止任何公网请求。"""
import argparse
import gzip
import hashlib
import http.server
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import threading

def sha(data):
    return hashlib.sha256(data).hexdigest()

def archive(target, legacy):
    files = {"bin/codex": b"#!/bin/sh\necho 'codex-cli 0.159.2'\n",
             "bin/codex-code-mode-host": b"#!/bin/sh\nexit 0\n",
             "codex-path/rg": b"#!/bin/sh\nexit 0\n", "codex-resources/bwrap": b"#!/bin/sh\nexit 0\n",
             "codex-package.json": b"{}"}
    if legacy:
        files = {f"package/vendor/{target}/codex/codex": files["bin/codex"],
                 f"package/vendor/{target}/path/rg": files["codex-path/rg"],
                 f"package/vendor/{target}/codex-resources/bwrap": files["codex-resources/bwrap"]}
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w:gz") as tar:
        for name, data in files.items():
            entry = tarfile.TarInfo(name)
            entry.size = len(data)
            entry.mode = 0o755
            tar.addfile(entry, io.BytesIO(data))
    return stream.getvalue()

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, default=Path(__file__).resolve().parents[1] / "installers/codex/generated")
    args = parser.parse_args()
    config = {}
    seen = []
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass
        def do_GET(self):
            seen.append(self.path)
            if config["failure"] == "redirect":
                self.send_response(302)
                self.send_header("Location", "https://github.com/openai/codex")
                self.end_headers()
                return
            if self.path.endswith("latest") or self.path.endswith("release.json"):
                if config["failure"] == "metadata":
                    data = b"not json"
                else:
                    data = json.dumps(config["metadata"]).encode()
            else:
                data = config["files"].get(self.path.rsplit("/", 1)[-1])
                if data is None:
                    self.send_error(404)
                    return
            self.send_response(200)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base = f"http://127.0.0.1:{server.server_port}"
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
            for osname, arch, target, npm in variants:
                for legacy in [False, True]:
                    for requested in ["latest", "0.159.2"]:
                        run_case(root, args.directory, base, curl, config, seen, osname, arch, target, npm, legacy, requested, "")
                        count += 1
            for failure in ["metadata", "tag", "missing-digest", "missing-package", "manifest-hash", "manifest-entry", "archive-hash", "truncated", "redirect"]:
                run_case(root, args.directory, base, curl, config, seen, *variants[0], False, "latest", failure)
                count += 1
    finally:
        server.shutdown()
        server.server_close()
        thread.join()
    # PowerShell branch execution requires Windows; static audit is deliberately reported separately.
    ps = (args.directory / "install.ps1").read_text()
    if "https://" in ps or "http://" in ps or "Invoke-RestMethod" in ps:
        raise AssertionError("PowerShell 公网入口残留")
    print(f"shell {count} 个离线场景通过；PowerShell 静态出口检查通过，Windows 运行未验证。")

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
    body = archive(target, legacy)
    name = f"codex-npm-{npm}-0.159.2.tgz" if legacy else f"codex-package-{target}.tar.gz"
    manifest = f"{sha(body)}  {name}\n".encode()
    if failure == "manifest-entry":
        manifest = f"{sha(body)}  unrelated.tar.gz\n".encode()
    metadata = {"tag_name": "rust-v0.159.2", "assets": [
        {"name": name, "digest": "sha256:" + sha(body), "browser_download_url": "https://attacker.example/injected"}]}
    if not legacy:
        metadata["assets"].append({"name": "codex-package_SHA256SUMS", "digest": "sha256:" + sha(manifest), "browser_download_url": base + "/manifest"})
    files = {name: body, "codex-package_SHA256SUMS": manifest}
    if failure == "tag":
        # Use a fixed requested version so a mismatching response must fail.
        requested = "0.159.2"
        metadata["tag_name"] = "rust-v0.159.3"
    if failure == "missing-digest":
        metadata["assets"][0]["digest"] = "invalid"
    if failure == "missing-package":
        metadata["assets"] = metadata["assets"][1:]
    if failure == "manifest-hash":
        files["codex-package_SHA256SUMS"] += b"corruption"
    if failure == "archive-hash":
        files[name] += b"corruption"
    if failure == "truncated":
        files[name] = body[:len(body) // 2]
    config.clear()
    config.update(metadata=metadata, files=files, failure=failure)
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
    marker = case / "codex-home/packages/standalone/auto-update-version"
    if marker.exists():
        raise AssertionError("自动更新标记未抑制")
    if not log.exists():
        raise AssertionError("没有网络记录")
    for line in log.read_text().splitlines():
        urls = json.loads(line)
        if len(urls) != 1 or not urls[0].startswith(base + "/"):
            raise AssertionError("安装器尝试访问公网: " + repr(urls))

if __name__ == "__main__":
    main()
