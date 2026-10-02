#!/usr/bin/env python3
"""离线 CLI：无害二进制桩、真实 Bash/curl、受控本地 HTTP；不运行上游二进制。"""
import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, default=Path(__file__).resolve().parents[1] / 'installers/anthropic/claude-code/generated')
    args = parser.parse_args()
    seen, config = [], {}
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_): pass
        def do_GET(self):
            if not self.path.startswith('/anthropic/claude-code/'):
                seen.append(self.path);self.send_error(404);return
            path=self.path[len('/anthropic/claude-code'):]
            seen.append(path)
            if config.get('failure') == 'redirect':
                self.send_response(302); self.send_header('Location', '/escaped'); self.end_headers(); return
            version = config['version']
            if path in ['/latest', '/stable']:
                body = version.encode() if config.get('failure') != 'channel' else b'2.1.285/../../escape'
            elif path == f'/{version}/manifest.json':
                body = json.dumps({'version': version, 'platforms': {config['platform']: {'binary': 'claude', 'checksum': hashlib.sha256(config['body']).hexdigest(), 'size': len(config['body'])}}}).encode()
                if config.get('failure') == 'manifest': body = b'<html>Error</html>'
            elif path == f'/{version}/{config["platform"]}/claude':
                body = config['body'] + (b'tampered' if config.get('failure') == 'hash' else b'')
                if config.get('failure') == 'download': self.send_error(503); return
            else:
                self.send_error(404); return
            self.send_response(200); self.send_header('Content-Length', str(len(body))); self.end_headers(); self.wfile.write(body)
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
    base = f'http://127.0.0.1:{server.server_port}/anthropic/claude-code'
    script = (args.directory / 'install.sh').read_text().replace('@REDAPP_BASE_URL@', base)
    count = 0
    try:
        with tempfile.TemporaryDirectory(prefix='claude-installer-test-') as temp:
            root = Path(temp)
            for osname, arch, musl, rosetta in [('Linux','x86_64',False,False),('Linux','aarch64',False,False),('Linux','x86_64',True,False),('Linux','aarch64',True,False),('Darwin','x86_64',False,False),('Darwin','arm64',False,False),('Darwin','x86_64',False,True)]:
                for requested in ['', 'latest', 'stable', '2.1.285']:
                    for use_jq in ([False,True] if shutil.which('jq') else [False]):
                        case(root, args.directory, script, config, seen, base, osname, arch, musl, rosetta, requested, '', use_jq); count += 1
            for failure in ['channel','manifest','hash','download','redirect','linked-directory','existing-launcher','existing-version']:
                case(root,args.directory,script,config,seen,base,'Linux','x86_64',False,False,'latest',failure,False); count+=1
    finally:
        server.shutdown(); server.server_close(); thread.join()
    ps = (args.directory / 'install.ps1').read_text()
    assert '& $binaryPath install' not in ps
    assert "$env:DISABLE_UPDATES = '1'" in ps and 'finally { $env:DISABLE_UPDATES = $previous }' in ps
    assert '-MaximumRedirection 0' in ps and 'https://downloads.claude.ai' not in ps
    assert '[IO.File]::Replace' in ps and '@VERSION@.exe" @args' in ps
    if shutil.which('pwsh'):
        env={**os.environ,'REDAPP_INSTALLER_SYNTAX_PATH':str(args.directory/'install.ps1')}
        subprocess.run(['pwsh','-NoProfile','-NonInteractive','-Command',"$e=$null;$t=$null;[System.Management.Automation.Language.Parser]::ParseFile($env:REDAPP_INSTALLER_SYNTAX_PATH,[ref]$t,[ref]$e)|Out-Null;if($e.Count){$e;exit 1}"],env=env,check=True,timeout=30)
        print('PowerShell parser: PASS; Windows execution not covered here')
    else: print('PowerShell static checks: PASS; parser/Windows execution not available')
    print(f'Claude Shell {count} offline cases: PASS (platforms simulated, no official binary executed)')


