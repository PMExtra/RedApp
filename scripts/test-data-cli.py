#!/usr/bin/env python3
"""Exercise optional configuration, explicit failures and untouched old directories."""
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
binary = str(root / "bin/redapp")
env = {key: value for key, value in os.environ.items() if not key.startswith("REDAPP_")}


def invoke(args, directory, overrides=None):
    return subprocess.run([binary, *args], cwd=directory,
                          env={**env, **(overrides or {})}, capture_output=True,
                          text=True, timeout=10)


def fingerprint(directory):
    return {str(path.relative_to(directory)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in directory.rglob("*") if path.is_file()}


with tempfile.TemporaryDirectory(prefix="redapp-data-cli-") as temp:
    directory = Path(temp)
    config_path = directory / "config.json"
    data = directory / "new-data"
    config = {"schema_version": 1, "data_dir": str(data),
              "allowed_hosts": ["localhost:8080"]}
    config_path.write_text(json.dumps(config))
    result = invoke(["config", "validate", "--config", str(config_path)], directory)
    assert result.returncode == 0 and not data.exists(), result.stderr
    result = invoke(["config", "validate"], directory, {"REDAPP_DATA": str(data)})
    assert result.returncode == 0 and not data.exists(), result.stderr
    # Explicit CLI path wins over a missing environment-selected path.
    result = invoke(["config", "validate", "--config", str(config_path)], directory,
                    {"REDAPP_CONFIG": str(directory / "missing.yaml")})
    assert result.returncode == 0 and not data.exists(), result.stderr
    for args, overrides in [
        (["serve", "--config", str(directory / "missing.yaml")], {}),
        ([], {"REDAPP_CONFIG": str(directory / "missing.yaml")}),
    ]:
        result = invoke(args, directory, overrides)
        assert result.returncode == 1 and "missing.yaml" in result.stderr and not data.exists()
    result = invoke(["config", "validate", "--config", str(config_path)], directory,
                    {"REDAPP_PUBLIC_URL": "https://example.test/invalid"})
    assert result.returncode == 1 and not data.exists()
    config_path.write_text(json.dumps(config)[:-1] + ',"schema_version":1}')
    result = invoke(["config", "validate", "--config", str(config_path)], directory)
    assert result.returncode == 1 and "Duplicate" in result.stderr and not data.exists()
    # Old and unknown directories must be byte-identical after a refused startup,
    # including absence of a new instance.lock or SQLite sidecar.
    for kind in ["old-schema", "unknown"]:
        data = directory / kind
        data.mkdir()
        if kind == "old-schema":
            db = sqlite3.connect(data / "state.sqlite")
            db.executescript("CREATE TABLE schema_version(version INTEGER NOT NULL);"
                             "INSERT INTO schema_version VALUES(2);")
            db.close()
        else:
            (data / "keep-me.txt").write_text("unrelated original data")
        before = fingerprint(data)
        # Restored file-free startup must still refuse to modify old data.
        result = invoke([], directory, {"REDAPP_DATA": str(data)})
        assert result.returncode == 1 and "new empty data directory" in result.stderr, result.stderr
        assert fingerprint(data) == before and not (data / "instance.lock").exists()
    # Linux read-only filesystem: failed initialization must not fall back elsewhere.
    config["data_dir"] = "/sys/redapp-permission-test"
    config_path.write_text(json.dumps(config))
    result = invoke(["serve", "--config", str(config_path)], directory)
    assert result.returncode == 1 and ("read-only" in result.stderr.lower() or "permission denied" in result.stderr.lower())
    assert not (directory / "new-data").exists()
print("CLI optional config, explicit JSON/path failures, invalid origin/duplicate keys, unchanged old/unknown directories and permission failure passed.")
