#!/usr/bin/env python3
"""Windows CLI integration with a harmless locally compiled executable and loopback HTTP."""
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

ROOT=Path(__file__).resolve().parents[1]

def run(command,**kwargs):
    return subprocess.run(command,capture_output=True,text=True,timeout=40,**kwargs)

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--shell',default='pwsh')
    args=parser.parse_args()
    if os.name!='nt':raise SystemExit('This test requires a native Windows runner; it cannot be counted as passed elsewhere.')
    shell=shutil.which(args.shell)
    if not shell:raise SystemExit('Requested PowerShell executable is missing')
    seen,config=[],{}
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self,*_):pass
        def do_GET(self):
            seen.append(self.path)
            if config.get('failure')=='redirect':
                self.send_response(302);self.send_header('Location','/escaped');self.end_headers();return
            version=config['version']
            if self.path in ('/latest','/stable'):
                body=version.encode() if config.get('failure')!='channel' else b'2.1.285/../../escape'
            elif self.path==f'/{version}/manifest.json':
                body=json.dumps({'version':version,'platforms':{config['platform']:{'binary':'claude.exe','checksum':hashlib.sha256(config['body']).hexdigest(),'size':len(config['body'])}}}).encode()
                if config.get('failure')=='manifest':body=b'<html>error</html>'
            elif self.path==f'/{version}/{config["platform"]}/claude.exe':
                if config.get('failure')=='download':self.send_error(503);return
                body=config['body']+(b'tampered' if config.get('failure')=='hash' else b'')
            else:self.send_error(404);return
            self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
    thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix='redapp-windows-installer-') as temp:
            root=Path(temp);binary=root/'fixture.exe';source=root/'fixture.cs'
            source.write_text('using System; class Fixture { static int Main(string[] args) { Console.WriteLine(Environment.GetEnvironmentVariable("DISABLE_UPDATES")); foreach (string arg in args) Console.WriteLine(arg); if (args.Length > 0 && args[0] == "hold") System.Threading.Thread.Sleep(15000); return 23; } }')
            compile_command="Add-Type -Path $env:FIXTURE_SOURCE -OutputAssembly $env:FIXTURE_BINARY -OutputType ConsoleApplication"
            compile_result=run(['powershell.exe','-NoProfile','-NonInteractive','-Command',compile_command],env={**os.environ,'FIXTURE_SOURCE':str(source),'FIXTURE_BINARY':str(binary)})
            assert compile_result.returncode==0 and binary.is_file(),compile_result.stderr
            original=(ROOT/'installers/claude-code/generated/install.ps1').read_text()
            script=root/'install.ps1';script.write_text(original.replace('@REDAPP_BASE_URL@',f'http://127.0.0.1:{server.server_port}'),encoding='utf-8')
            parse=" $e=$null;$t=$null;[System.Management.Automation.Language.Parser]::ParseFile($env:FIXTURE_SCRIPT,[ref]$t,[ref]$e)|Out-Null;if($e.Count){$e;exit 1}"
            parsed=run([shell,'-NoProfile','-NonInteractive','-Command',parse],env={**os.environ,'FIXTURE_SCRIPT':str(script)})
            assert parsed.returncode==0,parsed.stdout+parsed.stderr
            count=0
            for arch,platform in [('AMD64','win32-x64'),('ARM64','win32-arm64')]:
                for target in ('','latest','stable','2.1.285'):
                    case(root,script,binary.read_bytes(),shell,config,seen,arch,platform,target,'');count+=1
            for failure in ('hash','channel','manifest','download','redirect','existing-launcher','existing-version','junction'):
                case(root,script,binary.read_bytes(),shell,config,seen,'AMD64','win32-x64','latest',failure);count+=1
            print(f'Claude {args.shell}: parser and {count} Windows offline cases PASS; actual architecture AMD64, ARM64 path selection simulated; no official binary executed')
    finally:server.shutdown();server.server_close();thread.join()


def case(root,script,body,shell,config,seen,arch,platform,target,failure):
    home=root/('user home '+str(len(list(root.iterdir()))));home.mkdir()
    env={**os.environ,'USERPROFILE':str(home),'PROCESSOR_ARCHITECTURE':arch,'DISABLE_UPDATES':'0'}
    config.clear();config.update(version='2.1.285',platform=platform,body=body,failure=failure)
    bin_dir=home/'.local/bin';versions=home/'.local/share/claude/versions';installed=versions/'2.1.285.exe'
    if failure=='existing-launcher':bin_dir.mkdir(parents=True);(bin_dir/'claude.cmd').write_text('existing installation')
    if failure=='existing-version':versions.mkdir(parents=True);installed.write_bytes(b'bad existing binary')
    sentinel=root/('sentinel-'+home.name);sentinel.mkdir();(sentinel/'keep.txt').write_text('do not touch')
    if failure=='junction':
        create=run(['cmd.exe','/d','/c','mklink','/J',str(home/'.local'),str(sentinel)])
        assert create.returncode==0,create.stderr
    seen.clear();command=[shell,'-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',str(script)]+(['-Target',target] if target else [])
    result=run(command,env=env)
    if failure:
        assert result.returncode!=0,(failure,result.stdout,result.stderr)
        assert 'Installation complete!' not in result.stdout
        assert not (bin_dir/'claude.ps1').exists()
        if failure=='existing-launcher':assert (bin_dir/'claude.cmd').read_text()=='existing installation'
        assert (sentinel/'keep.txt').read_text()=='do not touch'
    else:
        assert result.returncode==0,(result.stdout,result.stderr)
        assert installed.read_bytes()==body
        assert ('/latest' in seen)==(target in ('','latest'))
        assert ('/stable' in seen)==(target=='stable')
        for entry in ('claude.ps1','claude.cmd'):
            # The child exit code and argument boundaries must survive each native launcher.
            invocation="& $env:FIXTURE_LAUNCHER 'argument with spaces' 'semi;colon' '--flag'; $result=$LASTEXITCODE; Write-Output ('restored=' + $env:DISABLE_UPDATES); exit $result"
            launched=run([shell,'-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-Command',invocation],env={**env,'FIXTURE_LAUNCHER':str(bin_dir/entry)})
            assert launched.returncode==23,(entry,launched.stdout,launched.stderr)
            assert launched.stdout.splitlines()==['1','argument with spaces','semi;colon','--flag','restored=0'],(entry,launched.stdout,launched.stderr)
        modified=installed.stat().st_mtime_ns
        repeated=run(command,env=env)
        assert repeated.returncode==0 and installed.stat().st_mtime_ns==modified,(repeated.stdout,repeated.stderr)
        old={p.name:p.read_bytes() for p in bin_dir.iterdir()}
        config['version']='2.1.286';config['failure']='hash'
        upgraded=run([shell,'-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',str(script),'-Target','2.1.286'],env=env)
        assert upgraded.returncode!=0 and {p.name:p.read_bytes() for p in bin_dir.iterdir()}==old
        assert not (versions/'2.1.286.exe').exists()
        config['failure']=''
        upgraded=run([shell,'-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',str(script),'-Target','stable'],env=env)
        assert upgraded.returncode==0,(upgraded.stdout,upgraded.stderr)
        assert installed.read_bytes()==body and (versions/'2.1.286.exe').read_bytes()==body
        assert all('2.1.286' in (bin_dir/name).read_text() for name in ('claude.ps1','claude.cmd'))
    assert not list((home/'.claude/downloads').glob('redapp-*')),'temporary download directory leaked'
    assert '/escaped' not in seen,'redirect followed'

if __name__=='__main__':main()
