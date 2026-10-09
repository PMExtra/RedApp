#!/usr/bin/env python3
"""Native HTTP instances A→B: YAML/ZIP portability, omissions, copy and receipt restart."""
import hashlib
import http.cookiejar
import io
import json
import os
from pathlib import Path
import re
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid
import zipfile

root = Path(__file__).resolve().parents[1]
class Instance:
    def __init__(self, directory):
        self.directory = directory
        directory.mkdir()
        with socket.socket() as sock:
            sock.bind(('127.0.0.1',0)); self.port = sock.getsockname()[1]
        self.base = f'http://127.0.0.1:{self.port}'
        self.config = directory/'config.json'
        self.config.write_text(json.dumps({'schema_version':1,'data_dir':str(directory/'data'),'listen':f'127.0.0.1:{self.port}'}))
        self.log = directory/'server.log'
        self.csrf = ''
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),urllib.request.ProxyHandler({}))
        self.start()
        self.password = re.search(r'Initial admin password: (.*?);',self.log.read_text()).group(1)
        self.login()
    def request(self,path,body=None,method=None,status=200,raw=None,content_type='application/json'):
        req = urllib.request.Request(self.base+path,data=raw if raw is not None else json.dumps(body).encode() if body is not None else None,method=method,headers={'Origin':self.base,'X-CSRF-Token':self.csrf,'Content-Type':content_type})
        try: response = self.opener.open(req,timeout=15)
        except urllib.error.HTTPError as error: response = error
        with response:
            data = response.read()
            value = json.loads(data) if response.headers.get('Content-Type','').startswith('application/json') else data
            assert response.status == status,(path,response.status,status,value if isinstance(value,dict) else 'binary response')
            return value
    def start(self):
        self.stream = self.log.open('a')
        env = {key:value for key,value in os.environ.items() if not key.startswith('REDAPP_')}
        self.process = subprocess.Popen([str(root/'bin/redapp'),'serve','--config',str(self.config)],cwd=self.directory,env=env,stdout=self.stream,stderr=self.stream)
        for _ in range(150):
            if self.process.poll() is not None: raise RuntimeError('Native CLI startup failed')
            try: self.request('/api/bootstrap'); return
            except OSError: time.sleep(.05)
        self.stop(); raise RuntimeError('Readiness timeout')
    def login(self): self.csrf = self.request('/admin/api/login',{'password':self.password})['csrf']
    def stop(self):
        if self.process.poll() is None: self.process.terminate();self.process.wait(timeout=15)
        self.stream.close()
    def preview(self,package,choices=None):
        boundary = 'redapp-'+uuid.uuid4().hex
        raw = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="ignored-name.zip"\r\nContent-Type: application/octet-stream\r\n\r\n'.encode()+package+f'\r\n--{boundary}\r\nContent-Disposition: form-data; name="choices"\r\n\r\n'.encode()+json.dumps(choices or []).encode()+f'\r\n--{boundary}--\r\n'.encode())
        return self.request('/admin/api/configuration/import/preview',raw=raw,method='POST',content_type=f'multipart/form-data; boundary={boundary}')
    def execute(self,preview,trust=True,status=200):return self.request(f'/admin/api/configuration/import/{preview["id"]}/execute',{'confirm':True,'trust_instructions':trust},status=status)
    def config_of(self,key):return self.request('/admin/api/apps/'+key+'/configuration')

def body_images(package):
    with zipfile.ZipFile(io.BytesIO(package)) as archive:
        files = {name:archive.read(name) for name in archive.namelist()}
    assert all(name.startswith('presets/') for name in files)
    return files

