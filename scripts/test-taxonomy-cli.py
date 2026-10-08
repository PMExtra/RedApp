#!/usr/bin/env python3
"""Native local CLI: taxonomy CRUD, overlays, filtering, related cards, restart."""
import http.cookiejar
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix='redapp-taxonomy-cli-') as temp:
    directory = Path(temp)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0)); port = sock.getsockname()[1]
    base = f'http://127.0.0.1:{port}'
    config = directory/'config.json'
    config.write_text(json.dumps({'schema_version':1,'data_dir':str(directory/'data'),'listen':f'127.0.0.1:{port}'}))
    log = directory/'server.log'
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),urllib.request.ProxyHandler({}))
    csrf = ''
    def request(path, body=None, method=None, status=200):
        req = urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,method=method,headers={'Content-Type':'application/json','Origin':base,'X-CSRF-Token':csrf})
        try: response = opener.open(req, timeout=10)
        except urllib.error.HTTPError as error: response = error
        with response:
            value = json.loads(response.read())
            assert response.status == status, (path, response.status, value)
            return value
    def start():
        stream = log.open('a')
        env = {key:value for key,value in os.environ.items() if not key.startswith('REDAPP_')}
        process = subprocess.Popen([str(root/'bin/redapp'),'serve','--config',str(config)],cwd=directory,env=env,stdout=stream,stderr=stream)
        for _ in range(150):
            if process.poll() is not None: raise RuntimeError(log.read_text())
            try: request('/api/bootstrap'); return process,stream
            except OSError: time.sleep(.05)
        process.terminate();process.wait(timeout=15);stream.close();raise RuntimeError('Readiness timeout')
    def stop(process,stream): process.terminate();process.wait(timeout=15);stream.close()
    process,stream = start()
    try:
        password = re.search(r'Initial admin password: (.*?);',log.read_text()).group(1)
        csrf = request('/admin/api/login',{'password':password})['csrf']
        name = {'en':'Tools','zh-CN':'工具'}
        category = request('/admin/api/taxonomy/categories',{'id':'tools','name':name})
        tag = request('/admin/api/taxonomy/tags',{'id':'cli','name':{'en':'CLI','zh-CN':'命令行'}})
        request('/admin/api/vendors',{'id':'taxonomy','name':name,'enabled':True},status=201)
        for app in ['one','two','disabled']:
            request('/admin/api/vendors/taxonomy/apps',{'id':app,'name':name,'provider':'info','enabled':app!='disabled','category':'tools','tags':['cli']},status=201)
        own = '/admin/api/apps/taxonomy/one/configuration'
        cfg = request(own)
        public_revision = request('/api/bootstrap')['revision']
        category = request('/admin/api/taxonomy/categories/tools',{'revision':category['revision'],'set':{'name.en':'Renamed'},'unset':[]},method='PATCH')
        assert request(own)['revision'] == cfg['revision']
        assert request('/api/bootstrap')['revision'] != public_revision
        catalog = request('/api/catalog?category=tools&q=two&limit=1')
        assert catalog['total']==1 and catalog['items'][0]['id']=='taxonomy/two'
        assert catalog['categories']==[{'id':'tools','name':{'en':'Renamed','zh-CN':'工具'}}]
        related = request('/api/apps/taxonomy/one/related')['items']
        assert [item['id'] for item in related] == ['taxonomy/two']
        for item in [catalog,related]:
            assert all(f'"{key}"' not in json.dumps(item) for key in ['defaults','overrides','proxy_effective','source_epoch','base_url'])
        conflict = request('/admin/api/taxonomy/tags/cli',{'revision':tag['revision']},method='DELETE',status=409)
        assert conflict['references']==3
        for app in ['one','two','disabled']:
            path = f'/admin/api/apps/taxonomy/{app}/configuration'
            current = request(path)
            request(path,{'revision':current['revision'],'set':{'category':'','tags':[]},'unset':[]},method='PATCH')
        request('/admin/api/taxonomy/tags/cli',{'revision':tag['revision']},method='DELETE')
        request('/admin/api/taxonomy/categories/tools',{'revision':category['revision']},method='DELETE')
        stop(process,stream); process,stream = start()
        csrf = request('/admin/api/login',{'password':password})['csrf']
        assert request('/admin/api/taxonomy')['total']==0
        assert request('/api/apps/taxonomy/one/related')['items']==[]
        assert request(own)['effective']['tags']==[]
        print('PASS native taxonomy CLI: CRUD/CAS, references, public filter/privacy, related, clear/delete, restart')
    finally: stop(process,stream)
