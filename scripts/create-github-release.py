#!/usr/bin/env python3
"""Create release notes after image promotion; never overwrite existing releases."""
import json
import os
import re
import urllib.error
import urllib.request
from pathlib import Path

from release_checks import require, require_commit, require_version


def api(path, payload=None):
    """Call the repository API, propagating authentication and transport failures."""
    request = urllib.request.Request(
        f'https://api.github.com/repos/{os.environ["GITHUB_REPOSITORY"]}/{path}',
        data=None if payload is None else json.dumps(payload).encode(),
        headers={
            'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
            'Accept': 'application/vnd.github+json',
            'Content-Type': 'application/json',
            'X-GitHub-Api-Version': '2022-11-28',
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def existing_release(tag):
    """Only a missing release permits creation; other failures must stop the job."""
    try:
        return api('releases/tags/' + tag)
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        return None


def validate_release(release, tag):
    """Do not silently publish a draft or accept a conflicting release state."""
    require(release['tag_name'] == tag, 'Release tag mismatch')
    require(release['draft'] is False, 'Existing release is a draft; review it manually')
    require(release['prerelease'] is False, 'Existing release is a prerelease; review it manually')


def create(version, sha, digest, tag, repository, run_id):
    """Bind notes to the verified image and exact annotated tag before writing."""
    require_version(version)
    require_commit(sha)
    require(tag == 'v' + version, 'Tag does not match VERSION')
    require(repository == 'PMExtra/RedApp', 'Unexpected release repository')
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', digest), 'Invalid published image digest')
    require(re.fullmatch(r'[0-9]+', run_id), 'Invalid publishing run id')
    ref = api('git/ref/tags/' + tag)['object']
    require(ref['type'] == 'tag', 'Release requires an annotated tag')
    target = api('git/tags/' + ref['sha'])['object']
    require(target['type'] == 'commit' and target['sha'] == sha, 'Release tag moved')
    release = existing_release(tag)
    if release is not None:
        validate_release(release, tag)
        print('Existing published release preserved: ' + release['html_url'])
        return release
    body = (
        '## Container image\n\n'
        f'`ghcr.io/pmextra/redapp:{version}` (Linux amd64 and arm64).\n\n'
        f'Immutable image: `ghcr.io/pmextra/redapp@{digest}`\n\n'
        f'Source commit: [{sha}](https://github.com/{repository}/commit/{sha})\n\n'
        f'[Verified image publication](https://github.com/{repository}/actions/runs/{run_id}). '
        'The exact CI-tested images were promoted without rebuilding. '
        'No standalone binary release assets are published.\n'
    )
    api('releases', {
        'tag_name': tag, 'target_commitish': sha, 'name': 'RedApp ' + version,
        'body': body, 'draft': False, 'prerelease': False, 'generate_release_notes': True,
    })
    release = existing_release(tag)
    require(release is not None, 'Created release could not be read back')
    validate_release(release, tag)
    print('Published release: ' + release['html_url'])
    return release


if __name__ == '__main__':
    result = create(
        Path('VERSION').read_text().strip(), os.environ['GITHUB_SHA'],
        os.environ['IMAGE_DIGEST'], os.environ['GITHUB_REF_NAME'],
        os.environ['GITHUB_REPOSITORY'], os.environ['GITHUB_RUN_ID'],
    )
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as summary:
        summary.write('GitHub Release: ' + result['html_url'] + '\n')
