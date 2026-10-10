#!/usr/bin/env python3
"""Publish only a verified installer bundle as a draft PR; never execute its contents."""
import argparse
import base64
import json
from installer_bundle import load_bundle
import os
from pathlib import Path
import re
import subprocess
import tempfile
import urllib.error
import urllib.request
import zipfile

BRANCH = 'automation/installer-updates'
BOT = 'github-actions[bot]'
from installer_manifest import ROOT, allowed_paths
MARKER = re.compile(r'<!-- redapp-installer-update-head: ([0-9a-f]{40}) -->')
BASE_MARKER = re.compile(r'<!-- redapp-installer-update-base: ([0-9a-f]{40}) -->')



class GitHub:
    def __init__(self, repository, token):
        if repository != 'PMExtra/RedApp': raise ValueError('Unexpected target repository')
        self.repository,self.token=repository,token
    def request(self, method, path, body=None):
        request=urllib.request.Request('https://api.github.com/repos/'+self.repository+path,
            data=json.dumps(body).encode() if body is not None else None,method=method,
            headers={'Authorization':'Bearer '+self.token,'Accept':'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28','User-Agent':'RedApp-installer-maintenance','Content-Type':'application/json'})
        try:
            with urllib.request.urlopen(request,timeout=30) as response: return json.load(response)
        except urllib.error.HTTPError as error:
            if error.code==403:
                raise ValueError('GitHub denied the draft PR operation (HTTP 403). Check whether this repository permits GITHUB_TOKEN to create pull requests; no settings were changed.') from None
            raise ValueError(f'GitHub API {method} failed with HTTP {error.code}; no settings were changed') from None
    def open_prs(self): return self.request('GET','/pulls?state=open&head=PMExtra:'+BRANCH+'&base=main')
    def create(self,body): return self.request('POST','/pulls',{'title':'Update official installer baselines','head':BRANCH,'base':'main','draft':True,'body':body})
    def update(self,number,body): return self.request('PATCH',f'/pulls/{number}',{'body':body})


def git(root,*args,env=None,input=None):
    return subprocess.run(['git',*args],cwd=root,env=env,input=input,capture_output=True,check=True,timeout=90).stdout


def check_pr(pr,remote_head):
    matches=MARKER.findall(pr.get('body') or '')
    bases=BASE_MARKER.findall(pr.get('body') or '')
    if not pr.get('draft') or pr.get('user',{}).get('login')!=BOT or pr.get('head',{}).get('ref')!=BRANCH or pr.get('base',{}).get('ref')!='main':
        raise ValueError('Existing PR is not the managed draft; maintainer intervention is required')
    if len(matches)!=1 or matches[0]!=remote_head or pr['head']['sha']!=remote_head or len(bases)!=1:
        raise ValueError('Existing branch/PR changed outside the updater; refusing to overwrite it')
    return bases[0]


def body_for(payload,head,run_url):
    lines=['Updates official installer originals and generated files using the patches already reviewed on main.','',f"Baseline main: `{payload['baseline']}`",'', '| Script | Official source | Previous SHA256 | New SHA256 |','| --- | --- | --- | --- |']
    for row in payload['rows']:
        if row['status']=='changed':lines.append(f"| {row['application']}/{row['name']} | {row['url']} | `{row['baseline_sha256']}` | `{row['current_sha256']}` |")
    lines+=['','Validation: conflict-free zero-fuzz patch application (line offsets allowed), isolated offline Shell tests, a trusted file/digest recheck, and Windows PowerShell 7/5.1 parsing and harmless-executable tests of this exact candidate bundle.',
        'Windows AMD64 behavior was tested; Claude ARM64 selection was simulated. Native Windows ARM64, macOS, and official binary runtime were not tested.',
        f'Updater run: {run_url}',
        'PR CI for GITHUB_TOKEN-created updates may require manual approval. The required candidate Windows job ran before this PR was created; this does not claim a separate PR CI run.',
        'This PR stays a draft and is never automatically merged. Review source changes, generated diffs, licensing and platform behavior before merging. To make manual branch changes, take ownership first; subsequent automation will stop.',
        f"<!-- redapp-installer-update-base: {payload['baseline']} -->",f'<!-- redapp-installer-update-head: {head} -->']
    return '\n'.join(lines)+'\n'


