#!/usr/bin/env python3
"""Prove actual image update, business failure, manual and recovered timed rollback."""
import json,os,subprocess,time
from datetime import datetime,timezone
from pathlib import Path
import urllib.request
ROOT=Path(__file__).resolve().parent.parent
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
checks=[];token='';evidence={}
def api(path,data=None):
 global token
 req=urllib.request.Request(BASE+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def session():
 end=time.monotonic()+30
 while time.monotonic()<end:
  try:return api('/api/session')['token']
  except OSError:time.sleep(.5)
 raise AssertionError('control session did not recover')
def action(name):return api('/api/action',{'action':name})
def k(*args):return subprocess.run(K+list(args),check=True,capture_output=True,text=True,timeout=120).stdout
def wait(name,predicate,seconds=90):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  try:
   s=api('/api/state')
   if not s.get('cluster'):
    time.sleep(.5);continue
   if predicate(s):checks.append(name);print('PASS',name,flush=True);return s
  except (OSError,ValueError,KeyError):pass
  time.sleep(1)
 raise AssertionError(name)
def stamp(raw):return datetime.fromisoformat(raw.replace('Z','+00:00')).timestamp()
def ready(s,image):
 d=next((d for d in s['cluster']['deployments'] if d['name']=='echo'),{})
 pods=[p for p in s['cluster']['pods'] if p['component']=='echo' and p['phase']=='Running']
 return d.get('images')==[image] and d.get('ready',0)==d.get('replicas',-1) and bool(pods) and all(p['images']==[image] and p['ready']==p['containers'] and bool(p.get('image_ids')) and all(p['image_ids']) for p in pods)
def response(s,status,after,image):
 names={p['name'] for p in s['cluster']['pods'] if p['component']=='echo' and p['images']==[image]}
 return any(e['kind']=='http' and e['to']=='echo' and e['status']==status and stamp(e['time'])>after and e.get('target_pod') in names for e in s['events'])
def digests(s,image):return sorted({v for p in s['cluster']['pods'] if p['component']=='echo' and p['phase']=='Running' and p['ready']==p['containers'] and p['images']==[image] for v in p.get('image_ids',[]) if v})
token=session()
try:
 initial=api('/api/state');images=initial['self']['release_images'];base,candidate=images['current'],images['candidate']
 assert base.startswith('systems-lab:') and candidate==base+'-demo-fault'
 action('rollback-demo')
 api('/api/config',{'rps':5,'concurrency':4,'delay_ms':10,'error_percent':0,'cpu_ms':0,'duration_seconds':180})
 start=time.time();s=wait('baseline image serves actual HTTP 200',lambda s:ready(s,base) and response(s,200,start,base))
 evidence['base']=digests(s,base)
 start=time.time();action('release-demo')
 s=wait('new immutable image: ready pods serve real HTTP 503',lambda s:ready(s,candidate) and response(s,503,start,candidate))
 evidence['candidate']=digests(s,candidate)
 assert evidence['base'] and evidence['candidate'] and not(set(evidence['base'])&set(evidence['candidate'])),{'error':'current ready image identity did not change','evidence':evidence}
 start=time.time();action('rollback-demo')
 wait('manual rollback restores original image and fresh HTTP 200',lambda s:ready(s,base) and response(s,200,start,base))
 action('release-demo')
 s=wait('second faulty release is actually running',lambda s:ready(s,candidate))
 deadline=s['config']['release_until'];assert deadline>time.time()
 k('rollout','restart','deployment/control');k('rollout','status','deployment/control','--timeout=90s')
 token=session()
 s=api('/api/state');assert s['config']['release_until']==deadline,'control restart lost/extended recovery intent'
 checks.append('recovery deadline survives control restart');print('PASS',checks[-1],flush=True)
 wait('timed rollback restores base image and fresh HTTP 200 after controller restart',lambda s:ready(s,base) and s['config']['release_until']==0 and response(s,200,deadline,base),seconds=90)
finally:
 try:
  token=session();action('reset')
 finally:
  (ROOT/'.state/release-smoke-results.json').write_text(json.dumps({'checks':checks,'image_ids':evidence,'at':datetime.now(timezone.utc).isoformat()},indent=2))
print('PASS',len(checks),'release/rollback checks',flush=True)
