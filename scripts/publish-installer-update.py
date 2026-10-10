#!/usr/bin/env python3
"""Publish only a verified installer bundle as one draft PR per application; never execute its contents."""
import argparse
import base64
import hashlib
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

PREFIX = 'automation/installer-updates/'
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
    def open_prs(self): return self.request('GET','/pulls?state=open&base=main&per_page=100')
    def create(self,app,branch,body): return self.request('POST','/pulls',{'title':'Update official '+app+' installers','head':branch,'base':'main','draft':True,'body':body})
    def close(self,number,comment):
        self.request('POST',f'/issues/{number}/comments',{'body':comment})
        return self.request('PATCH',f'/pulls/{number}',{'state':'closed'})


def git(root,*args,env=None,input=None):
    return subprocess.run(['git',*args],cwd=root,env=env,input=input,capture_output=True,check=True,timeout=90).stdout


def check_pr(pr,repository):
    matches=MARKER.findall(pr.get('body') or '')
    bases=BASE_MARKER.findall(pr.get('body') or '')
    if not pr.get('draft') or pr.get('user',{}).get('login')!=BOT or (pr.get('head',{}).get('repo') or {}).get('full_name')!=repository or pr.get('base',{}).get('ref')!='main':
        raise ValueError('Existing PR is not the managed draft; maintainer intervention is required')
    if len(matches)!=1 or matches[0]!=pr['head']['sha'] or len(bases)!=1:
        raise ValueError('Existing branch/PR changed outside the updater; refusing to close it')
    return bases[0]


def body_for(payload,app,head,run_url):
    lines=['Updates official installer originals and generated files using the patches already reviewed on main.','',f"Baseline main: `{payload['baseline']}`",'', '| Script | Official source | Previous SHA256 | New SHA256 |','| --- | --- | --- | --- |']
    for row in payload['rows']:
        if row['status']=='changed' and row['application']==app:lines.append(f"| {row['application']}/{row['name']} | {row['url']} | `{row['baseline_sha256']}` | `{row['current_sha256']}` |")
    lines+=['','Validation: conflict-free zero-fuzz patch application (line offsets allowed), isolated offline Shell tests, a trusted file/digest recheck, and Windows PowerShell 7/5.1 parsing and harmless-executable tests of this exact candidate bundle.',
        'Windows AMD64 behavior was tested; Claude ARM64 selection was simulated. Native Windows ARM64, macOS, and official binary runtime were not tested.',
        f'Updater run: {run_url}',
        'PR CI for GITHUB_TOKEN-created updates may require manual approval. The required candidate Windows job ran before this PR was created; this does not claim a separate PR CI run.',
        'This PR stays a draft and is never automatically merged. A newer official update for this application replaces it: the updater opens a new draft and closes this one. Review source changes, generated diffs, licensing and platform behavior before merging. To make manual branch changes, take ownership first; subsequent automation for this application will stop.',
        f"<!-- redapp-installer-update-base: {payload['baseline']} -->",f'<!-- redapp-installer-update-head: {head} -->']
    return '\n'.join(lines)+'\n'


def fetch(root,remote,ref,sha,env):
    git(root,'fetch','--no-tags',remote,'refs/heads/'+ref,env=env)
    if git(root,'rev-parse','FETCH_HEAD').decode().strip()!=sha:raise ValueError('Branch '+ref+' changed outside the updater; refusing to replace it')


def branch_for(app,files):
    # Named by script content only, so provenance check times do not create new PRs.
    content=hashlib.sha256()
    for name,data in sorted(files.items()):
        if not name.endswith('/provenance.json'):content.update(name.encode()+b'\0'+hashlib.sha256(data).digest())
    return PREFIX+app+'/'+content.hexdigest()[:12]


