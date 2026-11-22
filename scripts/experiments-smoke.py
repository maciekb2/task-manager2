#!/usr/bin/env python3
"""Per-run isolation, actual A/B results, queue/job carriers and restart semantics."""
import json,os,subprocess,time
from pathlib import Path
from datetime import datetime
import urllib.request,urllib.error
ROOT=Path(__file__).resolve().parent.parent
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
token='';checks=[]
def api(path,data=None,auth=True):
 req=urllib.request.Request(BASE+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json',**({'X-Lab-Token':token} if auth else {})})
 with urllib.request.urlopen(req,timeout=25) as response:return json.load(response)
def wait(name,read,predicate,seconds=65):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  try:
   value=read()
   if predicate(value):checks.append(name);print('PASS',name,flush=True);return value
  except (OSError,ValueError,KeyError,IndexError):pass
  time.sleep(.5)
 raise AssertionError(name)
def record(id):return next(r for r in api('/api/experiments')['experiments'] if r['id']==id)
def command(action,id='',**kw):return api('/api/experiments',dict(action=action,id=id,**kw))
def phase(id,label,delay,duration=4):
 command(label,id,config={'rps':10,'concurrency':1,'delay_ms':delay,'cpu_ms':0,'error_percent':0,'payload_bytes':1024,'autoscale_until':time.time()+3600,'release_changed_at':time.time()+3600},duration_seconds=duration,replicas=2)
 return wait(label+' seals only its completed work',lambda:record(id),lambda r:r['phases'][-1]['state']=='completed' and r['phases'][-1]['summary']['complete'])['phases'][-1]
token=api('/api/session')['token']
# Clear a prior interrupted/aborted fixture through the same bounded reset.
api('/api/action',{'action':'reset'})
try:
 try:api('/api/experiments',{'action':'create'},auth=False)
 except urllib.error.HTTPError as e:assert e.code==403
 else:raise AssertionError('experiment mutation accepted without session')
 checks.append('new experiment mutations require session token')
 id=command('create',name='HTTP baseline vs delay')['active']
 for action in ['autoscale-baseline','autoscale-on','autoscale-off']:
  try:api('/api/action',{'action':action})
  except urllib.error.HTTPError as e:assert e.code==400
  else:raise AssertionError('global HPA action admitted during scoped experiment')
 assert api('/api/experiments')['active']==id and not record(id)['phases'] and api('/api/state')['config']['rps']==0
 checks.append('all HPA controls preserve a newly created active experiment')
 api('/api/action',{'action':'scale','target':'gateway','replicas':0})
 wait('gateway really stopped before phase preparation',lambda:api('/api/state'),lambda s:bool(s.get('cluster')) and any(d['name']=='gateway' and d['replicas']==0 and d['ready']==0 for d in s['cluster']['deployments']) and not any(p['component']=='gateway' and p['phase']=='Running' for p in s['cluster']['pods']))
 baseline=phase(id,'baseline',0)
 pods=json.loads(subprocess.check_output(K+['get','pods','-l','app.kubernetes.io/component=gateway','-o','json'],text=True,timeout=30))['items']
 started=datetime.fromisoformat(baseline['started'].replace('Z','+00:00'))
 ready=[c for p in pods if p['status']['phase']=='Running' and not p['metadata'].get('deletionTimestamp') for c in p['status']['conditions'] if c['type']=='Ready' and c['status']=='True']
 assert ready and all(datetime.fromisoformat(c['lastTransitionTime'].replace('Z','+00:00'))<=started for c in ready),'measurement started before the restored gateway was ready'
 assert baseline['summary']['count']>0,'zero-traffic baseline falsely accepted'
 assert baseline['config']['autoscale_until']==0 and baseline['config']['release_until']==0 and baseline['config']['release_changed_at']==0
 checks.append('phase configuration cannot supply server-owned rollout or HPA deadlines')
 checks.append('clock starts after restored gateway readiness, with actual successful work')
 variant=phase(id,'variant',400)
 assert baseline['id']!=variant['id']
 assert variant['summary']['mean_ms']>baseline['summary']['mean_ms']+200
 assert variant['summary']['rps']<baseline['summary']['rps']
 assert all(not p['resources'] for p in [baseline,variant]),'short CPU window fabricated'
 checks.append('distinct run IDs and real slower/lower-throughput variant; no invented CPU samples')
 result=api('/api/experiments/export?id='+id)
 assert result['schema']=='systems-lab-experiment-v1' and len(result['experiment']['phases'])==2
 for p in result['experiment']['phases']:
  assert p['traces'] and all(e.get('run_id')==p['id'] for e in p['timeline'])
  events=api('/api/trace?id='+p['traces'][-1])
  assert {'generator','gateway','validator','enricher'}<={e['from'] for e in events if e['kind']=='http' and e.get('run_id')==p['id']}
 checks.append('export and all four actual HTTP hops preserve phase scope')
 scope=command('create',name='Queue and Job correlation')['active']
 command('baseline',scope,config={'rps':0,'concurrency':2,'delay_ms':10,'cpu_ms':0,'error_percent':0},duration_seconds=40,replicas=2)
 current=wait('queue/job measurement starts after readiness',lambda:record(scope),lambda r:r['phases'][-1]['state']=='running')
 run=current['phases'][-1]['id']
 task=api('/api/action',{'action':'enqueue','count':1,'failures':1})['ids'][0]
 job=api('/api/action',{'action':'job','failures':0})['name']
 state=wait('owned queue retry and native Job actually complete',lambda:api('/api/state'),lambda s:any(t['id']==task and t['state']=='completed' and t['run_id']==run for t in s['queue']['tasks']) and any(j['name']==job and j['succeeded']>0 and j['run_id']==run for j in s['cluster']['jobs']))
 t=next(t for t in state['queue']['tasks'] if t['id']==task)
 native=wait('standard OTel spans carry run ID through producer/consumer',lambda:api('/api/otel/trace?id='+t['trace']),lambda d:bool(d.get('data')) and any(tag['key']=='lab.run.id' and tag['value']==run for span in d['data'][0]['spans'] for tag in span.get('tags',[])))
 command('finish',scope)
 r=wait('drain includes owned messages and jobs before sealing',lambda:record(scope),lambda r:r['phases'][-1]['state']=='completed' and r['phases'][-1]['summary']['complete'])
 assert r['phases'][-1]['summary']['count']>=3,'lost retry or Job root'
 command('close',scope)
 partial=command('create',name='Explicit restart interruption')['active']
 command('baseline',partial,config={'rps':5,'concurrency':2,'delay_ms':0,'cpu_ms':0,'error_percent':0},duration_seconds=60,replicas=2)
 wait('interruption fixture is running',lambda:record(partial),lambda r:r['phases'][-1]['state']=='running')
 api('/api/action',{'action':'cron','suspended':False,'schedule':'* * * * *'})
 wait('scoped CronJob really enabled before interruption',lambda:api('/api/state'),lambda s:bool(s.get('cluster')) and any(not c['suspended'] for c in s['cluster']['cronjobs']))
 subprocess.run(K+['rollout','restart','deployment/control'],check=True,capture_output=True,timeout=30)
 subprocess.run(K+['rollout','status','deployment/control','--timeout=90s'],check=True,capture_output=True,timeout=100)
 wait('interrupted run is explicit; sealed earlier results survive restart',lambda:api('/api/experiments'),lambda d:any(r['id']==partial and r['state']=='interrupted' and r['phases'][-1]['partial'] for r in d['experiments']) and any(r['id']==id and r['state']=='completed' for r in d['experiments']))
 token=api('/api/session')['token']
 assert api('/api/state')['config']['rps']==0
 wait('interrupted run stops both HTTP and its scheduled producer',lambda:api('/api/state'),lambda s:s['config']['rps']==0 and not s['config'].get('run_id') and bool(s.get('cluster')) and all(c['suspended'] for c in s['cluster']['cronjobs']))
finally:
 token=api('/api/session')['token'];api('/api/action',{'action':'reset'})
 (ROOT/'.state/experiments-smoke-results.json').write_text(json.dumps({'checks':checks,'at':time.time()},indent=2))
print('PASS',len(checks),'experiment checks',flush=True)