def publish(root,payload,files,api,token,run_url,remote='origin'):
    baseline=payload['baseline']
    allowed=allowed_paths(root)
    if set(files)-allowed:raise ValueError('Unapproved installer publication paths')
    auth=base64.b64encode(('x-access-token:'+token).encode()).decode()
    env={**os.environ,'GIT_CONFIG_COUNT':'1','GIT_CONFIG_KEY_0':'http.https://github.com/.extraheader','GIT_CONFIG_VALUE_0':'AUTHORIZATION: basic '+auth,
        'GIT_AUTHOR_NAME':BOT,'GIT_AUTHOR_EMAIL':'41898282+github-actions[bot]@users.noreply.github.com','GIT_COMMITTER_NAME':BOT,'GIT_COMMITTER_EMAIL':'41898282+github-actions[bot]@users.noreply.github.com'}
    refs={line.split()[1]:line.split()[0] for line in git(root,'ls-remote',remote,'refs/heads/main','refs/heads/'+BRANCH,env=env).decode().splitlines()}
    if refs.get('refs/heads/main')!=baseline:raise ValueError('Remote main advanced; rerun the check on the new baseline')
    remote_head=refs.get('refs/heads/'+BRANCH)
    prs=api.open_prs()
    if len(prs)>1 or bool(remote_head)!=bool(prs):raise ValueError('Unowned branch or inconsistent PR state; refusing to replace it')
    pr=prs[0] if prs else None
    if pr:
        previous_base=check_pr(pr,remote_head)
        git(root,'fetch','--no-tags',remote,BRANCH,env=env)
        changed=set(git(root,'diff','--name-only',previous_base,remote_head).decode().splitlines())
        if changed-allowed:raise ValueError('Existing branch contains non-installer changes')
        if previous_base==baseline and changed==set(files):
            identical=True
            for name,data in files.items():
                old=git(root,'show',remote_head+':'+name)
                if name.endswith('/provenance.json'):
                    old_json,new_json=json.loads(old),json.loads(data)
                    for value in (old_json,new_json):value.get('script_baseline',{}).pop('checked_at',None)
                    identical=identical and old_json==new_json
                else:identical=identical and old==data
            if identical:
                print('Managed draft already contains this validated update; no new commit')
                return pr
    with tempfile.TemporaryDirectory(prefix='redapp-publish-') as tmp:
        index=Path(tmp)/'index'
        index_env={**env,'GIT_INDEX_FILE':str(index)}
        git(root,'read-tree',baseline,env=index_env)
        for name,data in sorted(files.items()):
            blob=git(root,'hash-object','-w','--stdin',env=index_env,input=data).decode().strip()
            mode=git(root,'ls-tree',baseline,'--',name).decode().split()[0]
            if mode not in ('100644','100755'):raise ValueError('Baseline path is not a regular audited file')
            git(root,'update-index','--add','--cacheinfo',f'{mode},{blob},{name}',env=index_env)
        tree=git(root,'write-tree',env=index_env).decode().strip()
        if remote_head and git(root,'rev-parse',remote_head+'^{tree}').decode().strip()==tree:
            print('Managed draft already contains this validated update; no new commit')
            return pr
        parents=['-p',remote_head] if remote_head else ['-p',baseline]
        if remote_head and remote_head!=baseline:parents+=['-p',baseline]
        head=git(root,'commit-tree',tree,*parents,env=env,input=b'Update official installer baselines\n').decode().strip()
    # Compare again before the ordinary fast-forward push. Concurrent branch writes cause push rejection.
    now={line.split()[1]:line.split()[0] for line in git(root,'ls-remote',remote,'refs/heads/main','refs/heads/'+BRANCH,env=env).decode().splitlines()}
    if now!=refs:raise ValueError('Remote refs changed during publication; refusing to push')
    git(root,'push',remote,head+':refs/heads/'+BRANCH,env=env)
    body=body_for(payload,head,run_url)
    result=api.update(pr['number'],body) if pr else api.create(body)
    print('Draft PR: '+result['html_url'])
    return result


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('bundle',type=Path);parser.add_argument('--sha256',required=True);parser.add_argument('--baseline',required=True)
    args=parser.parse_args()
    payload,files=load_bundle(args.bundle,args.sha256,args.baseline)
    repo=os.environ['GITHUB_REPOSITORY'];token=os.environ['GH_TOKEN']
    run_url='https://github.com/'+repo+'/actions/runs/'+os.environ['GITHUB_RUN_ID']
    publish(Path(__file__).resolve().parents[1],payload,files,GitHub(repo,token),token,run_url)

if __name__=='__main__':
    try:main()
    except subprocess.CalledProcessError as error:
        # Subprocess output can contain HTTP credentials; never copy it into public logs.
        raise SystemExit(f'Installer publication git operation failed (exit {error.returncode}); inspect repository permissions or branch changes. No force push was used.')
    except (OSError,ValueError,KeyError,zipfile.BadZipFile) as error:raise SystemExit('Installer publication failed: '+str(error))
