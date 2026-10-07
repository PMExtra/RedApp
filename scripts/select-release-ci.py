#!/usr/bin/env python3
"""Select exact main CI run and require its two non-expired image artifacts."""
import json
import os
import re
import urllib.request
from pathlib import Path


def select(runs, sha):
    candidates = [r for r in runs if r['head_sha'] == sha and r['head_branch'] == 'main'
                  and r['event'] == 'push' and r['status'] == 'completed'
                  and r['conclusion'] == 'success' and r['path'] == '.github/workflows/ci.yml']
    assert candidates, 'Exact commit must pass complete main CI before publishing'
    return max(candidates, key=lambda r: r['id'])


def validate_artifacts(items, sha):
    for arch in ['amd64', 'arm64']:
        name = f'runtime-{sha}-{arch}'
        matches = [a for a in items if a['name'] == name and not a['expired']]
        assert len(matches) == 1, f'Missing/expired/ambiguous {name}; rerun exact main CI, never use another commit'


def get(path):
    request = urllib.request.Request(f'https://api.github.com/repos/{os.environ["GITHUB_REPOSITORY"]}/{path}',
                                    headers={'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
                                             'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28'})
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


if __name__ == '__main__':
    version = Path('VERSION').read_text().strip()
    sha = os.environ['GITHUB_SHA']
    assert re.fullmatch(r'0\.\d+\.\d+', version) and re.fullmatch(r'[0-9a-f]{40}', sha)
    assert os.environ['GITHUB_REF_NAME'] == 'v' + version, 'Tag does not match VERSION'
    assert get('git/ref/heads/main')['object']['sha'] == sha, 'Release must still be current main'
    run = select(get(f'actions/workflows/ci.yml/runs?head_sha={sha}&event=push&per_page=100')['workflow_runs'], sha)
    validate_artifacts(get(f'actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts'], sha)
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f'run_id={run["id"]}\nversion={version}\nseries={".".join(version.split(".")[:2])}\n')
