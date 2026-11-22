#!/usr/bin/env python3
"""Native CPU HPA decisions, ready replicas, measured effects and bounded recovery."""
import json,os,subprocess,time,urllib.request
from datetime import datetime
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
token='';checks=[];measurements={}
def api(path,body=None):
 req=urllib.request.Request(BASE+path,data=None if body is None else json.dumps(body).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def k(*args):return subprocess.check_output(K+list(args),text=True,timeout=120)
def wait(name,predicate,seconds=60):
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
def echo(s):return next(d for d in s['cluster']['deployments'] if d['name']=='echo')
def hpa(s):return next((h for h in s['cluster'].get('autoscalers',[]) if h['name']=='echo'),{})
def stamp(t):return datetime.fromisoformat(t.replace('Z','+00:00')).timestamp()
def measure(after,before):
 rows=[p for p in api('/api/performance?minutes=5')['traffic_series'] if stamp(p['at'])-5>=after and stamp(p['at'])<=before]
 assert rows,'no fully-contained HTTP buckets'
 return {'window_seconds':len(rows)*5,'requests':sum(p['requests'] for p in rows),'errors':sum(p['errors'] for p in rows),'rps':sum(p['requests'] for p in rows)/(len(rows)*5),'bucket_p95_ms':[p['p95_ms'] for p in rows]}
token=api('/api/session')['token']
try:
 api('/api/action',{'action':'reset'})
 api('/api/action',{'action':'scale','target':'echo','replicas':1})
 wait('single ready replica establishes a native scaling baseline',lambda s:echo(s)['replicas']==1 and echo(s)['ready']==1)
 api('/api/config',{'rps':50,'concurrency':20,'delay_ms':0,'cpu_ms':50,'error_percent':0,'payload_bytes':0,'duration_seconds':300})
 baseline=time.time()+2
 time.sleep(24)
 measurements['one_replica']=measure(baseline,time.time())
 api('/api/action',{'action':'autoscale-on'})
 s=wait('HPA resource confirms fixed CPU target and 1–5 replica limits',lambda s:bool(hpa(s)) and hpa(s)['min_replicas']==1 and hpa(s)['max_replicas']==5)
 deadline=s['config']['autoscale_until'];assert 250<deadline-time.time()<=301
 s=wait('native HPA CPU decision produces additional ready echo replicas',lambda s:hpa(s).get('desired_replicas',0)>1 and echo(s)['ready']>1 and bool(hpa(s).get('current_metrics')),seconds=210)
 assert any(c['type']=='ScalingActive' and c['status']=='True' for c in hpa(s)['conditions']),hpa(s)
 assert echo(s)['replicas']<=5 and hpa(s)['desired_replicas']<=5
 measurements['hpa']=hpa(s);measurements['echo']=echo(s)
 after=time.time();time.sleep(24)
 measurements['scaled']=measure(after,time.time())
 checks.append('before/after uses actual completed HTTP buckets; no promised speedup or per-request CPU claim')
 print('MEASURED',json.dumps(measurements),flush=True)
 # Preserve the actual controller deadline across ordinary API updates.
 config=api('/api/state')['config'];config.update(rps=0)
 api('/api/config',config);assert api('/api/state')['config']['autoscale_until']==deadline
 checks.append('traffic configuration changes cannot overwrite server-owned HPA recovery deadline')
 # Inject an expired persisted deadline as an operator fixture, then really restart
 # control: verifies recovery of an already-expired demo without a five-minute sleep.
 raw=json.loads(k('get','configmap','lab-config','-o','json'))
 print('CONFIG_KEYS',list(raw['data']),flush=True)
 key=next(key for key,value in raw['data'].items() if value.strip().startswith('{'))
 config=json.loads(raw['data'][key]);config['autoscale_until']=time.time()-1
 k('patch','configmap','lab-config','--type=merge','-p',json.dumps({'data':{key:json.dumps(config)}}))
 k('rollout','restart','deployment/control');k('rollout','status','deployment/control','--timeout=90s')
 wait('expired persisted demo removes HPA and restores 2 ready pods after control restart',lambda s:not hpa(s) and s['config'].get('autoscale_until',0)==0 and echo(s)['ready']==2 and echo(s)['replicas']==2,seconds=90)
 token=api('/api/session')['token']
finally:
 token=api('/api/session')['token'];api('/api/action',{'action':'reset'})
 (ROOT/'.state/autoscaling-smoke-results.json').write_text(json.dumps({'checks':checks,'measurements':measurements,'at':time.time()},indent=2))
print('PASS',len(checks),'autoscaling checks',flush=True)
