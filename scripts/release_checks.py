"""Release gates that stay active under python -O / PYTHONOPTIMIZE (unlike assert)."""
import re

VERSION_PATTERN = re.compile(r'\d+\.\d+\.\d+')
COMMIT_PATTERN = re.compile(r'[0-9a-f]{40}')


def require(condition, message):
    if not condition:
        raise SystemExit('Release check failed: ' + message)


def require_version(version):
    require(VERSION_PATTERN.fullmatch(version), f'invalid stable version {version!r}')


def require_commit(revision):
    require(COMMIT_PATTERN.fullmatch(revision), f'exact 40-character commit required, got {revision!r}')
