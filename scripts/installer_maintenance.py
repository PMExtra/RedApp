#!/usr/bin/env python3
"""官方原文字节检测、隔离验证和受限制品封装；不执行下载内容的检测阶段。"""
import argparse
from datetime import datetime, timezone
import hashlib
import ipaddress
import http.client
import socket
import ssl
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import urllib.parse
import zipfile

from installer_manifest import ROOT, applications, inventory
MAX_SCRIPT = 256 * 1024
MAX_REDIRECTS = 5

def digest(data): return hashlib.sha256(data).hexdigest()
def run(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=180, **kwargs).stdout

def validate_https_url(url):
    target=urllib.parse.urlsplit(url)
    if target.scheme!='https' or not target.hostname or target.username is not None or target.password is not None or target.fragment:
        raise ValueError('Installer URL must use HTTPS without credentials or a fragment')
    try:
        addresses=socket.getaddrinfo(target.hostname,target.port or 443,type=socket.SOCK_STREAM)
    except (OSError,ValueError) as error:
        raise ValueError('Cannot validate the installer destination address') from error
    if not addresses:raise ValueError('Installer destination has no addresses')
    for address in addresses:
        validate_address(address[4][0])

def validate_address(address):
    ip=ipaddress.ip_address(address.split('%',1)[0])
    if not ip.is_global or ip.is_multicast or ip.is_reserved:
        raise ValueError('Installer destination resolves to a non-public address')

class PublicHTTPSConnection(http.client.HTTPSConnection):
    def connect(self):
        # Check the actual socket peer before TLS/request data, not just a prior
        # DNS lookup. ProxyHandler({}) below prevents environment proxy routing.
        http.client.HTTPConnection.connect(self)
        try:
            validate_address(self.sock.getpeername()[0])
            self.sock=self._context.wrap_socket(self.sock,server_hostname=self.host)
        except Exception:
            self.close()
            raise

class PublicHTTPSHandler(urllib.request.HTTPSHandler):
    def https_open(self, request):
        return self.do_open(PublicHTTPSConnection,request,context=self._context)

class HTTPSRedirect(urllib.request.HTTPRedirectHandler):
    def __init__(self, original):
        self.visited={original}
        self.count=0
    def redirect_request(self,req,fp,code,msg,headers,newurl):
        if self.count>=MAX_REDIRECTS or newurl in self.visited:
            raise ValueError('Installer redirect limit or loop detected')
        validate_https_url(newurl)
        self.count+=1;self.visited.add(newurl)
        return super().redirect_request(req,fp,code,msg,headers,newurl)

def download(url):
    validate_https_url(url)
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),HTTPSRedirect(url),PublicHTTPSHandler(context=ssl.create_default_context()))
    request=urllib.request.Request(url,headers={'User-Agent':'RedApp-installer-maintenance','Accept-Encoding':'identity'})
    start=time.monotonic()
    with opener.open(request,timeout=25) as response:
        if response.status!=200: raise ValueError('Unexpected upstream response')
        validate_https_url(response.url)
        if response.headers.get('Content-Encoding','identity')!='identity': raise ValueError('Unexpected upstream content encoding')
        chunks=[];size=0
        while True:
            if time.monotonic()-start>40: raise TimeoutError('Upstream download deadline exceeded')
            chunk=response.read1(min(64*1024,MAX_SCRIPT+1-size))
            if not chunk: break
            chunks.append(chunk);size+=len(chunk)
            if size>MAX_SCRIPT: raise ValueError('Upstream script exceeds size limit')
        return b''.join(chunks)

def script_shape(name,data):
    if not data or len(data)>MAX_SCRIPT or b'\0' in data: raise ValueError('Empty, oversized or binary response')
    text=data.decode('utf-8-sig')
    if re.match(r'\s*<(?:!doctype|html|head|body)\b',text,re.I): raise ValueError('Upstream returned an HTML error page')
    if name=='install.sh' and not re.match(r'^#![^\n]{0,100}\b(?:ba)?sh\b',text): raise ValueError('Response is not the expected Shell script')

