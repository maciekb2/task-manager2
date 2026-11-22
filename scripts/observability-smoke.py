#!/usr/bin/env python3
"""Focused acceptance for the learning/observability release; synthetic cluster only."""
import json
import time
from datetime import datetime, timezone
from pathlib import Path
import urllib.request
import urllib.error

BASE='http://127.0.0.1:18085'
ROOT=Path(__file__).resolve().parent.parent
checks=[]
def api(path,data=None):
    headers={'Content-Type':'application/json'}
    if token: headers['X-Lab-Token']=token
    req=urllib.request.Request(BASE+path,data=None if data is None else json.dumps(data).encode(),headers=headers)
    with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
def wait(name,read,predicate,seconds=90):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        try:
            value=read()
            if predicate(value):checks.append(name);print('PASS',name,flush=True);return value
        except (urllib.error.URLError,TimeoutError,KeyError):pass
        time.sleep(2)
    raise AssertionError(name+' timed out')
token=''
token=api('/api/session')['token']
started=datetime.now(timezone.utc)
def fresh(e):return datetime.fromisoformat(e['time'].replace('Z','+00:00'))>started
try:
    api('/api/config',{'rps':5,'concurrency':4,'delay_ms':20,'error_percent':0,'cpu_ms':30,'duration_seconds':60})
    state=wait('fresh HTTP through instrumented chain',lambda:api('/api/state'),lambda s:any(fresh(e) and e['from']=='generator' and e['kind']=='http' and e['status']==200 for e in s['events']))
    root=next(e for e in state['events'] if fresh(e) and e['from']=='generator' and e['kind']=='http' and e['status']==200)
    native=wait('Jaeger stores standard trace with HTTP services and CPU/delay child spans',lambda:api('/api/otel/trace?id='+root['trace']),lambda d:bool(d.get('data')) and {'generator','gateway','validator','enricher','echo'}<=set(p['serviceName'] for p in d['data'][0]['processes'].values()) and {'echo.cpu','echo.delay','validator.validate','enricher.decorate'}<=set(s['operationName'] for s in d['data'][0]['spans']))
    spans=native['data'][0]['spans'];ids={s['spanID'] for s in spans};assert all(s['traceID']==root['trace'] for s in spans);assert any(r['spanID'] in ids for s in spans for r in s.get('references',[]))
    perf=wait('fresh pod/node CPU RAM samples, history and real function timings',lambda:api('/api/performance?minutes=1'),lambda p:len(p['series'])>=3 and p['latest']['cpu_cores'] is not None and p['latest']['node_cpu_cores'] is not None and datetime.fromisoformat(p['latest']['metric_at'].replace('Z','+00:00'))>started and any(f['name']=='echo/echo.cpu' and f['count']>0 and f['mean_ms']>=20 for f in p['functions']))
    assert perf['latest']['covered_pods']>0
    api('/api/config',{'rps':0,'concurrency':4,'delay_ms':40,'error_percent':0,'cpu_ms':0,'duration_seconds':1})
    task=api('/api/action',{'action':'enqueue','count':1,'failures':1})['ids'][0]
    state=wait('queued retry retains trace and measured wait/total durations',lambda:api('/api/state'),lambda s:any(t['id']==task and t['state']=='completed' and t['attempt']==2 and t['total_ms']>=1900 and t['queue_ms']>=0 for t in s['queue']['tasks']))
    record=next(t for t in state['queue']['tasks'] if t['id']==task)
    wait('queue producer/consumer spans share a standard distributed trace',lambda:api('/api/otel/trace?id='+record['trace']),lambda d:bool(d.get('data')) and {'task.enqueue','consumer.process','consumer.request'}<=set(s['operationName'] for s in d['data'][0]['spans']))
    assert urllib.request.urlopen(BASE+'/tracing/').status==200
    checks.append('same-origin Jaeger UI reachable')
finally:
    api('/api/action',{'action':'reset'})
    (ROOT/'.state/observability-smoke-results.json').write_text(json.dumps({'checks':checks,'at':datetime.now(timezone.utc).isoformat()},indent=2))
print('PASS',len(checks),'focused checks',flush=True)
