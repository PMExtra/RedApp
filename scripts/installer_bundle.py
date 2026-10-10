"""Verify and materialize the exact allowlisted installer candidate, without executing it."""
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import zipfile
from installer_manifest import ROOT, allowed_paths, inventory

SHA = re.compile(r'[0-9a-f]{40}')

def load_bundle(path, expected_sha, baseline, root=ROOT):
    allowed=allowed_paths(root)
    raw = path.read_bytes()
    if len(raw) > 8*1024*1024 or hashlib.sha256(raw).hexdigest() != expected_sha:
        raise ValueError('Bundle digest/size differs from the read-only validation job')
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        entries = archive.infolist()
        names = [x.filename for x in entries]
        if len(names) != len(set(names)) or set(names) - allowed - {'update.json'} or 'update.json' not in names:
            raise ValueError('Bundle has duplicate or unapproved paths')
        if sum(x.file_size for x in entries) > 8*1024*1024 or any((x.external_attr >> 16) & 0o170000 == 0o120000 for x in entries):
            raise ValueError('Bundle has oversized files or symbolic links')
        payload = json.loads(archive.read('update.json'))
        files = {name:archive.read(name) for name in names if name != 'update.json'}
    if payload.get('baseline') != baseline or not SHA.fullmatch(baseline):
        raise ValueError('Bundle baseline differs from the pinned main commit')
    if not files or payload.get('files') != {name:hashlib.sha256(body).hexdigest() for name,body in files.items()}:
        raise ValueError('Bundle file digests are incomplete or inconsistent')
    rows = payload.get('rows', [])
    expected = {(x['application'],x['name']):x['url'] for x in inventory(root)}
    if len(rows) != len(expected) or {(r['application'],r['name']) for r in rows} != set(expected):
        raise ValueError('Bundle does not report every declared official installer exactly once')
    expected_files=set()
    for row in rows:
        app,name = row['application'],row['name']
        source=expected[(app,name)]
        if row['url']!=source or row['status'] not in ('changed','unchanged') or any(not re.fullmatch('[0-9a-f]{64}',row[key]) for key in ('baseline_sha256','current_sha256')):
            raise ValueError('Untrusted upstream result')
        if row['status']=='changed':
            expected_files.update({f'installers/{app}/upstream/{name}',f'installers/{app}/generated/{name}',f'installers/{app}/provenance.json'})
            if hashlib.sha256(files.get(f'installers/{app}/upstream/{name}',b'')).hexdigest()!=row['current_sha256']:
                raise ValueError('Upstream digest differs from the check report')
    if set(files)!=expected_files: raise ValueError('Changed file set differs from the check report')
    return payload,files


def materialize(path, expected_sha, baseline, destination, root=ROOT):
    payload, files = load_bundle(path, expected_sha, baseline, root)
    shutil.copytree(root / 'installers', destination)
    for name, data in files.items():
        relative = Path(name).relative_to('installers')
        (destination / relative).write_bytes(data)
    return payload
