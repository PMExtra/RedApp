#!/usr/bin/env python3
"""离线 CLI：无害二进制桩、真实 Bash/curl/wget、受控本地 HTTP；不运行上游二进制。"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


from installer_test_support import InstallerServer


def test_shell(directory, application):
    for dependency in ['bash', 'curl', 'wget', 'jq']:
        assert shutil.which(dependency), f'{dependency} is required for installer contracts'
    fixture = InstallerServer(application, 'claude-code')
    fixture.__enter__()
    config, seen, base = fixture.config, fixture.seen, fixture.base
    script = (directory / 'install.sh').read_text().replace('@REDAPP_BASE_URL@', base)
    try:
        with tempfile.TemporaryDirectory(prefix='claude-installer-test-') as temp:
            root = Path(temp)
            def run(**options):
                case(root, script, config, seen, base, **options)

            # Platform detection is independent of target parsing and launcher lifecycle.
            for osname, arch, musl, rosetta, platform in [
                ('Linux', 'x86_64', False, False, 'linux-x64'),
                ('Linux', 'aarch64', False, False, 'linux-arm64'),
                ('Linux', 'x86_64', True, False, 'linux-x64-musl'),
                ('Linux', 'aarch64', True, False, 'linux-arm64-musl'),
                ('Darwin', 'x86_64', False, False, 'darwin-x64'),
                ('Darwin', 'arm64', False, False, 'darwin-arm64'),
                ('Darwin', 'x86_64', False, True, 'darwin-arm64'),
            ]:
                run(osname=osname, arch=arch, musl=musl, rosetta=rosetta, platform=platform)
            print('Platform selection: PASS (Linux glibc/musl, Darwin and Rosetta simulated)')

            # Default target is covered above; explicit channels and versions here.
            for requested in ['latest', 'stable', '2.1.285']:
                run(requested=requested)
            for requested in ['../escape', '2.1.285/../../escape']:
                run(requested=requested, failure='target')
            print('Target parsing: PASS (default, latest, stable, explicit version, unsafe input)')

            # Representative lifecycle includes fresh-terminal execution, reinstall,
            # rejected upgrade and successful atomic launcher replacement.
            run(use_jq=True, lifecycle=True)
            run(downloader='wget', lifecycle=True)
            for downloader in ['curl','wget']:
                run(downloader=downloader,redirect=True)
            print('Launcher lifecycle: PASS (curl/jq and wget/fallback)')

            # Exercise every actual downloader/parser pair at the failure boundaries.
            for downloader in ['curl', 'wget']:
                for use_jq in [False, True]:
                    for failure in ['channel', 'manifest', 'hash', 'download']:
                        run(downloader=downloader, use_jq=use_jq, requested='latest', failure=failure)
            for takeover in ['linked-directory', 'existing-launcher', 'existing-version', 'official-installation']:
                run(takeover=takeover)
            print('Failure protection: PASS (jq/fallback × curl/wget; malformed data, hash, download; official takeover and repair)')
    finally:
        fixture.__exit__()
    print('Claude Shell contracts: PASS (no official binary executed)')


def case(root, script, config, seen, base, *, osname='Linux', arch='x86_64',
         musl=False, rosetta=False, platform='linux-x64', requested='', failure='',
         use_jq=False, downloader='curl', lifecycle=False, takeover='', redirect=False):
    work=root/str(len(list(root.iterdir()))); tools=work/'tools'; home=work/'user home'; tools.mkdir(parents=True); home.mkdir()
    def tool(name, content): p=tools/name;p.write_text(content);p.chmod(0o755)
    for name in ['id','mkdir','mktemp','rm','chmod','cp','ln','mv','grep','tr','sed','cut','head','dirname','sha256sum','stty','cat']:
        path=shutil.which(name); assert path,name; (tools/name).symlink_to(path)
    if use_jq: (tools/'jq').symlink_to(shutil.which('jq'))
    executable=shutil.which(downloader);assert executable
    download_log=work/'downloader.log'
    tool(downloader, '#!/usr/bin/python3\nimport sys,subprocess\nurls=[x for x in sys.argv[1:] if x.startswith(("http://","https://"))]\nassert len(urls)==1 and urls[0].startswith('+repr(base+'/')+')\nwith open('+repr(str(download_log))+', "a") as log: log.write('+repr(downloader+'\n')+')\nsys.exit(subprocess.call(['+repr(executable)+']+sys.argv[1:]))\n')
    assert not (tools/('curl' if downloader=='wget' else 'wget')).exists()
    tool('uname', f'#!/bin/sh\ncase "$1" in -s) echo {osname};; -m) echo {arch};; *) exit 1;; esac\n')
    tool('sysctl', '#!/bin/sh\necho '+('1' if rosetta else '0')+'\n')
    tool('ldd', '#!/bin/sh\necho '+('musl' if musl else 'glibc')+'\n')
    tool('shasum', '#!/bin/sh\nshift 2\nexec sha256sum "$@"\n')
    # Exercise the original optional compression fallback: unsigned compressed metadata is unavailable.
    tool('zstd','#!/bin/sh\nexit 77\n')
    config.clear();config.update(redirect=redirect,version='2.1.285',platform=platform,failure=failure,body=b'#!/bin/sh\nprintf "%s\\n" "$DISABLE_UPDATES" "$@"\nexit 23\n')
    env={'PATH':str(tools),'HOME':str(home),'SUDO_USER':'','DISABLE_UPDATES':'0','LC_ALL':'C'}
    destination=home/'.local/share/claude/versions/2.1.285';launcher=home/'.local/bin/claude'
    sentinel=work/'sentinel';sentinel.write_text('do not change')
    if takeover=='linked-directory':
        linked=work/'linked';linked.mkdir();(home/'.local').symlink_to(linked)
    if takeover=='existing-launcher': launcher.parent.mkdir(parents=True);launcher.write_text('existing unrelated launcher')
    if takeover=='existing-version': destination.parent.mkdir(parents=True);destination.write_text('existing invalid version')
    if takeover=='official-installation':
        destination.parent.mkdir(parents=True);destination.write_text('old official binary')
        launcher.parent.mkdir(parents=True);launcher.symlink_to(destination)
    original_launcher=launcher.read_bytes() if launcher.is_file() else None
    original_version=destination.read_bytes() if destination.is_file() else None
    seen.clear(); command=[shutil.which('bash'),'-s','--']+([requested] if requested else [])
    r=subprocess.run(command,input=script,text=True,capture_output=True,env=env,timeout=20)
    if failure:
        assert r.returncode!=0,(failure,r.stdout,r.stderr)
        assert 'Installation complete!' not in r.stdout
        if original_launcher is not None: assert launcher.read_bytes()==original_launcher
        else: assert not launcher.exists()
        if original_version is not None: assert destination.read_bytes()==original_version
        else: assert not destination.exists()
        assert sentinel.read_text()=='do not change'
        if failure=='target': assert not seen, seen
        if failure=='hash': assert 'Checksum verification failed' in r.stderr, r.stderr
        if failure == 'channel':
            assert seen == ['/latest'], seen
        if failure == 'manifest':
            assert not any(path.endswith('/claude') for path in seen), seen
    else:
        assert r.returncode==0,(r.stdout,r.stderr)
        assert destination.read_bytes()==config['body'] and destination.stat().st_mode&0o111
        assert 'Add "$HOME/.local/bin" to PATH' in r.stdout
        assert ('/latest' in seen)==(requested in ['', 'latest'])
        assert ('/stable' in seen)==(requested=='stable')
        assert f'/2.1.285/{platform}/claude' in seen, seen
    if lifecycle:
        run=subprocess.run([str(launcher),'argument with spaces','semi;colon','--flag'],env=env,capture_output=True,text=True,timeout=5)
        assert run.returncode==23 and run.stdout.splitlines()==['1','argument with spaces','semi;colon','--flag'],run
        # A fresh process receives the same update policy; no parent/global environment change is needed.
        run=subprocess.run([str(launcher),'--version'],env={**env,'DISABLE_UPDATES':'false'},capture_output=True,text=True,timeout=5)
        assert run.stdout.splitlines()==['1','--version'] and env['DISABLE_UPDATES']=='0'
        rerun=subprocess.run(command,input=script,text=True,capture_output=True,env=env,timeout=20)
        assert rerun.returncode==0
        old_launcher=launcher.read_bytes();config['version']='2.1.286';config['failure']='hash'
        bad=subprocess.run([shutil.which('bash'),'-s','--','2.1.286'],input=script,text=True,capture_output=True,env=env,timeout=20)
        assert bad.returncode!=0 and launcher.read_bytes()==old_launcher and not (destination.parent/'2.1.286').exists()
        config['failure']=''
        upgraded=subprocess.run([shutil.which('bash'),'-s','--','stable'],input=script,text=True,capture_output=True,env=env,timeout=20)
        assert upgraded.returncode==0,(upgraded.stdout,upgraded.stderr)
        assert destination.exists() and '2.1.286' in launcher.read_text()
    assert not list((home/'.claude/downloads').glob('redapp.*')),'temporary download directory leaked'
    if failure != 'target':
        assert set(download_log.read_text().splitlines()) == {downloader}