def publish(root,payload,files,api,token,run_url,remote='origin'):
    baseline=payload['baseline']
    allowed=allowed_paths(root)
    if set(files)-allowed:raise ValueError('Unapproved installer publication paths')
    auth=base64.b64encode(('x-access-token:'+token).encode()).decode()
    env={**os.environ,'GIT_CONFIG_COUNT':'1','GIT_CONFIG_KEY_0':'http.https://github.com/.extraheader','GIT_CONFIG_VALUE_0':'AUTHORIZATION: basic '+auth,
        'GIT_AUTHOR_NAME':BOT,'GIT_AUTHOR_EMAIL':'41898282+github-actions[bot]@users.noreply.github.com','GIT_COMMITTER_NAME':BOT,'GIT_COMMITTER_EMAIL':'41898282+github-actions[bot]@users.noreply.github.com'}
    def refs(*names):
        return {line.split()[1]:line.split()[0] for line in git(root,'ls-remote',remote,'refs/heads/main',*names,env=env).decode().splitlines()}
    if refs().get('refs/heads/main')!=baseline:raise ValueError('Remote main advanced; rerun the check on the new baseline')
    apps=sorted({row['application'] for row in payload['rows'] if row['status']=='changed'})
    if {name.split('/',3)[1]+'/'+name.split('/',3)[2] for name in files}-set(apps):raise ValueError('Bundle files do not match the changed applications')
    # Fork PRs can reuse the branch name; they are never managed drafts and must not block updates.
    prs=[pr for pr in api.open_prs() if pr.get('head',{}).get('ref','').startswith(PREFIX) and (pr['head'].get('repo') or {}).get('full_name')==api.repository]
    return [publish_app(root,payload,app,{k:v for k,v in files.items() if k.startswith('installers/'+app+'/')},
                        [pr for pr in prs if pr['head']['ref'].startswith(PREFIX+app+'/')],allowed,api,run_url,remote,env,refs) for app in apps]


def publish_app(root,payload,app,files,prs,allowed,api,run_url,remote,env,refs):
    baseline=payload['baseline'];branch=branch_for(app,files)
    owned=[path for path in allowed if path.startswith('installers/'+app+'/')]
    for pr in prs:
        previous_base=check_pr(pr,api.repository)
        fetch(root,remote,pr['head']['ref'],pr['head']['sha'],env)
        if set(git(root,'diff','--name-only',previous_base,pr['head']['sha']).decode().splitlines())-set(owned):
            raise ValueError('Existing branch contains non-installer changes')
    current=next((pr for pr in prs if pr['head']['ref']==branch),None)
    if current:
        print(app+': managed draft already contains this validated update; no new commit')
    else:
        with tempfile.TemporaryDirectory(prefix='redapp-publish-') as tmp:
            index_env={**env,'GIT_INDEX_FILE':str(Path(tmp)/'index')}
            git(root,'read-tree',baseline,env=index_env)
            for name,data in sorted(files.items()):
                blob=git(root,'hash-object','-w','--stdin',env=index_env,input=data).decode().strip()
                mode=git(root,'ls-tree',baseline,'--',name).decode().split()[0]
                if mode not in ('100644','100755'):raise ValueError('Baseline path is not a regular audited file')
                git(root,'update-index','--add','--cacheinfo',f'{mode},{blob},{name}',env=index_env)
            tree=git(root,'write-tree',env=index_env).decode().strip()
        now=refs('refs/heads/'+branch);head=now.get('refs/heads/'+branch)
        if now.get('refs/heads/main')!=baseline:raise ValueError('Remote main advanced; rerun the check on the new baseline')
        if head:
            # A previous run pushed this branch but could not open its PR.
            fetch(root,remote,branch,head,env)
            if git(root,'rev-parse',head+'^{tree}').decode().strip()!=tree or git(root,'rev-parse',head+'^').decode().strip()!=baseline:
                raise ValueError('Unowned branch '+branch+' differs from this update; refusing to replace it')
        else:
            head=git(root,'commit-tree',tree,'-p',baseline,env=env,input=b'Update official '+app.encode()+b' installers\n').decode().strip()
            # The empty lease rejects the push if the branch appeared concurrently; no force push.
            git(root,'push','--force-with-lease=refs/heads/'+branch+':',remote,head+':refs/heads/'+branch,env=env)
        current=api.create(app,branch,body_for(payload,app,head,run_url))
        print(app+' draft PR: '+current['html_url'])
    for pr in prs:
        if pr is current:continue
        api.close(pr['number'],f"Superseded by #{current['number']}, which carries a newer official update for {app}.")
        git(root,'push','--force-with-lease=refs/heads/'+pr['head']['ref']+':'+pr['head']['sha'],remote,':refs/heads/'+pr['head']['ref'],env=env)
        print(app+': closed superseded draft #'+str(pr['number']))
    return current


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
