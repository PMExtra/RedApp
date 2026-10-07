#!/usr/bin/env python3
"""Record/verify a native runtime image saved by the exact trusted CI run."""
import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path


def checked_metadata(directory, version, revision, arch):
    assert re.fullmatch(r'0\.\d+\.\d+', version), 'Invalid stable version'
    assert re.fullmatch(r'[0-9a-f]{40}', revision), 'Exact commit required'
    assert arch in ('amd64', 'arm64'), 'Unsupported architecture'
    return {'version': version, 'revision': revision, 'architecture': arch,
            'image_tag': f'redapp:ci-{revision}-{arch}'}


def checksum(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def run(args):
    expected = checked_metadata(args.directory, args.version, args.revision, args.arch)
    archive = args.directory / 'image.tar'
    if args.mode == 'record':
        subprocess.run(['docker', 'save', '-o', str(archive), expected['image_tag']], check=True)
        metadata = {**expected, 'sha256': checksum(archive)}
        (args.directory / 'metadata.json').write_text(json.dumps(metadata, sort_keys=True) + '\n')
    else:
        metadata = json.loads((args.directory / 'metadata.json').read_text())
        assert all(metadata.get(k) == v for k, v in expected.items()), 'Artifact identity mismatch'
        assert metadata['sha256'] == checksum(archive), 'Artifact checksum mismatch'
        subprocess.run(['docker', 'load', '-i', str(archive)], check=True)
    info = json.loads(subprocess.check_output(['docker', 'image', 'inspect', expected['image_tag']]))[0]
    assert info['Os'] == 'linux' and info['Architecture'] == args.arch, 'Image platform mismatch'
    labels = info['Config']['Labels']
    assert labels['org.opencontainers.image.version'] == args.version, 'Image version mismatch'
    assert labels['org.opencontainers.image.revision'] == args.revision, 'Image revision mismatch'
    print(json.dumps(expected))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['record', 'verify'])
    parser.add_argument('directory', type=Path)
    parser.add_argument('version')
    parser.add_argument('revision')
    parser.add_argument('arch', choices=['amd64', 'arm64'])
    run(parser.parse_args())
