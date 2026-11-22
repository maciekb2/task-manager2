#!/usr/bin/env python3
"""Verify rendered config changes really replace NATS/Jaeger pods, then restore."""
import json,os,shutil,subprocess,tempfile,time
from pathlib import Path
import urllib.request
ROOT=Path(__file__).resolve().parent.parent
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
checks=[]
def api():return json.load(urllib.request.urlopen(BASE+'/api/state',timeout=20))
def k(*args,input=None):return subprocess.run(K+list(args),input=input,capture_output=True,text=True,check=True,timeout=150).stdout
def pods():
 data=json.loads(k('get','pods','-o','json'))
 return {p['metadata']['labels'].get('app.kubernetes.io/component'):p['metadata']['uid'] for p in data['items'] if p['metadata']['labels'].get('app.kubernetes.io/component') in ('nats','jaeger') and not p['metadata'].get('deletionTimestamp')}
def apply(items):
 k('apply','-f','-',input=json.dumps({'apiVersion':'v1','kind':'List','items':items}))
 for name in ['nats','jaeger']:k('rollout','status','deployment/'+name,'--timeout=120s')
def selected(manifest):return [x for x in manifest['items'] if (x['kind']=='ConfigMap' and x['metadata']['name'] in ('nats-config','jaeger-config')) or (x['kind']=='Deployment' and x['metadata']['name'] in ('nats','jaeger'))]
state=api()
assert not(state['config']['rps']>0 and state['config']['run_until']>time.time()),'stop traffic before config acceptance'
existing={t['id'] for t in state['queue'].get('tasks',[])}
env=dict(os.environ,LAB_IMAGE=(ROOT/'.state/image').read_text().strip())
base=selected(json.loads(subprocess.check_output(['python3',str(ROOT/'deploy/render.py')],env=env)))
with tempfile.TemporaryDirectory(dir=ROOT/'.state') as folder:
 path=Path(folder)
 shutil.copy(ROOT/'deploy/render.py',path/'render.py');shutil.copy(ROOT/'deploy/jaeger.yaml',path/'jaeger.yaml')
 render=(path/'render.py').read_text().replace('max_memory_store: 64MB','max_memory_store: 63MB')
 (path/'render.py').write_text(render)
 (path/'jaeger.yaml').write_text((path/'jaeger.yaml').read_text().replace('max_traces: 5000','max_traces: 4999'))
 variant=selected(json.loads(subprocess.check_output(['python3',str(path/'render.py')],env=env)))
 for original,changed in zip([x for x in base if x['kind']=='Deployment'],[x for x in variant if x['kind']=='Deployment']):
  assert original['spec']['template']!=changed['spec']['template'],'config change failed to alter pod template'
 before=pods()
 try:
  apply(variant);after=pods();assert all(after[n]!=before[n] for n in before)
  checks.append('both rendered config changes replace real pods');print('PASS',checks[-1],flush=True)
 finally:
  apply(base)
 restored=pods();assert all(restored[n]!=after[n] for n in after)
 checks.append('restoring canonical configuration replaces pods again');print('PASS',checks[-1],flush=True)
 end=time.monotonic()+30
 while time.monotonic()<end:
  current=api()
  if not current['queue'].get('error') and existing<={t['id'] for t in current['queue'].get('tasks',[])}:break
  time.sleep(1)
 else:raise AssertionError('stored task history did not recover')
 checks.append('JetStream task history survives config update and restoration');print('PASS',checks[-1],flush=True)
 (ROOT/'.state/config-rollout-smoke-results.json').write_text(json.dumps({'checks':checks,'at':time.time()},indent=2))