def inspect(root=ROOT,fetch=download):
    rows=[]
    for item in inventory(root):
        row={**item,'status':'error','current_sha256':None}
        try:
            base=root/'installers'/item['application']
            body=(base/'upstream'/item['name']).read_bytes()
            expected=json.loads((base/'provenance.json').read_text())['files'][item['name']]['sha256']
            row['baseline_sha256']=digest(body)
            if row['baseline_sha256']!=expected: raise ValueError('Main baseline differs from its audited digest')
            current=fetch(item['url']);script_shape(item['name'],current)
            row['current_sha256']=digest(current);row['data']=current
            row['status']='unchanged' if current==body else 'changed'
        except (OSError,ValueError,UnicodeError,TimeoutError) as error:
            row['error']=str(error) if not isinstance(error,urllib.error.HTTPError) else f'Upstream HTTP {error.code}'
        rows.append(row)
    return rows

def annotation(text): return text.replace('%','%25').replace('\r','%0D').replace('\n','%0A')
def report(rows,baseline):
    lines=['## Official installer check','',f'Baseline main commit: `{baseline}`','', '| File | Official source | Result | Baseline SHA256 | Current SHA256 |','| --- | --- | --- | --- | --- |']
    for r in rows:
        file=r['application']+'/'+r['name']
        lines.append(f"| {file} | {r['url']} | {r['status']} | `{r.get('baseline_sha256','unavailable')}` | `{r['current_sha256'] or 'unavailable'}` |")
        if r['status']=='error': print('::error title=Installer upstream check failed::'+annotation(file+': '+r.get('error','Unknown failure')))
        if r['status']=='changed': print('::notice title=Official installer changed::'+annotation(file+': '+r['baseline_sha256']+' -> '+r['current_sha256']))
    lines+=['','Changed scripts require strict patch application and isolated tests before a draft PR. Download/validation failures are errors, not “unchanged”.']
    summary='\n'.join(lines)+'\n'
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'],'a') as f:f.write(summary)
    return summary

def prepare(destination,root=ROOT,fetch=download):
    destination.mkdir(parents=True,exist_ok=True)
    baseline=run(['git','rev-parse','HEAD'],cwd=root).strip()
    if not re.fullmatch('[0-9a-f]{40}',baseline): raise ValueError('Invalid checked-out main commit')
    rows=inspect(root,fetch);summary=report(rows,baseline)
    (destination/'summary.md').write_text(summary)
    plan={'baseline':baseline,'checked_at':datetime.now(timezone.utc).isoformat(),'rows':[{k:v for k,v in r.items() if k!='data'} for r in rows]}
    (destination/'plan.json').write_text(json.dumps(plan,indent=2)+'\n')
    if any(r['status']=='error' for r in rows): raise ValueError('One or more upstream checks failed; all results were collected, no update will be published')
    for r in rows:
        target=destination/'sources'/r['application']/r['name'];target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(r['data'])
    changed=any(r['status']=='changed' for r in rows)
    if os.environ.get('GITHUB_OUTPUT'):
        with open(os.environ['GITHUB_OUTPUT'],'a') as f:f.write(f'changed={str(changed).lower()}\nbaseline={baseline}\n')
    return changed

def strict_patch(original,patch):
    with tempfile.TemporaryDirectory(prefix='redapp-patch-') as tmp:
        file=Path(tmp)/'installer';file.write_bytes(original)
        result=run(['patch','--batch','--forward','--fuzz=0',str(file),str(patch.resolve())])
        if re.search(r'offset|fuzz|FAILED|Reversed',result,re.I): raise ValueError('Patch context moved; maintainer review is required')
        return file.read_bytes()

def audit_shell(directory, provider):
    shell = (directory / 'install.sh').read_text()
    if '@REDAPP_BASE_URL@' not in shell: raise ValueError('Missing installer origin placeholder')
    if provider == 'claude-code':
        if '"$binary_path" install' in shell or 'DISABLE_UPDATES=1 exec' not in shell:
            raise ValueError('Unexpected second-stage installer or missing managed update policy')
        if 'https://downloads.claude.ai' in shell or 'https://claude.ai/install.' in shell:
            raise ValueError('Unexpected installer download origin')
    else:
        if re.search(r'https?://', shell) or 'rm -f "$AUTO_UPDATE_VERSION"' not in shell or 'AUTO_UPDATE_VERSION.tmp' in shell:
            raise ValueError('Unexpected Codex origin or update policy')
    run(['bash' if provider == 'claude-code' else 'sh', '-n', str(directory / 'install.sh')])


def validate_shell(directory, descriptor, root=ROOT):
    audit_shell(directory, descriptor['installer_validator'])
    print(run(['python3', str(root / 'scripts/test-installers.py'), '--platform', 'shell',
               '--application', descriptor['id'], '--directory', str(directory)]), end='')


