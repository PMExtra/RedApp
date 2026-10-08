#!/usr/bin/env python3
"""Native CLI: cold index and immutable platform prewarm, then restart."""
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
    def do_HEAD(self):
        self.send_response(304)
        self.send_header('ETag','"fixture"')
        self.end_headers()
    def do_GET(self):
        path = urllib.parse.urlsplit(self.path).path
        if path == '/files/':
            body = b'<a href="one">one</a><a href="sub/">sub</a><a href="../escape">ignore</a>'
        elif path == '/files/sub/':
            body = b'[{"name":"two","type":"file","size":4}]'
        elif path.startswith('/files/'):
            body = b'body'
        else:
            key = 'codex-npm-linux-x64-1.0.0.tgz'
            body = b'verified platform fixture'
            if path.endswith('/release.json'):
                body = json.dumps({'tag_name':'rust-v1.0.0','assets':[{'name':key,
                    'digest':'sha256:'+hashlib.sha256(body).hexdigest(),
                    'browser_download_url':'http://prewarm.example/releases/1.0.0/'+key}]}).encode()
        self.send_response(200)
        self.send_header('Content-Length',str(len(body)))
        self.send_header('ETag','"fixture"')
        if path == '/files/sub/': self.send_header('Content-Type','application/json')
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
        def await_job(key,job):
            for _ in range(200):
                result = request('/admin/api/apps/'+key+'/prewarm/'+job['id'])
                if result['state'] != 'running': return result
                time.sleep(.02)
            raise RuntimeError('Prewarm job timeout')
        proc,stream = start()
        try:
            password = re.search(r'Initial admin password: (.*?);',log.read_text()).group(1)
            csrf = request('/admin/api/login',{'password':password})['csrf']
            proxy = request('/admin/api/settings/proxy')
            request('/admin/api/settings/proxy',{'mode':'url','url':f'http://127.0.0.1:{fixture.server_port}'},method='PUT',headers={'If-Match':f'"{proxy["revision"]}"'})
            request('/admin/api/vendors',{'id':'prewarm','name':{'en':'Prewarm','zh-CN':'预热'},'enabled':True})
            for name,provider,upstream in [('http','http-cache','http://prewarm.example/files'),('binary','codex','http://prewarm.example')]:
                request('/admin/api/vendors/prewarm/apps',{'id':name,'provider':provider,'name':{'en':name,'zh-CN':name},'base_url':upstream,'cache_ttl_seconds':60,'enabled':True})
            http_input={'request_id':'a'*32,'indexes':['/']}
            http_job=request('/admin/api/apps/prewarm/http/prewarm/start',http_input)
            http_result=await_job('prewarm/http',http_job)
            assert http_result['state']=='completed' and http_result['succeeded']==2,http_result
            items=request('/admin/api/apps/prewarm/http/prewarm/'+http_job['id']+'/items')['items']
            assert [x['key'] for x in items]==['/one','/sub/two'],items
            release_input={'request_id':'b'*32,'target':'1.0.0','platforms':['linux-x64']}
            release_job=request('/admin/api/apps/prewarm/binary/prewarm/start',release_input)
            release_result=await_job('prewarm/binary',release_job)
            assert release_result['state']=='completed' and release_result['succeeded']==1,release_result
            cached=await_job('prewarm/binary',request('/admin/api/apps/prewarm/binary/prewarm/start',{**release_input,'request_id':'c'*32}))
            assert cached['state']=='completed' and cached['bytes']==0,cached
            with sqlite3.connect(directory/'data/state.sqlite') as db:
                assert db.execute('SELECT count(*) FROM http_cache_generations WHERE is_current=1').fetchone()[0]==2
                assert db.execute('SELECT count(*) FROM http_cache_generations WHERE last_access_bucket_s != 0').fetchone()[0]==0
                assert db.execute("SELECT count(*) FROM generations WHERE phase='complete' AND is_current=1").fetchone()[0]==1
        finally: stop(proc,stream)
        proc,stream = start()
        try:
            csrf=request('/admin/api/login',{'password':password})['csrf']
            assert request('/admin/api/apps/prewarm/http/prewarm/start',http_input)['id']==http_job['id']
            assert request('/admin/api/apps/prewarm/binary/prewarm/'+release_job['id'])['state']=='completed'
            assert request('/prewarm/http/one')==b'body'
            assert request('/prewarm/binary/releases/1.0.0/codex-npm-linux-x64-1.0.0.tgz')==b'verified platform fixture'
            with sqlite3.connect(directory/'data/state.sqlite') as db:
                assert db.execute('SELECT count(*) FROM prewarm_jobs').fetchone()[0]==3,'startup scheduled immediate warm'
        finally: stop(proc,stream)
        print('CLI cold HTML/nginx indexes → HTTP cached files; verified platform release → complete hit; restart/task idempotency/no immediate warm: passed.')
finally:
    fixture.shutdown();fixture.server_close()
