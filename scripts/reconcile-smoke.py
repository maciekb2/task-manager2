#!/usr/bin/env python3
"""Existing task-KV settings and recreated local NATS Secret reconcile through deploy."""
import base64,hashlib,json,os,subprocess,time,urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
token='';checks=[]
def api(path,body=None):
 req=urllib.request.Request(BASE+path,data=None if body is None else json.dumps(body).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def k(*args):return subprocess.check_output(K+list(args),text=True,timeout=180)
def session():
 end=time.monotonic()+45
 while time.monotonic()<end:
  try:return api('/api/session')['token']
  except OSError:time.sleep(.5)
 raise AssertionError('session unavailable')
def complete(id):
 end=time.monotonic()+45
 while time.monotonic()<end:
  tasks=api('/api/state')['queue']['tasks']
  if any(t['id']==id and t['state']=='completed' for t in tasks):return
  time.sleep(.5)
 raise AssertionError('task did not complete')
def brokerproof(mode):
 env=dict(os.environ,GOTOOLCHAIN='go1.26.0',LAB_PROOF_MODE=mode)
 env['LAB_PROOF_TOKEN']=base64.b64decode(k('get','secret','lab-nats','-o','jsonpath={.data.token}')).decode()
 forward=subprocess.Popen(K+['port-forward','service/nats','18089:4222','--address=127.0.0.1'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  time.sleep(1)
  if forward.poll() is not None:raise AssertionError('owned broker forward failed')
  subprocess.run(['go','run','scripts/nats-proof.go'],cwd=ROOT,env=env,check=True,timeout=45)
 finally:forward.terminate();forward.wait(timeout=10);env.pop('LAB_PROOF_TOKEN',None)
def identities():
 raw=json.loads(k('get','pods','-l','app.kubernetes.io/part-of=systems-lab','-o','json'))
 return {p['metadata']['labels']['app.kubernetes.io/component']:p['metadata']['uid'] for p in raw['items'] if p['status']['phase']=='Running' and not p['metadata'].get('deletionTimestamp') and p['metadata']['labels']['app.kubernetes.io/component'] not in ['web','jaeger','job']}
token=session();api('/api/action',{'action':'reset'})
try:
 task=api('/api/action',{'action':'enqueue','count':1,'failures':0})['ids'][0];complete(task)
 brokerproof('kv-drift')
 k('rollout','restart','deployment/control');k('rollout','status','deployment/control','--timeout=90s');token=session()
 brokerproof('kv-config');complete(task)
 checks.append('existing task KV TTL/byte/history drift reconciles on startup and retains the terminal task')
 before=identities();images=k('get','deployments','-l','app.kubernetes.io/part-of=systems-lab','-o','json')
 old=hashlib.sha256(k('get','secret','lab-nats','-o','jsonpath={.data.token}').encode()).hexdigest()
 k('delete','secret','lab-nats')
 subprocess.run(['make','deploy'],cwd=ROOT,check=True,stdout=subprocess.DEVNULL,timeout=360)
 new=hashlib.sha256(k('get','secret','lab-nats','-o','jsonpath={.data.token}').encode()).hexdigest()
 assert old!=new,'credential was not actually recreated'
 after=identities();assert before.keys()==after.keys() and all(before[c]!=after[c] for c in before),'a broker client kept its old pod'
 newimages=k('get','deployments','-l','app.kubernetes.io/part-of=systems-lab','-o','json')
 def imageMap(raw):return {d['metadata']['name']:[c['image'] for c in d['spec']['template']['spec']['containers']] for d in json.loads(raw)['items']}
 assert imageMap(images)==imageMap(newimages),'roll was caused by changed images, not Secret digest'
 token=session();complete(task)
 task2=api('/api/action',{'action':'enqueue','count':1,'failures':0})['ids'][0];complete(task2)
 checks.append('recreated local credential rolls NATS and every broker client with unchanged images; old/new tasks complete')
finally:
 token=session();api('/api/action',{'action':'reset'})
 (ROOT/'.state/reconcile-smoke-results.json').write_text(json.dumps({'checks':checks,'at':time.time()},indent=2))
for c in checks:print('PASS',c,flush=True)
