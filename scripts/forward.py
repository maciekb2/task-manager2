#!/usr/bin/env python3
"""Own only this checkout's loopback port-forward; never kill an unrelated PID."""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
import urllib.request

root = Path(__file__).resolve().parent.parent
state = root / '.state'
state.mkdir(exist_ok=True)
pidfile = state / 'forward.json'

def owned(record):
    try:
        args = Path(f'/proc/{record["pid"]}/cmdline').read_bytes().split(b'\0')
        return str(root / '.tools/kubectl').encode() == args[0] and str(state / 'kubeconfig').encode() in args and b'service/web' in args
    except (FileNotFoundError, KeyError):
        return False

record = json.loads(pidfile.read_text()) if pidfile.exists() else {}
if len(sys.argv) > 1 and sys.argv[1] == 'stop':
    if owned(record):
        os.kill(record['pid'], signal.SIGTERM)
    pidfile.unlink(missing_ok=True)
    sys.exit(0)
port = int(sys.argv[2]) if len(sys.argv) > 2 else 18085
if owned(record) and record.get('port') == port:
    try:
        with urllib.request.urlopen(f'http://127.0.0.1:{port}/api/session', timeout=2) as response:
            if response.status == 200:
                print(f'Panel: http://127.0.0.1:{port}')
                sys.exit(0)
    except Exception:
        pass
if owned(record):
    os.kill(record['pid'], signal.SIGTERM)
    for _ in range(50):
        if not owned(record):
            break
        time.sleep(0.1)
with socket.socket() as sock:
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        sock.bind(('127.0.0.1', port))
    except OSError:
        sys.exit(f'Port {port} is already occupied. Set LAB_PORT to another port.')
with open(state / 'forward.log', 'ab') as log:
    process = subprocess.Popen([str(root / '.tools/kubectl'), '--kubeconfig', str(state / 'kubeconfig'), '--context', 'systems-lab', '-n', 'systems-lab', 'port-forward', '--address', '127.0.0.1', 'service/web', f'{port}:8080'], stdout=log, stderr=log, start_new_session=True)
pidfile.write_text(json.dumps({'pid': process.pid, 'port': port}))
for _ in range(60):
    if process.poll() is not None:
        sys.exit('Port-forward failed; see .state/forward.log')
    try:
        with urllib.request.urlopen(f'http://127.0.0.1:{port}/api/session', timeout=1) as response:
            if response.status == 200:
                print(f'Panel: http://127.0.0.1:{port}')
                break
    except Exception:
        time.sleep(1)
else:
    sys.exit('Panel did not become ready; see .state/forward.log')
