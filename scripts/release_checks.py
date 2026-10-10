"""Release gates that stay active under python -O / PYTHONOPTIMIZE (unlike assert)."""
import re

VERSION_PATTERN = re.compile(r'\d+\.\d+\.\d+')
COMMIT_PATTERN = re.compile(r'[0-9a-f]{40}')


def require(condition, message):
    """Exit with a release-check failure message unless condition holds."""
    if not condition:
        raise SystemExit('Release check failed: ' + message)


def require_version(version):
    """Require a stable X.Y.Z version string."""
    require(VERSION_PATTERN.fullmatch(version), f'invalid stable version {version!r}')


def require_commit(revision):
    """Require an exact 40-character lowercase commit id."""
    require(COMMIT_PATTERN.fullmatch(revision), f'exact 40-character commit required, got {revision!r}')
