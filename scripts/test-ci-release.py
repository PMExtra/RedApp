#!/usr/bin/env python3
"""Exercise release source selection and corruption/identity rejection offline."""
import argparse
import importlib.util
import json
import tempfile
import unittest
import urllib.error
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
notes = module('create-github-release')


class ReleaseNotesTests(unittest.TestCase):
    """Exercise idempotence and failure boundaries without GitHub writes."""

    def setUp(self):
        self.release = dict(tag_name='v1.2.3', draft=False, prerelease=False,
                            html_url='https://github.com/PMExtra/RedApp/releases/tag/v1.2.3')
        self.ref = {'object': {'type': 'tag', 'sha': 'b' * 40}}
        self.target = {'object': {'type': 'commit', 'sha': 'a' * 40}}
        self.args = ('1.2.3', 'a' * 40, 'sha256:' + 'c' * 64,
                     'v1.2.3', 'PMExtra/RedApp', '123')

    def missing(self):
        return urllib.error.HTTPError('https://api.github.com', 404, 'missing', {}, None)

    def test_create_and_read_back(self):
        """New notes include immutable provenance and GitHub-generated changes."""
        with patch.object(notes, 'api', side_effect=[
            self.ref, self.target, self.missing(), self.release, self.release,
        ]) as api:
            self.assertEqual(notes.create(*self.args), self.release)
        writes = [call.args for call in api.call_args_list if len(call.args) == 2]
        self.assertEqual(len(writes), 1)
        path, payload = writes[0]
        self.assertEqual(path, 'releases')
        self.assertTrue(payload['generate_release_notes'])
        self.assertEqual(payload['target_commitish'], 'a' * 40)
        self.assertIn('sha256:' + 'c' * 64, payload['body'])
        self.assertIn('/actions/runs/123', payload['body'])

    def test_existing_release_preserved(self):
        """A retry never edits an existing release or its custom notes."""
        release = {**self.release, 'body': 'Maintainer-written notes'}
        with patch.object(notes, 'api', side_effect=[self.ref, self.target, release]) as api:
            self.assertEqual(notes.create(*self.args), release)
        self.assertTrue(all(len(call.args) == 1 for call in api.call_args_list))

    def test_conflicting_state_never_written(self):
        """Drafts, prereleases and changed tags require explicit intervention."""
        for release in [
            {**self.release, 'draft': True}, {**self.release, 'prerelease': True},
            {**self.release, 'tag_name': 'v1.2.4'},
        ]:
            with patch.object(notes, 'api', side_effect=[self.ref, self.target, release]) as api:
                with self.assertRaises(SystemExit):
                    notes.create(*self.args)
                self.assertTrue(all(len(call.args) == 1 for call in api.call_args_list))
        for ref, target in [
            ({'object': {'type': 'commit', 'sha': 'a' * 40}}, self.target),
            (self.ref, {'object': {'type': 'commit', 'sha': 'd' * 40}}),
        ]:
            with patch.object(notes, 'api', side_effect=[ref, target]) as api:
                with self.assertRaises(SystemExit):
                    notes.create(*self.args)
                self.assertTrue(all(len(call.args) == 1 for call in api.call_args_list))

    def test_lookup_failures_are_not_absence(self):
        """Forbidden, server and transport failures cannot trigger creation."""
        for error in [
            urllib.error.HTTPError('https://api.github.com', code, 'failure', {}, None)
            for code in (401, 403, 500)
        ] + [urllib.error.URLError('offline')]:
            with patch.object(notes, 'api', side_effect=[self.ref, self.target, error]) as api:
                with self.assertRaises(urllib.error.URLError):
                    notes.create(*self.args)
                self.assertTrue(all(len(call.args) == 1 for call in api.call_args_list))

    def test_failed_creation_or_verification_fails_job(self):
        """A write failure or missing read-back never reports successful publication."""
        for tail in [
            [urllib.error.URLError('connection lost')],
            [self.release, self.missing()],
        ]:
            with patch.object(notes, 'api', side_effect=[
                self.ref, self.target, self.missing(), *tail,
            ]):
                with self.assertRaises((urllib.error.URLError, SystemExit)):
                    notes.create(*self.args)

    def test_invalid_identity_fails_before_network(self):
        """Workflow inputs cannot silently create notes for another release."""
        for index, value in [(0, 'invalid'), (1, 'short'), (2, ''), (3, 'v1.2.4'),
                             (4, 'other/repo'), (5, 'bad')]:
            args = list(self.args)
            args[index] = value
            with patch.object(notes, 'api') as api:
                with self.assertRaises(SystemExit):
                    notes.create(*args)
                api.assert_not_called()

    def test_workflow_privilege_and_dependency(self):
        """Only the notes job has contents write, after the full publishing job."""
        workflow = (ROOT.parent / '.github/workflows/publish.yml').read_text()
        publish, release = workflow.split('\n  release:\n')
        self.assertNotIn('contents: write', publish)
        self.assertIn('      packages: write', publish)
        self.assertIn('    needs: publish\n', release)
        self.assertIn('    permissions:\n      contents: write\n', release)
        self.assertNotIn('always()', release)
        self.assertNotIn('continue-on-error', workflow)
        self.assertIn('IMAGE_DIGEST: ${{ needs.publish.outputs.digest }}', release)
        self.assertIn('digest: ${{ steps.candidate.outputs.digest }}', publish)
        self.assertIn('run: python3 scripts/create-github-release.py', release)


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
