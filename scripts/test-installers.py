#!/usr/bin/env python3
"""Manifest-driven installer contracts; PowerShell is parsed/executed only on Windows."""
import argparse
import os
from pathlib import Path
import tempfile
from installer_manifest import ROOT, applications
from installer_bundle import materialize


def test(apps, installer_root, platform, directory=None, shell=None):
    if platform == 'windows':
        if os.name != 'nt':
            raise SystemExit('PowerShell validation requires native Windows; no Linux fallback or skip.')
        from installer_tests_windows import test_windows
        test_windows(apps, installer_root, directory, shell)
    else:
        if os.name == 'nt': raise SystemExit('Shell contracts require a Unix host.')
        from installer_tests_codex import test_shell as codex
        from installer_tests_claude import test_shell as claude
        validators = {'codex': codex, 'claude-code': claude}
        for app in apps:
            validators[app['installer_validator']](directory or installer_root / app['id'] / 'generated', app['id'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', choices=['shell', 'windows'], required=True)
    parser.add_argument('--application', choices=[a['id'] for a in applications()])
    parser.add_argument('--directory', type=Path, help='Exact generated directory; requires --application')
    parser.add_argument('--shell', choices=['pwsh', 'powershell.exe'], help='Windows engine; default tests both')
    parser.add_argument('--bundle', type=Path, help='Exact candidate ZIP from the Linux validation job')
    parser.add_argument('--sha256')
    parser.add_argument('--baseline')
    args = parser.parse_args()
    if args.directory and not args.application: parser.error('--directory requires --application')
    if args.shell and args.platform != 'windows': parser.error('--shell is Windows-only')
    if args.bundle and (not args.sha256 or not args.baseline or args.directory or args.application):
        parser.error('--bundle requires --sha256 and --baseline, and validates every application')
    apps = [a for a in applications() if not args.application or a['id'] == args.application]
    if args.bundle:
        import subprocess
        baseline = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        if baseline != args.baseline: raise ValueError('Candidate baseline differs from the trusted harness checkout')
        with tempfile.TemporaryDirectory(prefix='redapp-candidate-') as tmp:
            root = Path(tmp) / 'installers'
            materialize(args.bundle, args.sha256, args.baseline, root)
            print(f'Validating candidate bundle {args.sha256} against baseline {baseline}', flush=True)
            test(apps, root, args.platform, shell=args.shell)
    else:
        test(apps, ROOT / 'installers', args.platform, args.directory, args.shell)


if __name__ == '__main__': main()
