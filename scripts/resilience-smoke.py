#!/usr/bin/env python3
"""Real protection, parallel branches and outcome retention under span eviction."""
import json,os,subprocess,time,urllib.request,sys
from datetime import datetime
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
K=[str(ROOT/'.tools/kubectl'),'--kubeconfig',str(ROOT/'.state/kubeconfig'),'--context','systems-lab','-n','systems-lab']
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
token='';checks=[]
def api(path,body=None):
 req=urllib.request.Request(BASE+path,data=None if body is None else json.dumps(body).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def wait(name,read,predicate,seconds=45):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  try:
   value=read()
   if predicate(value):checks.append(name);print('PASS',name,flush=True);return value
  except (OSError,ValueError,KeyError,IndexError):pass
  time.sleep(.5)
 raise AssertionError(name)
def stamp(t):return datetime.fromisoformat(t.replace('Z','+00:00')).timestamp()
def configure(**kw):
 c=dict(rps=10,concurrency=12,delay_ms=0,cpu_ms=0,error_percent=0,payload_bytes=0,timeout_ms=0,max_in_flight=0,breaker=False,fan_out=False,branch_delay_ms=0,duration_seconds=60);c.update(kw);api('/api/config',c);return time.time()+2
def roots(s,after):return [e for e in s['events'] if e['kind']=='http' and e['from']=='generator' and stamp(e['time'])>after]
def k(*args):return subprocess.check_output(K+list(args),text=True,timeout=120)
token=api('/api/session')['token']
if '--from-fanout' in sys.argv:checks=json.loads((ROOT/'.state/resilience-smoke-results.json').read_text())['checks']
try:
 api('/api/action',{'action':'reset'})
 if '--from-fanout' not in sys.argv:
  after=configure(delay_ms=800,timeout_ms=150)
  s=wait('gateway deadline returns real 504 without waiting full 800 ms',lambda:api('/api/state'),lambda s:len(roots(s,after))>=8 and all(e['status']==504 and e['duration_ms']<650 for e in roots(s,after)))
  p=api('/api/protection');assert p['timeouts']>0
  after=configure(rps=20,concurrency=20,delay_ms=800,max_in_flight=2)
  s=wait('bounded admission rejects excess with 429 while admitted work completes',lambda:api('/api/state'),lambda s:{200,429}<={e['status'] for e in roots(s,after)})
  p=api('/api/protection');assert p['active']<=2 and p['backpressure_rejections']>0
  after=configure(rps=10,error_percent=100,breaker=True)
  s=wait('open circuit rejects new roots without calling failing dependency',lambda:api('/api/state'),lambda s:len(roots(s,after))>=20 and any(e['kind']=='protection' and e['message']=='circuit-open' and stamp(e['time'])>after for e in s['events']))
  roots_now=roots(s,after);downstream=[e for e in s['events'] if e['kind']=='http' and e['to']=='echo' and stamp(e['time'])>after]
  assert len(downstream)<len(roots_now)/2,(len(downstream),len(roots_now))
  p=api('/api/protection');assert p['circuit_rejections']>0
  after=configure(rps=10,error_percent=0,breaker=True)
  wait('half-open successful probe closes circuit and restores HTTP 200',lambda:api('/api/state'),lambda s:any(e['status']==200 for e in roots(s,after+3)) and api('/api/protection')['state']=='closed')
 after=configure(rps=4,concurrency=4,delay_ms=20,fan_out=True,branch_delay_ms=400)
 s=wait('fan-out calls both real services and completes the required aggregate',lambda:api('/api/state'),lambda s:any(e['status']==200 and 350<e['duration_ms']<900 for e in roots(s,after)))
 event=next(e for e in roots(s,after) if e['status']==200);trace=event['trace']
 wait('native trace includes both parallel lookup spans',lambda:api('/api/otel/trace?id='+trace),lambda d:bool(d.get('data')) and {'inventory.lookup','pricing.lookup'}<={s['operationName'] for s in d['data'][0]['spans']})
 native=api('/api/otel/trace?id='+trace)['data'][0]['spans'];spans={s['operationName']:s for s in native if s['operationName'] in ['inventory.lookup','pricing.lookup']}
 a,b=spans.values();overlap=min(a['startTime']+a['duration'],b['startTime']+b['duration'])-max(a['startTime'],b['startTime']);assert overlap>100000,overlap
 hops=api('/api/trace?id='+trace);assert {'inventory','pricing'}<={e['to'] for e in hops if e['kind']=='http' and e['status']==200}
 checks.append('parallel span overlap exceeds 100 ms; actual HTTP/payload events cover both branches')
 after=configure(rps=4,fan_out=True,branch_delay_ms=400,timeout_ms=100)
 wait('deadline cancels fan-out through the HTTP chain',lambda:api('/api/state'),lambda s:any(e['status']==504 for e in roots(s,after)) and any(e['kind']=='span' and e.get('operation')=='pricing.lookup' and e['status']==499 and stamp(e['time'])>after for e in s['events']))
 configure(rps=0)
 time.sleep(2)
 # Broker remains the outcome source while its read facade is offline.
 api('/api/action',{'action':'scale','target':'result-store','replicas':0})
 k('wait','--for=delete','pod','-l','app.kubernetes.io/component=result-store','--timeout=90s')
 task=api('/api/action',{'action':'enqueue','count':1,'failures':0})['ids'][0]
 s=wait('HTTP task completes while results facade is offline',lambda:api('/api/state'),lambda s:any(t['id']==task and t['state']=='completed' for t in s['queue']['tasks']))
 trace=next(t['trace'] for t in s['queue']['tasks'] if t['id']==task)
 # Dedicated operator proof verifies the source cannot be evicted by span traffic.
 import base64
 env=dict(os.environ,GOTOOLCHAIN='go1.26.0',LAB_PROOF_MODE='outcome-isolation',LAB_PROOF_TRACE=trace)
 env['LAB_PROOF_TOKEN']=base64.b64decode(k('get','secret','lab-nats','-o','jsonpath={.data.token}')).decode()
 forward=subprocess.Popen(K+['port-forward','service/nats','18089:4222','--address=127.0.0.1'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  time.sleep(1)
  if forward.poll() is not None:raise AssertionError('broker forward failed')
  subprocess.run(['go','run','scripts/nats-proof.go'],cwd=ROOT,env=env,check=True,timeout=90)
 finally:
  forward.terminate();forward.wait(timeout=10);env.pop('LAB_PROOF_TOKEN',None)
 api('/api/action',{'action':'scale','target':'result-store','replicas':1});k('rollout','status','deployment/result-store','--timeout=90s')
 wait('offline outcome survives EVENTS capacity eviction and facade restart',lambda:api('/api/results'),lambda rows:any(e['trace']==trace for e in rows))
finally:
 api('/api/action',{'action':'reset'})
 checks=list(dict.fromkeys(checks))
 (ROOT/'.state/resilience-smoke-results.json').write_text(json.dumps({'checks':checks,'at':time.time()},indent=2))
print('PASS',len(checks),'resilience checks',flush=True)
