#!/usr/bin/env python3
"""Measure actual HTTP response and JetStream payloads on the isolated lab."""
import json
import os
import time
from pathlib import Path
from datetime import datetime,timezone
import urllib.request
BASE=os.environ.get('LAB_URL','http://127.0.0.1:18085').rstrip('/')
ROOT=Path(__file__).resolve().parent.parent
checks=[]
token=''
def api(path,data=None):
    req=urllib.request.Request(BASE+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json','X-Lab-Token':token})
    with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def wait(name,read,predicate,seconds=45):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        result=read()
        if predicate(result):
            checks.append(name);print('PASS',name,flush=True);return result
        time.sleep(1)
    raise AssertionError(name)
def fresh(event,started):return datetime.fromisoformat(event['time'].replace('Z','+00:00'))>started
token=api('/api/session')['token']
try:
    sizes=[]
    for payload in [0,8192]:
        started=datetime.now(timezone.utc)
        api('/api/config',{'rps':4,'concurrency':4,'delay_ms':10,'error_percent':0,'cpu_ms':0,'payload_bytes':payload,'duration_seconds':45})
        state=wait(f'fresh HTTP response bytes for payload {payload}',lambda:api('/api/state'),lambda s:any(e['from']=='generator' and e['kind']=='http' and e.get('transfer_bytes',0)>payload and fresh(e,started) for e in s['events']))
        root=next(e for e in state['events'] if e['from']=='generator' and e['kind']=='http' and e.get('transfer_bytes',0)>payload and fresh(e,started))
        sizes.append(root['transfer_bytes'])
        chain=wait('same response bytes on all four measured HTTP hops',lambda:api('/api/trace?id='+root['trace']),lambda events:len([e for e in events if e['kind']=='http' and e.get('transfer_bytes')==root['transfer_bytes']])==4)
    assert sizes[1]-sizes[0]==8192,(sizes,'declared payload differs from actual byte delta')
    checks.append('8192 extra bytes observed, not inferred from configuration')
    report=wait('completed five-second throughput bucket contains HTTP bytes',lambda:api('/api/network'),lambda r:r['recent']['http_bytes_per_second']>0 and len(r['series'])>0)
    edge=next(e for e in report['summary']['edges'] if e['from']=='generator' and e['to']=='gateway')
    assert edge['source']=='gateway' and edge['target']=='generator'
    assert abs(edge['bytes']/30-edge['bytes_per_second'])<1e-6
    assert edge['mean_interval_ms'] is not None
    api('/api/config',{'rps':0,'concurrency':4,'delay_ms':10,'error_percent':0,'cpu_ms':0,'payload_bytes':0})
    started=datetime.now(timezone.utc)
    task=api('/api/action',{'action':'enqueue','count':1,'failures':1})['ids'][0]
    state=wait('queue retry completed with measured message deliveries',lambda:api('/api/state'),lambda s:any(t['id']==task and t['state']=='completed' for t in s['queue']['tasks']))
    trace=next(t['trace'] for t in state['queue']['tasks'] if t['id']==task)
    events=api('/api/trace?id='+trace)
    sent=[e for e in events if e['from']=='control' and e['to']=='nats' and e.get('transfer_protocol')=='nats-message']
    received=[e for e in events if e['from']=='nats' and e['to']=='consumer' and e.get('transfer_protocol')=='nats-message']
    assert len(sent)==1 and len(received)==2,(sent,received)
    assert all(e['transfer_bytes']==sent[0]['transfer_bytes']>0 for e in received)
    assert all('transfer_bytes' not in e for e in events if 'Ponowienie' in e.get('message',''))
    checks.append('one actual publication, two same-sized deliveries, NAK excluded')
    wait('JetStream throughput reaches closed network bucket',lambda:api('/api/network'),lambda r:r['recent']['queue_bytes_per_second']>0 or r['summary']['queue_bytes_per_second']>0)
    wait('stopped HTTP transfer falls to zero in recent bucket',lambda:api('/api/network'),lambda r:r['recent']['http_bytes_per_second']==0,seconds=20)
finally:
    api('/api/config',{'rps':0,'concurrency':4,'delay_ms':40,'error_percent':0,'cpu_ms':0,'payload_bytes':0})
    (ROOT/'.state/network-smoke-results.json').write_text(json.dumps({'checks':checks,'at':datetime.now(timezone.utc).isoformat()},indent=2))
print('PASS',len(checks),'network checks',flush=True)
