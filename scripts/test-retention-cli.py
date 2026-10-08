#!/usr/bin/env python3
"""Local trusted Codex fixture → cached binaries → retention → restart receipts."""
import hashlib
import http.cookiejar
import http.server
import json
import os
from pathlib import Path
import re
import socket
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.parse
import urllib.request

root = Path(__file__).resolve().parents[1]
class Fixture(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        path = urllib.parse.urlsplit(self.path).path
        pieces = path.strip('/').split('/')
        version = pieces[1] if pieces[0] == 'releases' else '2.0.0'
        body = ('binary:' + version).encode()
        if pieces[0] == 'channels' or path.endswith('/release.json'):
            body = json.dumps({'tag_name':'rust-v'+version,'assets':[{'name':'asset.tgz',
                'digest':'sha256:'+hashlib.sha256(body).hexdigest(),
                'browser_download_url':'http://retention.example/releases/'+version+'/asset.tgz'}]}).encode()
        self.send_response(200)
        self.send_header('Content-Length',str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self,*args): pass
fixture = http.server.ThreadingHTTPServer(('127.0.0.1',0),Fixture)
thread = threading.Thread(target=fixture.serve_forever,daemon=True)
thread.start()
try:
    with tempfile.TemporaryDirectory(prefix='redapp-retention-cli-') as temp:
        directory = Path(temp)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1',0)); port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        config = directory/'config.json'
        config.write_text(json.dumps({'schema_version':1,'data_dir':str(directory/'data'),'listen':f'127.0.0.1:{port}'}))
        log = directory/'server.log'
        jar = http.cookiejar.CookieJar()
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar),urllib.request.ProxyHandler({}))
        csrf = ''
        def request(path,body=None,method=None,headers=None):
            req = urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,
                method=method,headers={'Content-Type':'application/json','Origin':base,'X-CSRF-Token':csrf,**(headers or {})})
            with opener.open(req,timeout=10) as response:
                data = response.read()
                return json.loads(data) if response.headers.get('Content-Type','').startswith('application/json') else data
        def start():
            stream = log.open('a')
            env = {k:v for k,v in os.environ.items() if not k.startswith('REDAPP_')}
            proc = subprocess.Popen([str(root/'bin/redapp'),'serve','--config',str(config)],cwd=directory,env=env,stdout=stream,stderr=stream)
            for _ in range(150):
                if proc.poll() is not None: raise RuntimeError('CLI startup failed')
                try: request('/api/bootstrap'); return proc,stream
                except (OSError,urllib.error.URLError): time.sleep(.05)
            raise RuntimeError('CLI readiness timeout')
        def stop(proc,stream):
            proc.terminate(); proc.wait(timeout=15); stream.close()
        proc,stream = start()
        try:
            password = re.search(r'Initial admin password: (.*?);',log.read_text()).group(1)
            csrf = request('/admin/api/login',{'password':password})['csrf']
            proxy = request('/admin/api/settings/proxy')
            request('/admin/api/settings/proxy',{'mode':'url','url':f'http://127.0.0.1:{fixture.server_port}'},method='PUT',headers={'If-Match':f'"{proxy["revision"]}"'})
            request('/admin/api/vendors',{'id':'retention','name':{'en':'Retention','zh-CN':'保留'},'enabled':True})
            request('/admin/api/vendors/retention/apps',{'id':'binary','provider':'codex','name':{'en':'Binary','zh-CN':'二进制'},'base_url':'http://retention.example','cache_ttl_seconds':60,'enabled':True})
            for version in ['1.0.0','2.0.0','10.0.0']:
                assert request(f'/retention/binary/releases/{version}/asset.tgz') == ('binary:'+version).encode()
            cfg = request('/admin/api/apps/retention/binary/configuration')
            cfg = request('/admin/api/apps/retention/binary/configuration',{'revision':cfg['revision'],'set':{'retention':{'enabled':False,'keep_latest':1}},'unset':[]},method='PATCH')
            preview = request('/admin/api/apps/retention/binary/retention/preview',{'revision':cfg['revision']})
            assert preview['selected_versions'] == 1
            result = request(f'/admin/api/apps/retention/binary/retention/{preview["id"]}/execute',{})
            assert result['retired_versions'] == 1 and result['logical_bytes'] == len('binary:1.0.0')
            status = request('/admin/api/apps/retention/binary/retention/status')
            assert status['outcome'] == 'success'
            with sqlite3.connect(directory/'data/state.sqlite') as db:
                assert db.execute("SELECT count(*) FROM generations WHERE phase='complete' AND is_current=1").fetchone()[0] == 2
                assert db.execute("SELECT sum(artifact_requests) FROM app_versions WHERE version='1.0.0'").fetchone()[0] == 1
                receipt = db.execute('SELECT result_json FROM cleanup_previews WHERE id=?',(preview['id'],)).fetchone()[0]
            cfg = request('/admin/api/apps/retention/binary/configuration')
            request('/admin/api/apps/retention/binary/configuration',{'revision':cfg['revision'],'set':{'retention':{'enabled':True,'keep_latest':1}},'unset':[]},method='PATCH')
        finally: stop(proc,stream)
        proc,stream = start()
        try:
            csrf = request('/admin/api/login',{'password':password})['csrf']
            repeated = request(f'/admin/api/apps/retention/binary/retention/{preview["id"]}/execute',{})
            assert repeated == result
            assert request('/admin/api/apps/retention/binary/retention/status')['outcome'] == 'success'
            with sqlite3.connect(directory/'data/state.sqlite') as db:
                assert db.execute('SELECT result_json FROM cleanup_previews WHERE id=?',(preview['id'],)).fetchone()[0] == receipt
                assert db.execute('SELECT count(*) FROM cleanup_previews').fetchone()[0] == 1, 'startup ran an immediate cleanup'
                assert db.execute("SELECT count(*) FROM generations WHERE phase='complete' AND is_current=1").fetchone()[0] == 2
        finally: stop(proc,stream)
        print('CLI local trusted fixture downloads → N=1 → channel-protected preview → execution → retained stats → restart/idempotent receipt: passed.')
finally:
    fixture.shutdown();fixture.server_close()
