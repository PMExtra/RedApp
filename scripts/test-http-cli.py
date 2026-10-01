#!/usr/bin/env python3
"""通过真实可执行文件验证启动、HTTP 管理 API、健康检查和停止；不输出密码。"""
import http.cookiejar
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parents[1]
version_output = subprocess.check_output([str(root / "bin/redapp"), "version"], text=True)
assert re.fullmatch(r"RedApp [^\s]+ \(commit [^\s]+\)\n", version_output), "版本信息无效"
with tempfile.TemporaryDirectory(prefix="redapp-http-cli-") as temp:
    directory = Path(temp)
    with socket.socket() as socket_probe:
        socket_probe.bind(("127.0.0.1", 0))
        port = socket_probe.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    env = os.environ.copy()
    env.update(REDAPP_LISTEN=f"127.0.0.1:{port}", REDAPP_PUBLIC_URL=base, REDAPP_DATA=str(directory / "data"))
    log = (directory / "server.log").open("w+")
    process = subprocess.Popen([str(root / "bin/redapp")], env=env, stdout=log, stderr=log)
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise AssertionError("服务提前退出，日志保留在临时目录")
            try:
                with urllib.request.urlopen(base + "/health/ready", timeout=1) as response:
                    assert response.status == 200
                break
            except (OSError, urllib.error.URLError):
                time.sleep(0.05)
        else:
            raise AssertionError("服务未就绪")
        subprocess.run([str(root / "bin/redapp"), "healthcheck"], env=env, check=True)
        log.flush()
        log.seek(0)
        text = log.read()
        password_match = re.search(r"Initial admin password: ([0-9a-f]+)", text)
        assert password_match, "未输出初始密码"
        password = password_match.group(1)
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        login = urllib.request.Request(base + "/admin/api/login", data=json.dumps({"password": password}).encode(), headers={"Content-Type": "application/json"})
        with opener.open(login) as response:
            csrf = json.load(response)["csrf"]
        with opener.open(base + "/admin/api/status") as response:
            status = json.load(response)
            assert status["name"] == "RedApp" and status["disk"]["free_bytes"] > 0
        settings = urllib.request.Request(base + "/admin/api/settings", data=b'{"latest_ttl_seconds":120}', headers={"Content-Type": "application/json", "X-CSRF-Token": csrf})
        with opener.open(settings) as response:
            assert json.load(response)["ok"]
        with opener.open(base + "/install.sh") as response:
            script = response.read().decode()
            assert base in script and "https://github.com" not in script
        process.terminate()
        assert process.wait(timeout=20) == 0, "正常停止失败"
        log.close()
        log = (directory / "restart.log").open("w+")
        env["REDAPP_PUBLIC_URL"] = ""
        process = subprocess.Popen([str(root / "bin/redapp")], env=env, stdout=log, stderr=log)
        for _ in range(100):
            try:
                with urllib.request.urlopen(base + "/health/ready", timeout=1):
                    break
            except OSError:
                time.sleep(0.05)
        subprocess.run([str(root / "bin/redapp"), "healthcheck"], env=env, check=True)
        with urllib.request.urlopen(base + "/install.sh") as response:
            assert base in response.read().decode(), "自动 origin 未写入安装器"
        process.terminate()
        assert process.wait(timeout=20) == 0
        log.flush()
        log.seek(0)
        assert "Initial admin password" not in log.read(), "重启再次输出密码"
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        log.close()
print("真实 CLI：启动、健康检查、登录、CSRF 设置、状态 API、企业脚本、SIGTERM、重启密码不重复通过。")
