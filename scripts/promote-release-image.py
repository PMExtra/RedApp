#!/usr/bin/env python3
"""Promote a verified immutable index without rebuilding or overwriting releases."""
import json
import os
import re
import subprocess
import urllib.request
from pathlib import Path

from release_checks import require, require_commit, require_version


def existing_digest(image):
    """Return the manifest digest of an existing image tag, or None if it is absent."""
    result = subprocess.run(
        ['docker', 'buildx', 'imagetools', 'inspect', image, '--format', '{{.Manifest.Digest}}'],
        capture_output=True, text=True,
    )
    if result.returncode == 0:
        return result.stdout.strip()
    require('not found' in result.stderr.lower(), result.stderr)
    return None


if __name__ == '__main__':
    version = Path('VERSION').read_text().strip()
    sha, digest = os.environ['GITHUB_SHA'], os.environ['IMAGE_DIGEST']
    require_version(version)
    require_commit(sha)
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', digest), f'invalid image digest {digest!r}')
    require(os.environ['GITHUB_REF_NAME'] == 'v' + version, 'Tag does not match VERSION')
    url = f'https://api.github.com/repos/{os.environ["GITHUB_REPOSITORY"]}/git/ref/heads/main'
    request = urllib.request.Request(url, headers={
        'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
        'Accept': 'application/vnd.github+json',
    })
    with urllib.request.urlopen(request, timeout=30) as response:
        require(
            json.load(response)['object']['sha'] == sha,
            'main advanced; refuse stale rolling-tag promotion',
        )
    repository = 'ghcr.io/pmextra/redapp'
    tags = ['v' + version, version, '.'.join(version.split('.')[:2]), 'latest']
    pending = []
    for tag in tags[:2]:
        current = existing_digest(repository + ':' + tag)
        require(current is None or current == digest, f'Refuse to overwrite existing release {tag}')
        if current is None:
            pending.append(tag)
    pending += tags[2:]
    command = ['docker', 'buildx', 'imagetools', 'create']
    for tag in pending:
        command += ['--tag', repository + ':' + tag]
    subprocess.run(command + [repository + '@' + digest], check=True)
    for tag in tags:
        require(existing_digest(repository + ':' + tag) == digest, f'Promotion mismatch: {tag}')
    print('Verified all release and rolling tags: ' + digest)
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as summary:
        summary.write(f'Image: `{repository}@{digest}`\n')
