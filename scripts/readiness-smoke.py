#!/usr/bin/env python3
"""New pods use the persisted scenario; consumer recovers an actually deleted durable."""
import base64,json,os,subprocess,time,urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
token='';checks=[]
def api(path,body=None):
 req=urllib.request.Request(BASE+path,data=None if body is None else json.dumps(body).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def k(*args):return subprocess.check_output(K+list(args),text=True,timeout=120)
def wait(predicate,seconds=45):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  s=api('/api/state')
  if predicate(s):return s
  time.sleep(.5)
 raise AssertionError('acceptance timed out')
token=api('/api/session')['token']
try:
 api('/api/action',{'action':'reset'})
 api('/api/config',{'rps':5,'concurrency':4,'delay_ms':100,'cpu_ms':0,'error_percent':100,'duration_seconds':90})
 k('rollout','restart','deployment/echo');k('rollout','status','deployment/echo','--timeout=90s')
 actual=json.loads(k('get','pods','-l','app.kubernetes.io/component=echo','-o','json'))
 names={p['metadata']['name'] for p in actual['items'] if p['status']['phase']=='Running' and not p['metadata'].get('deletionTimestamp')}
 assert len(names)==2,names
 s=wait(lambda s:names<={p['name'] for p in s['cluster']['pods'] if p['component']=='echo' and p['phase']=='Running' and p['ready']==p['containers']})
 s=wait(lambda s:any(e['kind']=='http' and e['to']=='echo' and e.get('target_pod') in names for e in s['events']))
 fresh=[e for e in s['events'] if e['kind']=='http' and e['to']=='echo' and e.get('target_pod') in names]
 assert fresh and all(e['status']==503 for e in fresh),fresh
 checks.append('new worker pods serve persisted 100% failure scenario from their first observed request')
 api('/api/config',{'rps':0,'concurrency':4,'delay_ms':0,'cpu_ms':0,'error_percent':0})
 # Wait for the workers' ordinary asynchronous cache refresh before enqueueing.
 time.sleep(2)
 before=k('get','pods','-l','app.kubernetes.io/component=consumer','-o','json')
 env=dict(os.environ,GOTOOLCHAIN='go1.26.0',LAB_PROOF_MODE='delete-consumer')
 env['LAB_PROOF_TOKEN']=base64.b64decode(k('get','secret','lab-nats','-o','jsonpath={.data.token}')).decode()
 forward=subprocess.Popen(K+['port-forward','service/nats','18089:4222','--address=127.0.0.1'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  time.sleep(1)
  if forward.poll() is not None:raise AssertionError('owned broker forward failed')
  subprocess.run(['go','run','scripts/nats-proof.go'],cwd=ROOT,env=env,check=True,timeout=45)
 finally:
  forward.terminate();forward.wait(timeout=10);env.pop('LAB_PROOF_TOKEN',None)
 ids=api('/api/action',{'action':'enqueue','count':1,'failures':0})['ids']
 wait(lambda s:any(t['id']==ids[0] and t['state']=='completed' for t in s['queue']['tasks']))
 after=k('get','pods','-l','app.kubernetes.io/component=consumer','-o','json')
 def identity(raw):return [(p['metadata']['uid'],[c.get('restartCount',0) for c in p.get('status',{}).get('containerStatuses',[])]) for p in json.loads(raw)['items']]
 assert identity(before)==identity(after),'consumer recovered by pod replacement instead of rebinding'
 checks.append('deleted durable recreated and new task ACKed by the same consumer process')
finally:
 api('/api/action',{'action':'reset'})
 (ROOT/'.state/readiness-smoke-results.json').write_text(json.dumps({'checks':checks},indent=2))
for check in checks:print('PASS',check,flush=True)
