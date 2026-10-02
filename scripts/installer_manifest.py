"""Read the compiled application manifest without executing descriptor content.

Daily maintenance may update originals/generated/provenance only. Its reviewed
validators and filenames are fixed code; adding a protocol needs code review.
"""
import json
from pathlib import Path
import re
import urllib.parse

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = Path('internal/apps/builtin/manifest.json')
VALIDATORS = {'codex': 'test-installers.py', 'claude-code': 'test-claude-installers.py'}
SCRIPT_NAMES = frozenset(('install.sh', 'install.ps1'))
APP_ID = re.compile(r'[a-z0-9]+(?:-[a-z0-9]+)*/[a-z0-9]+(?:-[a-z0-9]+)*')


def applications(root=ROOT):
    manifest = json.loads((root / MANIFEST).read_text())
    if manifest.get('schema_version') != 1:
        raise ValueError('Unsupported builtin application manifest schema')
    apps = manifest.get('applications')
    if not isinstance(apps, list) or not apps:
        raise ValueError('Builtin application manifest is empty')
    seen = set()
    for app in apps:
        app_id = app.get('id', '')
        if not APP_ID.fullmatch(app_id) or any(len(part) > 63 for part in app_id.split('/')) or app_id.split('/')[0] in ('admin', 'api', 'assets', 'health') or app_id in seen:
            raise ValueError('Invalid or duplicate canonical application identity')
        seen.add(app_id)
        if app.get('installer_validator') not in VALIDATORS:
            raise ValueError('Installer validator must name a reviewed implementation')
        scripts = app.get('installers')
        if not isinstance(scripts, list) or not scripts:
            raise ValueError('Application installer list is empty')
        names = set()
        for script in scripts:
            name, source = script.get('file'), script.get('source', '')
            if name not in SCRIPT_NAMES or name in names:
                raise ValueError('Unsupported or duplicate installer filename')
            names.add(name)
            shells = ('sh', 'bash') if name == 'install.sh' else ('powershell',)
            if script.get('shell') not in shells:
                raise ValueError('Installer interpreter does not match its reviewed file type')
            url = urllib.parse.urlsplit(source)
            if url.scheme != 'https' or not url.hostname or url.username is not None or url.password is not None or url.fragment or url.query:
                raise ValueError('Official installer source must be a reviewed HTTPS URL')
    return apps


def inventory(root=ROOT):
    return [dict(application=app['id'], name=script['file'], url=script['source'])
            for app in applications(root) for script in app['installers']]


def allowed_paths(root=ROOT):
    paths = set()
    for app in applications(root):
        base = 'installers/' + app['id']
        paths.add(base + '/provenance.json')
        paths.update(f'{base}/{kind}/{script["file"]}' for script in app['installers']
                     for kind in ('upstream', 'generated'))
    return paths
