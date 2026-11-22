#!/usr/bin/env python3
"""Regression acceptance for bounded generator, independent history and control outage."""
import json,os,subprocess,time
from datetime import datetime,timezone
from pathlib import Path
import urllib.request
ROOT=Path(__file__).resolve().parent.parent
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
checks=[];token=''
def api(path,data=None):
 req=urllib.request.Request(BASE+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def k(*args):return subprocess.run(K+list(args),check=True,capture_output=True,text=True,timeout=100).stdout
def wait(name,read,predicate,seconds=45):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  try:
   value=read()
   if predicate(value):print('PASS',name,flush=True);checks.append(name);return value
  except (OSError,ValueError,KeyError):pass
  time.sleep(.5)
 raise AssertionError(name)
def config(rps,duration=45):return api('/api/config',{'rps':rps,'concurrency':8,'delay_ms':0,'cpu_ms':0,'error_percent':0,'duration_seconds':duration})
def stamp(t):return datetime.fromisoformat(t.replace('Z','+00:00')).timestamp()
token=api('/api/session')['token']
try:
 # Repeat short runs at different wall-clock fractions; each must emit real work.
 for i in range(3):
  started=time.time();c=config(1,1)
  assert 0.6<c['run_until']-started<1.3,c
  assert c['run_until']%1!=0,c
  wait('one-second / one-RPS run emits a fresh completed HTTP request',lambda:api('/api/state'),lambda s:any(e['kind']=='http' and e['from']=='generator' and e['status']==200 and stamp(e['time'])>started for e in s['events']),seconds=8)
  config(0);time.sleep(.2)
 started=time.time();config(10,90)
 wait('fresh traffic before control interruption',lambda:api('/api/state'),lambda s:any(e['kind']=='http' and e['from']=='generator' and stamp(e['time'])>started for e in s['events']))
 k('scale','deployment/control','--replicas=0')
 k('wait','--for=delete','pod','-l','app.kubernetes.io/component=control','--timeout=60s')
 outage=time.time();time.sleep(12);restored=time.time()
 k('scale','deployment/control','--replicas=1');k('rollout','status','deployment/control','--timeout=90s')
 wait('control API recovered',lambda:api('/api/session'),lambda s:bool(s.get('token')))
 token=api('/api/session')['token']
 def buckets(r):return [p for p in r['traffic_series'] if stamp(p['at'])-5>=outage and stamp(p['at'])<=restored]
 report=wait('HTTP continues during real control outage, recovered from event stream',lambda:api('/api/performance?minutes=1'),lambda r:bool(buckets(r)) and all(p['requests']>=25 for p in buckets(r)))
 state=api('/api/state');assert state['config']['run_until']<started+92,'restart extended traffic deadline'
 wait('topology retains independently aggregated observed traffic',lambda:api('/api/state'),lambda s:s['edges'].get('generator→gateway',{}).get('count',0)>120)
 config(0)
finally:
 k('scale','deployment/control','--replicas=1');k('rollout','status','deployment/control','--timeout=90s')
 token=api('/api/session')['token']
 api('/api/config',{'rps':0,'concurrency':4,'delay_ms':40,'cpu_ms':0,'error_percent':0})
 (ROOT/'.state/foundation-smoke-results.json').write_text(json.dumps({'checks':checks,'at':datetime.now(timezone.utc).isoformat()},indent=2))
print('PASS',len(checks),'foundation checks',flush=True)
