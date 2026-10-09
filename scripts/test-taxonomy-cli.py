#!/usr/bin/env python3
"""Native local CLI: multi-category save/rename/cleanup, free tags, public counts/privacy, restart."""
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
    def request(path, body=None, method=None, status=200, session=None):
        client, token = session or (opener, csrf)
        req = urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,method=method,headers={'Content-Type':'application/json','Origin':base,'X-CSRF-Token':token})
        try: response = client.open(req, timeout=10)
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
    def save(app, body, status=200):
        path = f'/admin/api/apps/taxonomy/{app}/configuration'
        return request(path,{'revision':request(path)['revision'],'unset':[],**body},method='PATCH',status=status)
    def categories(): return {item['id']:item for item in request('/admin/api/categories?limit=100')['items']}
    process,stream = start()
    try:
        password = re.search(r'Initial admin password: (.*?);',log.read_text()).group(1)
        csrf = request('/admin/api/login',{'password':password})['csrf']
        name = {'en':'Tools','zh-CN':'工具'}
        request('/admin/api/vendors',{'id':'taxonomy','name':name,'enabled':True},status=201)
        for app in ['one','two','disabled']:
            request('/admin/api/vendors/taxonomy/apps',{'id':app,'name':name,'provider':'info','enabled':app!='disabled'},status=201)
        # Typed categories are created with the App save; tags are free text without a dictionary.
        one = save('one',{'set':{'categories':[],'tags':[' #CLI ','cli','命令行']},'new_categories':['Tools','效率工具']})
        chinese = next(id for id in one['effective']['categories'] if id != 'tools')
        assert one['effective']['categories'] == sorted(['tools',chinese]) and one['effective']['tags'] == ['CLI','命令行']
        save('two',{'set':{'categories':['tools']}})
        save('disabled',{'set':{'categories':['tools']},'new_categories':['Private only']})
        save('two',{'set':{'categories':['tools']},'new_categories':['tools ', 'TOOLS']})
        assert set(categories()) == {'tools',chinese,'private-only'} and categories()['tools']['applications'] == 3
        own = '/admin/api/apps/taxonomy/one/configuration'
        cfg = request(own)
        public_revision = request('/api/bootstrap')['revision']
        tools = categories()['tools']
        request('/admin/api/categories/tools',{'revision':tools['revision'],'set':{'name.en':'Renamed'},'unset':[]},method='PATCH')
        assert request(own)['revision'] == cfg['revision']
        assert request('/api/bootstrap')['revision'] != public_revision
        catalog = request('/api/catalog?category=tools&q=two&limit=1')
        assert catalog['total']==1 and catalog['items'][0]['id']=='taxonomy/two'
        assert catalog['categories']==sorted([{'id':'tools','name':{'en':'Renamed','zh-CN':'Tools'},'count':2},{'id':chinese,'name':{'en':'效率工具','zh-CN':'效率工具'},'count':1}],key=lambda item:item['id'])
        tagged = request('/api/catalog?q=%23cli')
        assert [item['id'] for item in tagged['items']] == ['taxonomy/one'] and tagged['categories'] == catalog['categories']
        for item in [catalog,tagged,request('/api/bootstrap'),request('/api/search?q=cli')]:
            text = json.dumps(item, ensure_ascii=False)
            assert all(f'"{key}"' not in text for key in ['defaults','overrides','proxy_effective','source_epoch','base_url','tags','related'])
            assert 'private-only' not in text and 'taxonomy/disabled' not in text and '命令行' not in text
        request('/api/apps/taxonomy/one/related',status=404)
        request('/admin/api/categories',{'id':'x'},status=405)
        # Two administrator sessions: the stale form is rejected instead of overwriting the newer save.
        other = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),urllib.request.ProxyHandler({}))
        second = (other, request('/admin/api/login',{'password':password},session=(other,''))['csrf'])
        codex = '/admin/api/apps/openai/codex/configuration'
        stale = request(codex,session=second)
        first = request(codex)
        request(codex,{'revision':first['revision'],'set':{'name.en':'First session','tags':['first']},'unset':[]},method='PATCH')
        request(codex,{'revision':stale['revision'],'set':{'name.zh-CN':'第二会话'},'unset':[]},method='PATCH',status=409,session=second)
        current = request(codex)
        assert current['effective']['name']['en']=='First session' and current['fields']['name.zh-CN']['source']=='inherited'
        # Field reset is an unset: after restart the field follows the template again, others keep their overrides.
        request(codex,{'revision':current['revision'],'set':{},'unset':['name.en']},method='PATCH')
        save('one',{'set':{'categories':['tools']}})
        assert chinese not in categories()
        stop(process,stream); process,stream = start()
        csrf = request('/admin/api/login',{'password':password})['csrf']
        assert set(categories()) == {'tools','private-only'} and request(own)['effective']['tags'] == ['CLI','命令行']
        restarted = request(codex)
        assert restarted['fields']['name.en']['source']=='inherited' and restarted['effective']['name']['en']==restarted['defaults']['name']['en']
        assert restarted['fields']['tags']['source']=='custom' and restarted['effective']['tags']==['first']
        for app in ['one','two','disabled']: save(app,{'set':{'categories':[],'tags':[]}})
        stop(process,stream); process,stream = start()
        csrf = request('/admin/api/login',{'password':password})['csrf']
        assert request('/admin/api/categories')['total']==0 and request(own)['effective']['tags']==[]
        print('PASS native taxonomy CLI: typed multi-category save/reuse, rename, disabled references, cleanup, free tags, public counts/privacy, tag search, two-session CAS, field reset after restart')
    finally: stop(process,stream)
