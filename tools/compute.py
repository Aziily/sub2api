#!/usr/bin/env python3
"""Submit and inspect Colab GPU/CPU jobs using a Sub2API user API key (stdlib only)."""
import argparse
import getpass
import json
import os
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request
import uuid


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--base', required=True, help='HTTPS Sub2API origin')
    sub = p.add_subparsers(dest='command', required=True)
    submit = sub.add_parser('submit')
    submit.add_argument('file', type=Path)
    submit.add_argument('--hardware', default='CPU', help='Hardware spec: A100, A100_HIGHMEM, L4, G4, T4, CPU, CPU_HIGHMEM')
    submit.add_argument('--timeout', type=int, default=180)
    submit.add_argument('--id', default=None, help='Reuse the same UUID to safely retry a submission')
    for name in ['status', 'cancel']:
        sub.add_parser(name).add_argument('id')
    sub.add_parser('list')
    a = p.parse_args()
    u = urllib.parse.urlsplit(a.base)
    if u.scheme != 'https' or u.username or u.password or u.query or u.fragment or u.path not in ['', '/']:
        p.error('--base must be an HTTPS origin')
    key = os.getenv('SUB2API_API_KEY') or getpass.getpass('Sub2API API key: ')
    path = '/compute/v1/jobs'
    data = None
    method = 'GET'
    if a.command == 'submit':
        job_id = str(uuid.UUID(a.id)) if a.id else str(uuid.uuid4())
        print('Job ID (keep this for retry):', job_id)
        data = json.dumps(dict(id=job_id, title=a.file.name, code=a.file.read_text(), hardware=a.hardware, timeout_seconds=a.timeout)).encode()
        method = 'POST'
    elif a.command in ['status', 'cancel']:
        path += '/' + str(uuid.UUID(a.id))
        if a.command == 'cancel':
            path += '/cancel'
            method, data = 'POST', b'{}'
    request = urllib.request.Request(a.base.rstrip('/') + path, method=method, data=data,
        headers={'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(request, timeout=60) as r:
            print(json.dumps(json.load(r), ensure_ascii=False, indent=2))
    except urllib.error.HTTPError as e:
        raise SystemExit(f'HTTP {e.code}: ' + e.read(4096).decode(errors='replace'))


if __name__ == '__main__':
    main()
