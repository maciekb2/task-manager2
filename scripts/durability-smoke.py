#!/usr/bin/env python3
"""Actual duplicate delivery and create-only outcome replay; credentials stay in memory."""
import base64,json,os,subprocess,time
from pathlib import Path
import urllib.request
ROOT=Path(__file__).resolve().parent.parent
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
checks=[]
def k(*args):return subprocess.run(K+list(args),check=True,capture_output=True,text=True,timeout=120).stdout
def api(path,data=None):
 req=urllib.request.Request(BASE+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
token='';token=api('/api/session')['token']
api('/api/action',{'action':'reset'})
# Generate a dedicated terminal result, independent of previously retained tasks.
id=api('/api/action',{'action':'enqueue','count':1,'failures':0})['ids'][0]
end=time.monotonic()+30
while time.monotonic()<end:
 state=api('/api/state');task=next((t for t in state['queue']['tasks'] if t['id']==id),None)
 if task and task['state']=='completed':break
 time.sleep(.5)
else:raise AssertionError('proof task did not finish')
time.sleep(1)
event=next(e for e in api('/api/results') if e['trace']==task['trace'])
env=dict(os.environ,GOTOOLCHAIN='go1.26.0')
env['LAB_PROOF_TOKEN']=base64.b64decode(k('get','secret','lab-nats','-o','jsonpath={.data.token}')).decode()
forward=subprocess.Popen(K+['port-forward','service/nats','18089:4222','--address=127.0.0.1'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
def proof(mode):
 args=dict(env,LAB_PROOF_MODE=mode,LAB_PROOF_TASK=id,LAB_PROOF_EVENT=json.dumps(event))
 return json.loads(subprocess.check_output(['go','run','scripts/nats-proof.go'],cwd=ROOT,env=args,text=True,timeout=45))
try:
 time.sleep(1)
 if forward.poll() is not None:raise RuntimeError('owned NATS port-forward did not start')
 duplicate=proof('duplicate');checks.append('broker really redelivers terminal task; consumer ACKs without new work/result mutation');print('PASS',checks[-1],flush=True)
 before=proof('entry')
 k('rollout','restart','deployment/result-store');k('rollout','status','deployment/result-store','--timeout=90s')
 time.sleep(2)
 after=proof('entry');assert before==after,'replay renewed or changed stored result'
 checks.append('result-store restart/replay preserves original KV revision and creation time');print('PASS',checks[-1],flush=True)
 (ROOT/'.state/durability-smoke-results.json').write_text(json.dumps({'checks':checks,'duplicate':duplicate,'outcome':after},indent=2))
finally:
 forward.terminate();forward.wait(timeout=10);env.pop('LAB_PROOF_TOKEN',None)
