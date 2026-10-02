#!/usr/bin/env python3
"""Deterministic maintenance failures and local fast-forward draft publication; no upstream calls."""
import copy
import contextlib
import io
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zipfile
import installer_maintenance as m

spec=importlib.util.spec_from_file_location('publisher',Path(__file__).with_name('publish-installer-update.py'))
p=importlib.util.module_from_spec(spec);spec.loader.exec_module(p)

class CheckTests(unittest.TestCase):
    def original(self,url):
        item=next(x for x in m.inventory() if x['url']==url)
        return (m.ROOT/'installers'/item['application']/'upstream'/item['name']).read_bytes()
    def test_unchanged_and_one_changed(self):
        rows=m.inspect(fetch=self.original)
        self.assertEqual([r['status'] for r in rows],['unchanged']*4)
        def changed(url):return self.original(url)+(b'\n# new official comment\n' if url.endswith('/install.sh') and 'claude.ai' in url else b'')
        rows=m.inspect(fetch=changed)
        self.assertEqual(sum(r['status']=='changed' for r in rows),1)
    def test_all_errors_are_aggregated(self):
        calls=[]
        def fail(url):
            calls.append(url)
            if len(calls)==1:raise OSError('fixture network failure')
            if len(calls)==2:return b'<html>temporary server failure</html>'
            if len(calls)==3:raise ValueError('Unreviewed upstream redirect')
            return b''
        rows=m.inspect(fetch=fail)
        self.assertEqual(len(calls),4)
        self.assertTrue(all(r['status']=='error' for r in rows))
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):m.prepare(Path(tmp),fetch=fail)
            self.assertFalse((Path(tmp)/'sources').exists())
    def test_baseline_integrity_failure_is_not_a_no_change_result(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            shutil.copytree(m.ROOT/'installers',root/'installers')
            (root/'installers/codex/upstream/install.sh').write_bytes(b'#!/bin/sh\nmodified locally\n')
            rows=m.inspect(root=root,fetch=self.original)
            self.assertEqual(rows[0]['status'],'error')
            self.assertIn('audited digest',rows[0]['error'])
            self.assertEqual(len(rows),4)

    def test_redirects_and_shape_fail_closed(self):
        with self.assertRaises(ValueError):m.NoRedirect().redirect_request(None,None,None,None,None,None)
        for name,raw in [('install.sh',b'x'* (m.MAX_SCRIPT+1)),('install.sh',b'#! /bin/sh\0'),('install.ps1',b'<html>failure</html>')]:
            with self.assertRaises(ValueError):m.script_shape(name,raw)
    def test_only_the_reviewed_single_https_redirect_is_allowed(self):
        for source,target in m.REVIEWED_REDIRECTS.items():
            request=m.urllib.request.Request(source)
            handler=m.ReviewedRedirect(source)
            redirected=handler.redirect_request(request,None,302,'Found',{},target)
            self.assertEqual(redirected.full_url,target)
            with self.assertRaises(ValueError):handler.redirect_request(redirected,None,302,'Found',{},target)
            for rejected in (target+'?token=unexpected',target+'#fragment',target.replace('https:','http:'),target.replace('downloads.claude.ai','downloads.claude.ai.evil.example'),target.replace('/bootstrap.','/%62ootstrap.'),target.replace('downloads.claude.ai','user@downloads.claude.ai')):
                with self.assertRaises(ValueError):m.ReviewedRedirect(source).redirect_request(request,None,302,'Found',{},rejected)
            with self.assertRaises(ValueError):m.ReviewedRedirect(source).redirect_request(m.urllib.request.Request(target),None,302,'Found',{},target)
        source='https://releases.openai.com/codex/install.sh'
        with self.assertRaises(ValueError):m.ReviewedRedirect(source).redirect_request(m.urllib.request.Request(source),None,302,'Found',{},source)

    def test_strict_patch_generation_and_conflict(self):
        for app in m.APPS:
            for name in m.NAMES:
                root=m.ROOT/'installers'/app
                raw=(root/'upstream'/name).read_bytes()
                result=m.strict_patch(raw,root/'patches'/(name+'.patch'))
                self.assertEqual(result,(root/'generated'/name).read_bytes())
                with self.assertRaises((ValueError,subprocess.SubprocessError)):
                    m.strict_patch(b'\n'+raw,root/'patches'/(name+'.patch'))
    def test_isolated_test_failure_does_not_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp=Path(tmp);prepared=tmp/'prepared';m.prepare(prepared,fetch=self.original)
            original_run=m.run
            def failing(args,**kwargs):
                if args[0]=='python3':raise subprocess.CalledProcessError(1,args)
                return original_run(args,**kwargs)
            with patch.object(m,'run',side_effect=failing),self.assertRaises(subprocess.CalledProcessError):m.validate(prepared,tmp/'out')
            self.assertFalse(list((tmp/'out').glob('*')))
    def test_missing_powershell_parser_cannot_pass_validation(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp=Path(tmp);prepared=tmp/'prepared';m.prepare(prepared,fetch=self.original)
            original_run=m.run
            def only_patch(args,**kwargs):
                return original_run(args,**kwargs) if args[0]=='patch' else ''
            with patch.object(m,'run',side_effect=only_patch),patch.object(m.shutil,'which',return_value=None),self.assertRaisesRegex(ValueError,'PowerShell parser required'):
                m.validate(prepared,tmp/'out')
            self.assertFalse(list((tmp/'out').glob('*')))

    def test_readonly_package_rejects_changed_validation_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp=Path(tmp);prepared=tmp/'prepared';m.prepare(prepared,fetch=self.original)
            for app in m.APPS:shutil.copytree(m.ROOT/'installers'/app/'generated',tmp/'validated'/app)
            (tmp/'validated/codex/install.sh').write_text('tampered output')
            with self.assertRaises(ValueError):m.package(prepared,tmp/'validated',tmp/'bundle.zip')
            self.assertFalse((tmp/'bundle.zip').exists())

class FakeAPI:
    def __init__(self):self.pr=None;self.created=0;self.updated=0
    def open_prs(self):return [copy.deepcopy(self.pr)] if self.pr else []
    def create(self,body):
        self.created+=1
        self.pr={'number':1,'draft':True,'user':{'login':p.BOT},'head':{'ref':p.BRANCH,'sha':p.MARKER.search(body)[1]},'base':{'ref':'main'},'body':body,'html_url':'https://example.invalid/draft/1'}
        return self.pr
    def update(self,number,body):
        self.updated+=1;self.pr['body']=body;self.pr['head']['sha']=p.MARKER.search(body)[1];return self.pr

class PublishTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)/'repo';self.root.mkdir();self.remote=Path(self.temp.name)/'remote.git'
        p.git(self.root,'init','-b','main');p.git(self.root,'config','user.name','Fixture');p.git(self.root,'config','user.email','fixture@example.invalid')
        for name in sorted(p.ALLOWED):
            file=self.root/name;file.parent.mkdir(parents=True,exist_ok=True);file.write_text('{}\n' if name.endswith('.json') else 'old\n')
        p.git(self.root,'add','.');p.git(self.root,'commit','-m','baseline')
        p.git(self.root,'init','--bare',str(self.remote));p.git(self.root,'remote','add','origin',str(self.remote));p.git(self.root,'push','origin','main')
        self.baseline=p.git(self.root,'rev-parse','HEAD').decode().strip();self.api=FakeAPI()
        self.payload={'baseline':self.baseline,'rows':[]}
        for app in m.APPS:
            for name in m.NAMES:
                changed=app=='claude-code' and name=='install.sh'
                self.payload['rows'].append({'application':app,'name':name,'url':('https://claude.ai/' if app=='claude-code' else 'https://releases.openai.com/codex/')+name,'status':'changed' if changed else 'unchanged','baseline_sha256':m.digest(b'old\n'),'current_sha256':m.digest(b'new\n' if changed else b'old\n')})
        self.files={'installers/claude-code/upstream/install.sh':b'new\n','installers/claude-code/generated/install.sh':b'patched\n','installers/claude-code/provenance.json':b'{"script_baseline":{"checked_at":"first"}}\n'}
    def publish(self):return p.publish(self.root,self.payload,self.files,self.api,'fixture-token','https://example.invalid/run')
    def bundle(self,extras=None):
        path=Path(self.temp.name)/'bundle.zip'
        payload={**self.payload,'files':{name:m.digest(data) for name,data in self.files.items()}}
        with zipfile.ZipFile(path,'w') as z:
            for name,data in self.files.items():z.writestr(name,data)
            z.writestr('update.json',json.dumps(payload))
            for name,data in (extras or {}).items():z.writestr(name,data)
        return path,m.digest(path.read_bytes())
    def test_bundle_allowlist_and_digests(self):
        path,sha=self.bundle();payload,files=p.load_bundle(path,sha,self.baseline);self.assertEqual(files,self.files)
        with self.assertRaises(ValueError):p.load_bundle(path,'0'*64,self.baseline)
        path,sha=self.bundle({'../../script.py':b'no execution'})
        with self.assertRaises(ValueError):p.load_bundle(path,sha,self.baseline)
        self.files['installers/claude-code/patches/install.sh.patch']=b'unapproved'
        path,sha=self.bundle()
        with self.assertRaises(ValueError):p.load_bundle(path,sha,self.baseline)
    def test_create_idempotent_update_and_fast_forward(self):
        result=self.publish();first=result['head']['sha'];self.assertTrue(result['draft'])
        self.files['installers/claude-code/provenance.json']=b'{"script_baseline":{"checked_at":"tomorrow"}}\n'
        self.publish();self.assertEqual(self.api.created,1);self.assertEqual(self.api.updated,0)
        self.files['installers/claude-code/upstream/install.sh']=b'newer\n'
        self.publish();second=self.api.pr['head']['sha'];self.assertNotEqual(first,second)
        p.git(self.root,'merge-base','--is-ancestor',first,second)
        self.assertEqual(p.git(self.root,'ls-remote','origin','refs/heads/main').decode().split()[0],self.baseline)
    def test_manual_head_change_is_not_overwritten(self):
        self.publish();head=self.api.pr['head']['sha'];tree=p.git(self.root,'rev-parse',head+'^{tree}').decode().strip()
        manual=p.git(self.root,'commit-tree',tree,'-p',head,input=b'manual change\n').decode().strip()
        p.git(self.root,'push','origin',manual+':refs/heads/'+p.BRANCH)
        with self.assertRaisesRegex(ValueError,'changed outside'):self.publish()
        self.assertEqual(self.api.updated,0)
    def test_unowned_branch_nondraft_and_main_advance_stop(self):
        self.publish();self.api.pr['draft']=False
        with self.assertRaises(ValueError):self.publish()
        self.api.pr=None
        with self.assertRaises(ValueError):self.publish()
        p.git(self.root,'commit','--allow-empty','-m','main advanced');p.git(self.root,'push','origin','main')
        with self.assertRaisesRegex(ValueError,'main advanced'):self.publish()
    def test_branch_with_code_change_stops(self):
        self.publish();head=self.api.pr['head']['sha']
        p.git(self.root,'checkout','-b','manual',head)
        (self.root/'not-allowed.py').write_text('do not publish')
        p.git(self.root,'add','.');p.git(self.root,'commit','-m','extra code');manual=p.git(self.root,'rev-parse','HEAD').decode().strip()
        p.git(self.root,'push','origin',manual+':refs/heads/'+p.BRANCH)
        self.api.pr['head']['sha']=manual;self.api.pr['body']=self.api.pr['body'].replace(head,manual)
        with self.assertRaisesRegex(ValueError,'non-installer'):self.publish()

if __name__=='__main__':
    for name in ('GITHUB_OUTPUT', 'GITHUB_STEP_SUMMARY'):
        os.environ.pop(name, None)
    with contextlib.redirect_stdout(io.StringIO()): unittest.main(verbosity=2)
