#!/usr/bin/env python3
"""Pack the client without CDN dependencies, preserving a hashed CSP."""
from pathlib import Path
import hashlib,base64
ROOT=Path(__file__).resolve().parents[1]
html=(ROOT/'web/index.html').read_text(encoding='utf-8');css=(ROOT/'web/style.css').read_text(encoding='utf-8')
names=['domain.js', 'vendor/qr-encode.js', 'vendor/qr-decode.js', 'vendor/code128-patterns.js', 'codes.js', 'reports.js', 'scanner.js', 'app.js']
scripts=[(ROOT/'web'/n).read_text(encoding='utf-8') for n in names]
allow=' '.join("'sha256-"+base64.b64encode(hashlib.sha256(s.encode()).digest()).decode()+"'" for s in scripts)
html=html.replace("script-src 'self'",'script-src '+allow).replace('<link rel="stylesheet" href="style.css">','<style>'+css+'</style>')
for name,code in zip(names,scripts):html=html.replace(f'<script src="{name}"></script>','<script>'+code+'</script>')
html=html.replace('href="logo.svg"','href="data:image/svg+xml;base64,'+base64.b64encode((ROOT/'web/logo.svg').read_bytes()).decode()+'"')
for p in [ROOT/'dist/YARUS.html',ROOT/'android/app/src/main/assets/index.html']:
 p.parent.mkdir(parents=True,exist_ok=True);p.write_text(html,encoding='utf-8');print(p)