with tempfile.TemporaryDirectory(prefix='redapp-exchange-cli-') as temp:
    directory = Path(temp)
    a = Instance(directory/'a'); b = None
    try:
        name={'en':'Portable','zh-CN':'可移植'}
        a.request('/admin/api/vendors',{'id':'portable','name':name,'icon':'/assets/presets/builtin/openai.svg'},status=201)
        vendor=a.request('/admin/api/vendors/portable/configuration')
        a.request('/admin/api/vendors/portable/configuration',{'revision':vendor['revision'],'set':{'proxy':{'mode':'url','url':'http://private-user:private-password@127.0.0.1:3128'}},'unset':[]},method='PATCH')
        original=a.request('/admin/api/apps/openai/codex')['app']
        source=a.request('/admin/api/apps/openai/codex/copy',{'source_uid':original['uid'],'source_revision':original['revision'],'target_vendor':'portable','target_id':'source','mode':'linked'},status=201)['app']
        cfg=a.config_of('portable/source')
        a.request('/admin/api/apps/portable/source/configuration',{'revision':cfg['revision'],'set':{'name.en':cfg['effective']['name']['en'],'description.zh-CN':'','instructions.en':'## Portable\n\n<script>fetch("https://must-not-fetch.invalid")</script>\n','categories':[],'tags':['CLI','命令行'],'prewarm':{'enabled':False,'channels':[],'platforms':[]},'retention':{'enabled':True,'keep_latest':2},'proxy':{'mode':'inherit'}},'unset':[],'new_categories':['Tools']},method='PATCH')
        cfg=a.config_of('portable/source');assert cfg['effective']['categories']==['tools']
        a.request('/admin/api/apps/portable/source/admin-notes',{'revision':0,'text':'private-notes-sentinel'},method='PUT')
        def export(mode,**options):return a.request('/admin/api/configuration/export',{'selection':[{'kind':'App','key':'portable/source'}],'mode':mode,'include_notes':False,'include_proxy_credentials':False,**options})
        linked=export('linked'); independent=export('independent'); sensitive=export('linked',include_notes=True,include_proxy_credentials=True)
        files=body_images(linked)
        assert 'presets/portable.yaml' in files and 'presets/portable/source.yaml' in files and 'presets/_taxonomy.yaml' in files
        assert any(name.startswith('presets/assets/') for name in files)
        for value in files.values(): assert b'private-user' not in value and b'private-password' not in value and b'private-notes-sentinel' not in value
        assert b'omitted_fields:' in files['presets/portable.yaml']
        assert b'|' in files['presets/portable/source.yaml'] and b'template:' in files['presets/portable/source.yaml']
        sensitive_files=body_images(sensitive)
        assert b'private-password' in sensitive_files['presets/portable.yaml'] and b'private-notes-sentinel' in sensitive_files['presets/portable/source.yaml']
        source_icon=a.request(cfg['effective']['icon'])
        a.stop()
        b=Instance(directory/'b')
        unresolved=b.preview(linked);assert not unresolved['preview']['ready'];b.execute(unresolved,status=400)
        preview=b.preview(linked,[{'kind':'Vendor','key':'portable','proxy':{'mode':'direct'}}]);assert preview['preview']['ready'];b.execute(preview,trust=False,status=400)
        result=b.execute(preview);assert b.execute(preview,trust=False)==result
        dest=b.request('/admin/api/apps/portable/source')['app'];assert not dest['enabled'] and dest['uid']!=source['uid'] and dest['source_epoch']==1
        dest_cfg=b.config_of('portable/source');assert dest_cfg['template_ref']=='openai/codex' and dest_cfg['overrides']==cfg['overrides']
        assert dest_cfg['effective']==cfg['effective']
        assert [(c['id'],c['name']['en']) for c in b.request('/admin/api/categories')['items']]==[('tools','Tools')]
        vendor=b.request('/admin/api/vendors/portable')['vendor'];assert hashlib.sha256(b.request(vendor['icon'])).hexdigest() in vendor['icon']
        independent_preview=b.preview(independent,[{'kind':'App','key':'portable/source','target_id':'independent'}]);assert independent_preview['preview']['ready'];b.execute(independent_preview)
        independent_cfg=b.config_of('portable/independent');assert independent_cfg['template_ref'] is None
        expected=dict(cfg['effective']);actual=dict(independent_cfg['effective']);expected.pop('icon');actual.pop('icon');assert expected==actual
        assert b.request(independent_cfg['effective']['icon'])==source_icon
        b.request('/admin/api/vendors',{'id':'target','name':name},status=201)
        target=b.request('/admin/api/vendors/target/configuration');b.request('/admin/api/vendors/target/configuration',{'revision':target['revision'],'set':{'proxy':{'mode':'url','url':'http://127.0.0.1:3129'}},'unset':[]},method='PATCH')
        note=b.request('/admin/api/apps/portable/source/admin-notes');b.request('/admin/api/apps/portable/source/admin-notes',{'revision':note['revision'],'text':'copy-note'},method='PUT')
        note=b.request('/admin/api/apps/portable/source/admin-notes')
        for mode in ['linked','independent']:
            copied=b.request('/admin/api/apps/portable/source/copy',{'source_uid':dest['uid'],'source_revision':dest_cfg['revision'],'target_vendor':'target','target_id':mode,'mode':mode,'include_notes':mode=='independent','notes_revision':note['revision']},status=201)['app']
            assert copied['uid']!=dest['uid'] and not copied['enabled'] and copied['source_epoch']==1
            copied_cfg=b.config_of('target/'+mode);assert copied_cfg['effective']['proxy']=={'mode':'inherit'} and copied_cfg['proxy_effective']['source_id']=='target'
            assert (copied_cfg['template_ref'] is not None)==(mode=='linked')
            assert copied_cfg['effective']['categories']==['tools'] and copied_cfg['effective']['tags']==['CLI','命令行']
            copied_note=b.request('/admin/api/apps/target/'+mode+'/admin-notes');assert copied_note['text']==('copy-note' if mode=='independent' else '')
            with sqlite3.connect(directory/'b/data/state.sqlite') as db:
                assert db.execute('SELECT count(*) FROM prewarm_jobs WHERE app_uid=?',(copied['uid'],)).fetchone()[0]==0
                assert db.execute('SELECT count(*) FROM download_sketches WHERE app_uid=?',(copied['uid'],)).fetchone()[0]==0
                assert db.execute('SELECT count(*) FROM hosted_files WHERE app_uid=?',(copied['uid'],)).fetchone()[0]==0
        pending=b.preview(linked)
        b.stop();b.start();b.login()
        b.execute(pending,status=409)
        current=b.config_of('portable/source')
        with sqlite3.connect(directory/'b/data/state.sqlite') as db:
            counts=db.execute('SELECT count(*) FROM applications').fetchone()[0]
        assert b.execute(preview,trust=False)==result
        assert b.config_of('portable/source')==current
        with sqlite3.connect(directory/'b/data/state.sqlite') as db:
            assert db.execute('SELECT count(*) FROM applications').fetchone()[0]==counts
        csrf=b.csrf;b.csrf=''
        b.execute(preview,status=403)
        b.csrf=csrf
        b.request('/admin/api/logout',{},method='POST')
        b.execute(preview,status=401)
        b.login()
        assert b.execute(preview,trust=False)==result
        with sqlite3.connect(directory/'b/data/state.sqlite') as db:
            assert db.execute('SELECT count(*) FROM configuration_import_receipts WHERE id=?',(preview['id'],)).fetchone()[0]==1
        public=b.request('/api/bootstrap');assert 'private-password' not in json.dumps(public) and 'copy-note' not in json.dumps(public)
        print('PASS native A→B linked/independent ZIP, static assets, categories/tags, sensitive filters, explicit proxy/trust, replay, cross-vendor copy and receipt restart/session isolation')
    finally:
        a.stop()
        if b is not None:b.stop()
