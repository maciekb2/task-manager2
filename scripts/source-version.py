#!/usr/bin/env python3
import hashlib
from pathlib import Path
root = Path(__file__).resolve().parent.parent
h = hashlib.sha256()
paths = [root / n for n in ('Dockerfile', 'go.mod', 'go.sum', 'deploy/render.py', 'deploy/jaeger.yaml')]
for folder in ('cmd', 'internal'):
    paths.extend(p for p in (root / folder).rglob('*') if p.is_file())
for p in sorted(paths):
    h.update(str(p.relative_to(root)).encode())
    h.update(p.read_bytes())
print(h.hexdigest()[:16])
