#!/usr/bin/env python3
"""Exercise release source selection and corruption/identity rejection offline."""
import argparse
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parent


def module(name):
    """Import a script from this directory by file name."""
    spec = importlib.util.spec_from_file_location(name, ROOT / (name + '.py'))
    loaded = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(loaded)
    return loaded


selection = module('select-release-ci')
artifact = module('ci-image-artifact')


class ReleaseTests(unittest.TestCase):
    """Offline release source selection and artifact identity gates."""

    def test_exact_main_success_only(self):
        """Only a successful main push CI run of the exact commit is selected."""
        base = dict(id=1, head_sha='a' * 40, head_branch='main', event='push',
                    status='completed', conclusion='success', path='.github/workflows/ci.yml')
        for override in [
            dict(head_sha='b' * 40), dict(head_branch='feature'), dict(event='pull_request'),
            dict(conclusion='failure'), dict(status='in_progress'), dict(path='other.yml'),
        ]:
            with self.assertRaises(SystemExit):
                selection.select([{**base, **override}], 'a' * 40)
        self.assertEqual(selection.select([base, {**base, 'id': 2}], 'a' * 40)['id'], 2)

    def test_expired_missing_and_duplicate_artifacts(self):
        """Missing, expired or duplicate runtime artifacts are rejected."""
        sha = 'a' * 40
        valid = [dict(name=f'runtime-{sha}-{arch}', expired=False) for arch in ('amd64', 'arm64')]
        selection.validate_artifacts(valid, sha)
        for invalid in [valid[:1], [valid[0], {**valid[1], 'expired': True}], valid + valid[:1]]:
            with self.assertRaises(SystemExit):
                selection.validate_artifacts(invalid, sha)

    def test_corrupt_or_wrong_commit_never_loads(self):
        """A corrupt archive or wrong commit identity is rejected before docker load."""
        with tempfile.TemporaryDirectory() as directory:
            args = argparse.Namespace(
                mode='verify', directory=Path(directory), version='0.7.15',
                revision='a' * 40, arch='amd64',
            )
            args.directory.joinpath('image.tar').write_bytes(b'corrupt')
            expected = artifact.checked_metadata(
                args.directory, args.version, args.revision, args.arch
            )
            for metadata in [
                {**expected, 'revision': 'b' * 40, 'sha256': '0' * 64},
                {**expected, 'sha256': '0' * 64},
            ]:
                args.directory.joinpath('metadata.json').write_text(json.dumps(metadata))
                with patch.object(artifact.subprocess, 'run') as load:
                    with self.assertRaises(SystemExit):
                        artifact.run(args)
                    load.assert_not_called()

    def test_version_and_identity_gates(self):
        """Only stable versions, exact commits and supported architectures pass."""
        for version in ('0.7.15', '1.0.0', '12.34.56'):
            artifact.checked_metadata(None, version, 'a' * 40, 'arm64')
        for version, revision, arch in [
            ('v1.0.0', 'a' * 40, 'amd64'), ('1.0', 'a' * 40, 'amd64'),
            ('1.0.0-rc1', 'a' * 40, 'amd64'), ('1.0.0', 'a' * 39, 'amd64'),
            ('1.0.0', 'a' * 40, '386'),
        ]:
            with self.assertRaises(SystemExit):
                artifact.checked_metadata(None, version, revision, arch)


if __name__ == '__main__':
    unittest.main()
