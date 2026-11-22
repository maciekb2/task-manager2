#!/usr/bin/env python3
"""Exercise the real local cluster, restoring baseline configuration on exit."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
BASE = os.environ.get('LAB_URL', 'http://127.0.0.1:18085')
token = ''
parser = argparse.ArgumentParser()
parser.add_argument('--resume-failed-job', help='Continue the failed-Job/CronJob checks after a fixture-only interruption')
args = parser.parse_args()
checks = []
if args.resume_failed_job and (ROOT / '.state/smoke-results.json').exists():
    checks = json.loads((ROOT / '.state/smoke-results.json').read_text()).get('checks', [])

def api(path, data=None, auth=True):
    headers = {'Content-Type': 'application/json'}
    if auth:
        headers['X-Lab-Token'] = token
    request = urllib.request.Request(BASE + path, data=None if data is None else json.dumps(data).encode(), headers=headers)
    with urllib.request.urlopen(request, timeout=20) as response:
        return json.load(response)

def wait(label, predicate, seconds=90):
    deadline = time.monotonic() + seconds
    last = None
    while time.monotonic() < deadline:
        try:
            last = api('/api/state')
            if predicate(last):
                checks.append(label)
                print('PASS', label, flush=True)
                return last
        except (urllib.error.URLError, TimeoutError):
            pass
        time.sleep(2)
    raise AssertionError(f'{label}: timeout; last cluster errors={last.get("cluster", {}).get("errors") if last else None}')

def action(**data):
    return api('/api/action', data)

def config(rps=0, errors=0, duration=15):
    return api('/api/config', {'rps': rps, 'concurrency': 4, 'delay_ms': 40,
        'error_percent': errors, 'cpu_ms': 0, 'duration_seconds': duration})

def replicas(state, name, count):
    deployments = state.get('cluster', {}).get('deployments', [])
    pods = state.get('cluster', {}).get('pods', [])
    found = any(d['name'] == name and d['replicas'] == count and d['ready'] == count for d in deployments)
    return found and len([p for p in pods if p['component'] == name and p['phase'] == 'Running']) == count

def forbidden(data):
    try:
        api('/api/action', data)
    except urllib.error.HTTPError as error:
        assert error.code in (400, 403), error.code
        return
    raise AssertionError('forbidden action succeeded')

def k(*args):
    return subprocess.run([str(ROOT / '.tools/kubectl'), '--kubeconfig', str(ROOT / '.state/kubeconfig'), '--context', 'systems-lab', *args], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout.strip()

token = api('/api/session', auth=False)['token']
try:
    if not args.resume_failed_job:
        initial = wait('discovery: 11 deployments, nodes, services, no API errors', lambda s: len(s.get('cluster', {}).get('deployments', [])) == 11 and bool(s['cluster']['nodes']) and not s['cluster']['errors'])
        try:
            api('/api/action', {'action': 'scale', 'target': 'echo', 'replicas': 1}, auth=False)
            raise AssertionError('unauthenticated mutation succeeded')
        except urllib.error.HTTPError as error:
            assert error.code == 403
        forbidden({'action': 'scale', 'target': 'nats', 'replicas': 0})
        forbidden({'action': 'scale', 'target': 'kube-system', 'replicas': 0})
        assert k('auth', 'can-i', 'get', 'secrets', '--as=system:serviceaccount:systems-lab:observer') == 'no'
        assert k('auth', 'can-i', 'patch', 'deployments', '-n', 'default', '--as=system:serviceaccount:systems-lab:control') == 'no'
        checks.append('mutation authentication and RBAC boundaries')
        print('PASS mutation authentication and RBAC boundaries', flush=True)

        config(rps=5, duration=10)
        s = wait('real HTTP through generator, gateway, validator, enricher, echo', lambda s: all(any(e['from'] == source and e['to'] == target and e['status'] == 200 for e in s['events']) for source, target in [('generator','gateway'),('gateway','validator'),('validator','enricher'),('enricher','echo')]))
        event = next(e for e in s['events'] if e['from'] == 'generator' and e['status'] == 200)
        trace = api('/api/trace?id=' + event['trace'])
        assert len({e['from'] for e in trace if e['kind'] == 'http'}) >= 4
        assert api('/api/results'), 'result-store did not persist HTTP outcome'
        time.sleep(11)
        assert api('/api/state')['config']['run_until'] <= time.time()
        checks.append('correlated trace, persisted HTTP result, bounded generator duration')
        print('PASS correlated trace, persisted HTTP result, bounded generator duration', flush=True)

        config(rps=3, errors=100, duration=8)
        wait('controlled HTTP 503 propagates through the chain', lambda s: any(e['from'] == 'generator' and e['status'] == 503 for e in s['events']))
        config()
        action(action='scale', target='echo', replicas=3)
        s = wait('scale echo to three real ready pods', lambda s: replicas(s, 'echo', 3))
        victim = next(p['name'] for p in s['cluster']['pods'] if p['component'] == 'echo' and p['phase'] == 'Running')
        action(action='restart', target=victim)
        wait('Deployment replaces the deleted pod', lambda s: replicas(s, 'echo', 3) and victim not in [p['name'] for p in s['cluster']['pods']])

        action(action='scale', target='consumer', replicas=0)
        wait('consumer is fully stopped', lambda s: replicas(s, 'consumer', 0))
        ids = action(action='enqueue', count=3, failures=1)['ids']
        wait('queue backlog grows without a consumer', lambda s: s['queue']['pending'] >= 3 and all(t['state'] == 'queued' for t in s['queue']['tasks'] if t['id'] in ids))
        action(action='scale', target='consumer', replicas=1)
        wait('retry then success: three tasks durably completed after two attempts', lambda s: len([t for t in s['queue']['tasks'] if t['id'] in ids and t['state'] == 'completed' and t['attempt'] == 2]) == 3)
        dead = action(action='enqueue', count=1, failures=-1)['ids'][0]
        wait('permanent failure reaches DLQ after three attempts', lambda s: any(t['id'] == dead and t['state'] == 'dead-letter' and t['attempt'] == 3 for t in s['queue']['tasks']) and s['queue']['dead_letters'] > 0)

        job = action(action='job', failures=0)['name']
        wait('Kubernetes Job completes successfully', lambda s: any(j['name'] == job and j['succeeded'] >= 1 for j in s['cluster']['jobs']))
    job = args.resume_failed_job or action(action='job', failures=1)['name']
    wait('failed Kubernetes Job retries and reaches a terminal failure', lambda s: any(j['name'] == job and any(c['type'] == 'Failed' and c['status'] == 'True' for c in (j.get('conditions') or [])) for j in s['cluster']['jobs']), seconds=120)
    before = {j['name'] for j in api('/api/state')['cluster']['jobs']}
    action(action='cron', suspended=False, schedule='* * * * *')
    wait('CronJob actually schedules and completes a new Job', lambda s: any(j['name'].startswith('lab-tick-') and j['name'] not in before and j['succeeded'] > 0 for j in s['cluster']['jobs']), seconds=100)
finally:
    try:
        action(action='reset')
        wait('baseline restored: no generator traffic, two echo pods, CronJob suspended', lambda s: s['config']['rps'] == 0 and replicas(s, 'echo', 2) and bool(s['cluster']['cronjobs'][0]['suspended']))
    finally:
        (ROOT / '.state/smoke-results.json').write_text(json.dumps({'at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'checks': list(dict.fromkeys(checks))}, indent=2))
print(f'PASS {len(set(checks))} real-cluster checks', flush=True)
