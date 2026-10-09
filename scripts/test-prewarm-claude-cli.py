#!/usr/bin/env python3
"""Explicit network integration: official signed Claude manifest and one binary."""
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
        proc,stream=start()
        try:
            password=re.search(r'Initial admin password: (.*?);',log.read_text()).group(1)
            csrf=request('/admin/api/login',{'password':password})['csrf']
            manifest=json.loads((root/'internal/apps/claude/testdata/manifest.json').read_text())
            platform=min(manifest['platforms'],key=lambda k:manifest['platforms'][k]['size'])
            request('/admin/api/vendors',{'id':'signed','name':{'en':'Signed fixture','zh-CN':'签名夹具'},'enabled':True})
            request('/admin/api/vendors/signed/apps',{'id':'claude','provider':'claude-code','name':{'en':'Claude fixture','zh-CN':'Claude 夹具'},'base_url':'https://downloads.claude.ai/claude-code-releases','cache_ttl_seconds':60,'enabled':True})
            # The binary is over 200 MB, so slow egress is not a failure; only a stalled read is.
            def wait(job):
                deadline=time.monotonic()+1800
                progress,last=-1,time.monotonic()
                while time.monotonic()<deadline:
                    result=request('/admin/api/apps/signed/claude/prewarm/'+job['id'])
                    if result['state']!='running':return result
                    if result['bytes']!=progress:progress,last=result['bytes'],time.monotonic()
                    elif time.monotonic()-last>120:raise RuntimeError(f'Official Claude download made no progress for two minutes at {progress} bytes')
                    time.sleep(.2)
                raise RuntimeError('Official Claude download exceeded the 30-minute integration bound')
            job=request('/admin/api/apps/signed/claude/prewarm/start',{'request_id':'a'*32,'target':manifest['version'],'platforms':[platform]})
            result=wait(job)
            assert result['state']=='completed' and result['succeeded']==1,result
            with sqlite3.connect(directory/'data/state.sqlite') as db:
                phase=db.execute("SELECT phase FROM generations WHERE is_current=1").fetchone()[0]
                assert phase=='complete'
                assert db.execute("SELECT count(*) FROM generations WHERE phase='complete' AND is_current=1").fetchone()[0]==1
            cached=wait(request('/admin/api/apps/signed/claude/prewarm/start',{'request_id':'b'*32,'target':manifest['version'],'platforms':[platform]}))
            assert cached['state']=='completed' and cached['bytes']==0,cached
            print('Official Claude signature → platform '+platform+' → '+str(result['bytes'])+' bytes → final digest verification → zero-read complete cache hit: passed.')
        finally:stop(proc,stream)
except Exception:
    raise