def validate(prepared,output,root=ROOT):
    # Called inside the network-disabled container, with /src read-only and no GitHub credentials.
    plan=json.loads((prepared/'plan.json').read_text());output.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='redapp-installer-validation-') as tmp:
        work=Path(tmp);shutil.copytree(root/'installers',work/'installers')
        for descriptor in applications(root):
            app=descriptor['id'];names=[x['file'] for x in descriptor['installers']]
            stage=work/'installers'/app
            for name in names:
                raw=(prepared/'sources'/app/name).read_bytes()
                row=next(r for r in plan['rows'] if r['application']==app and r['name']==name)
                if digest(raw)!=row['current_sha256']:raise ValueError('Prepared source digest changed')
                (stage/'upstream'/name).write_bytes(raw)
                (stage/'generated'/name).write_bytes(strict_patch(raw,root/'installers'/app/'patches'/(name+'.patch')))
            validate_shell(stage/'generated', descriptor, root)
            target=output/app;target.mkdir(parents=True,exist_ok=True)
            for name in names:shutil.copyfile(stage/'generated'/name,target/name)

def package(prepared,validated,bundle,root=ROOT):
    # Trusted host step after the isolated container exits. Rebuild expected outputs without executing scripts.
    plan=json.loads((prepared/'plan.json').read_text());files={}
    if plan['baseline']!=run(['git','rev-parse','HEAD'],cwd=root).strip():raise ValueError('Baseline changed during validation')
    for descriptor in applications(root):
        app=descriptor['id'];names=[x['file'] for x in descriptor['installers']]
        base=root/'installers'/app;manifest=json.loads((base/'provenance.json').read_text());changed=False
        for name in names:
            raw=(prepared/'sources'/app/name).read_bytes();row=next(r for r in plan['rows'] if r['application']==app and r['name']==name)
            if digest(raw)!=row['current_sha256']:raise ValueError('Downloaded source changed after preparation')
            generated=strict_patch(raw,base/'patches'/(name+'.patch'))
            candidate=validated/app/name
            if candidate.is_symlink() or not candidate.is_file() or candidate.stat().st_size>MAX_SCRIPT or candidate.read_bytes()!=generated:raise ValueError('Isolated output differs from the audited source plus patch')
            if raw!=(base/'upstream'/name).read_bytes():
                changed=True;files[f'installers/{app}/upstream/{name}']=raw;files[f'installers/{app}/generated/{name}']=generated
                manifest['files'][name].update(bytes=len(raw),sha256=digest(raw),source=row['url'])
        if changed:
            manifest['script_baseline']={'kind':'official-live','checked_at':plan['checked_at'],'previous_main_commit':plan['baseline']}
            files[f'installers/{app}/provenance.json']=(json.dumps(manifest,indent=2,ensure_ascii=False)+'\n').encode()
    payload={'baseline':plan['baseline'],'rows':plan['rows'],'files':{name:digest(body) for name,body in files.items()},'validation':'Strict zero-offset patches; isolated offline descriptor-selected Shell tests. This candidate still requires the Windows PowerShell 7/5.1 job before draft publication.'}
    with zipfile.ZipFile(bundle,'w',compression=zipfile.ZIP_DEFLATED) as archive:
        for name,body in files.items():archive.writestr(name,body)
        archive.writestr('update.json',json.dumps(payload,indent=2))
    sha=digest(bundle.read_bytes())
    if os.environ.get('GITHUB_OUTPUT'):
        with open(os.environ['GITHUB_OUTPUT'],'a') as f:f.write('sha256='+sha+'\n')
    print('Validated installer update bundle SHA256: '+sha)

def main():
    parser=argparse.ArgumentParser(description=__doc__);sub=parser.add_subparsers(dest='mode',required=True)
    p=sub.add_parser('prepare');p.add_argument('destination',type=Path)
    p=sub.add_parser('validate');p.add_argument('prepared',type=Path);p.add_argument('output',type=Path)
    p=sub.add_parser('package');p.add_argument('prepared',type=Path);p.add_argument('validated',type=Path);p.add_argument('bundle',type=Path)
    args=parser.parse_args()
    if args.mode=='prepare':prepare(args.destination)
    elif args.mode=='validate':validate(args.prepared,args.output)
    else:package(args.prepared,args.validated,args.bundle)

if __name__=='__main__':
    try:main()
    except (OSError,ValueError,subprocess.SubprocessError) as error:raise SystemExit('Installer maintenance failed: '+str(error))