def case(root,directory,script,config,seen,base,osname,arch,musl,rosetta,requested,failure,use_jq):
    work=root/str(len(list(root.iterdir()))); tools=work/'tools'; home=work/'user home'; tools.mkdir(parents=True); home.mkdir()
    def tool(name, content): p=tools/name;p.write_text(content);p.chmod(0o755)
    for name in ['id','mkdir','mktemp','rm','chmod','cp','ln','mv','grep','tr','sed','cut','head','dirname','sha256sum','stty','cat']:
        path=shutil.which(name); assert path,name; (tools/name).symlink_to(path)
    if use_jq: (tools/'jq').symlink_to(shutil.which('jq'))
    curl=shutil.which('curl');assert curl
    tool('curl', '#!/usr/bin/python3\nimport sys,subprocess\nurls=[x for x in sys.argv[1:] if x.startswith(("http://","https://"))]\nassert len(urls)==1 and urls[0].startswith('+repr(base+'/')+')\nsys.exit(subprocess.call(['+repr(curl)+']+sys.argv[1:]))\n')
    tool('uname', f'#!/bin/sh\ncase "$1" in -s) echo {osname};; -m) echo {arch};; *) exit 1;; esac\n')
    tool('sysctl', '#!/bin/sh\necho '+('1' if rosetta else '0')+'\n')
    tool('ldd', '#!/bin/sh\necho '+('musl' if musl else 'glibc')+'\n')
    tool('shasum', '#!/bin/sh\nshift 2\nexec sha256sum "$@"\n')
    # Exercise the original optional compression fallback: unsigned compressed metadata is unavailable.
    tool('zstd','#!/bin/sh\nexit 77\n')
    cpu='arm64' if arch in ['arm64','aarch64'] or rosetta else 'x64'
    platform=('linux' if osname=='Linux' else 'darwin')+'-'+cpu+('-musl' if musl else '')
    config.clear();config.update(version='2.1.285',platform=platform,failure=failure,body=b'#!/bin/sh\nprintf "%s\\n" "$DISABLE_UPDATES" "$@"\nexit 23\n')
    env={'PATH':str(tools),'HOME':str(home),'SUDO_USER':'','DISABLE_UPDATES':'0','LC_ALL':'C'}
    destination=home/'.local/share/claude/versions/2.1.285';launcher=home/'.local/bin/claude'
    sentinel=work/'sentinel';sentinel.write_text('do not change')
    if failure=='linked-directory': (home/'.local').symlink_to(sentinel)
    if failure=='existing-launcher': launcher.parent.mkdir(parents=True);launcher.write_text('existing unrelated launcher')
    if failure=='existing-version': destination.parent.mkdir(parents=True);destination.write_text('existing invalid version')
    original_launcher=launcher.read_bytes() if launcher.is_file() else None
    seen.clear(); command=[shutil.which('bash'),'-s','--']+([requested] if requested else [])
    r=subprocess.run(command,input=script,text=True,capture_output=True,env=env,timeout=20)
    if failure:
        assert r.returncode!=0,(failure,r.stdout,r.stderr)
        assert 'Installation complete!' not in r.stdout
        if original_launcher is not None: assert launcher.read_bytes()==original_launcher
        else: assert not launcher.exists()
        assert sentinel.read_text()=='do not change'
    else:
        assert r.returncode==0,(r.stdout,r.stderr)
        assert destination.read_bytes()==config['body'] and destination.stat().st_mode&0o111
        assert 'Add "$HOME/.local/bin" to PATH' in r.stdout
        assert ('/latest' in seen)==(requested in ['', 'latest'])
        assert ('/stable' in seen)==(requested=='stable')
        run=subprocess.run([str(launcher),'argument with spaces','semi;colon','--flag'],env=env,capture_output=True,text=True,timeout=5)
        assert run.returncode==23 and run.stdout.splitlines()==['1','argument with spaces','semi;colon','--flag'],run
        # A fresh process receives the same update policy; no parent/global environment change is needed.
        run=subprocess.run([str(launcher),'--version'],env={**env,'DISABLE_UPDATES':'false'},capture_output=True,text=True,timeout=5)
        assert run.stdout.splitlines()==['1','--version'] and env['DISABLE_UPDATES']=='0'
        inode=destination.stat().st_ino
        rerun=subprocess.run(command,input=script,text=True,capture_output=True,env=env,timeout=20)
        assert rerun.returncode==0 and destination.stat().st_ino==inode
        old_launcher=launcher.read_bytes();config['version']='2.1.286';config['failure']='hash'
        bad=subprocess.run([shutil.which('bash'),'-s','--','2.1.286'],input=script,text=True,capture_output=True,env=env,timeout=20)
        assert bad.returncode!=0 and launcher.read_bytes()==old_launcher and not (destination.parent/'2.1.286').exists()
        config['failure']=''
        upgraded=subprocess.run([shutil.which('bash'),'-s','--','stable'],input=script,text=True,capture_output=True,env=env,timeout=20)
        assert upgraded.returncode==0,(upgraded.stdout,upgraded.stderr)
        assert destination.exists() and '2.1.286' in launcher.read_text()
    assert not list((home/'.claude/downloads').glob('redapp.*')),'temporary download directory leaked'
    assert '/escaped' not in seen,'redirect was followed'


if __name__=='__main__': main()
