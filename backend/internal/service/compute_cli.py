#!/usr/bin/env python3
"""Sub2API workspace CLI. Python 3.10+, no third-party packages required."""
import argparse
import fnmatch
import getpass
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zipfile

EXCLUDED = {'.git', '.venv', 'venv', 'node_modules', '__pycache__', '.ssh', '.aws', '.config'}
SECRET_PATTERNS = ('.env*', '*.pem', '*.key', '*credentials*.json', '*token*.json', '.sub2api-*')


def workspace_files(root, outputs):
    rules = []
    ignore = root / '.computeignore'
    if ignore.exists():
        rules = [s.strip() for s in ignore.read_text().splitlines() if s.strip() and not s.startswith('#')]
    result = []
    for directory, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = sorted(d for d in dirs if d not in EXCLUDED and not d.startswith('.') and not (Path(directory)/d).is_symlink())
        for name in sorted(files):
            p = Path(directory) / name
            rel = p.relative_to(root).as_posix()
            if p.is_symlink() or name.startswith('.') or any(fnmatch.fnmatch(name, x) for x in SECRET_PATTERNS):
                continue
            if any(rel == x or rel.startswith(x.rstrip('/') + '/') or fnmatch.fnmatch(rel, x) for x in [*rules, *outputs]):
                continue
            result.append((p, rel))
    if len(result) > 10000 or sum(p.stat().st_size for p, _ in result) > 256 * 1024 * 1024:
        raise ValueError('Workspace exceeds 10000 files or 256 MiB; add exclusions to .computeignore')
    return result


class Client:
    def __init__(self, base, key):
        u = urllib.parse.urlsplit(base)
        if u.scheme != 'https' or u.username or u.password or u.query or u.fragment or u.path not in ['', '/']:
            raise ValueError('--base must be an HTTPS origin')
        self.base, self.key = base.rstrip('/'), key
        self.opener = urllib.request.build_opener()

    def call(self, path, data=None, method=None):
        request = urllib.request.Request(self.base + path, method=method or ('POST' if data is not None else 'GET'),
            data=json.dumps(data).encode() if data is not None else None,
            headers={'Authorization': 'Bearer ' + self.key, 'Content-Type': 'application/json'})
        with self.opener.open(request, timeout=90) as response:
            body = json.load(response)
            return body.get('data', body)

    def upload(self, archive, name):
        size = archive.stat().st_size
        if size > 64 * 1024 * 1024:
            raise ValueError('Compressed workspace exceeds 64 MiB')
        ticket = self.call('/drive/v1/upload-sessions', {'path': '/', 'name': name, 'size': size, 'mime_type': 'application/zip'})
        # A file-scoped Google upload URL; never send the platform key to Google.
        target = urllib.parse.urlsplit(ticket['upload_url'])
        if target.scheme != 'https' or target.hostname != 'www.googleapis.com':
            raise ValueError('Unexpected Drive upload host')
        try:
            with archive.open('rb') as body:
                request = urllib.request.Request(ticket['upload_url'], data=body, method='PUT',
                    headers={'Content-Type': 'application/zip', 'Content-Length': str(size)})
                with self.opener.open(request, timeout=300) as response:
                    response.read()
            self.call('/drive/v1/uploads/' + str(ticket['upload_id']) + '/complete', {})
        except BaseException:
            self.call('/drive/v1/uploads/' + str(ticket['upload_id']), method='DELETE')
            raise
        return ticket['path']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', default=os.getenv('SUB2API_BASE_URL'), help='HTTPS platform origin or SUB2API_BASE_URL')
    commands = parser.add_subparsers(dest='action', required=True)
    run = commands.add_parser('run', help='Package the current directory and execute an argv in an isolated runtime')
    run.add_argument('--directory', type=Path, default=Path.cwd())
    run.add_argument('--hardware', default='CPU')
    run.add_argument('--timeout', type=int, default=600)
    run.add_argument('--output', action='append', help='Relative output file/directory, repeatable; default: outputs')
    run.add_argument('--id', help='Job UUID; reuse it to inspect an earlier submission without running it again')
    run.add_argument('--wait', action='store_true')
    run.add_argument('--dry-run', action='store_true', help='Print the exact file manifest without uploading or submitting')
    run.add_argument('argv', nargs=argparse.REMAINDER)
    submit = commands.add_parser('submit', help='Submit a standalone Python script')
    submit.add_argument('file', type=Path)
    submit.add_argument('--hardware', default='CPU')
    submit.add_argument('--timeout', type=int, default=180)
    submit.add_argument('--id')
    for name in ['status', 'cancel', 'wait']:
        commands.add_parser(name).add_argument('id')
    commands.add_parser('list')
    commands.add_parser('resources')
    args = parser.parse_args()
    if args.action == 'run':
        args.argv = args.argv[1:] if args.argv[:1] == ['--'] else args.argv
        if not args.argv:
            parser.error('Provide a command after --, e.g. -- python main.py')
        args.output = args.output or ['outputs']
        for value in args.output:
            p = Path(value)
            if p.is_absolute() or '..' in p.parts or value in ['', '.'] or '\\' in value:
                parser.error('--output must be a relative file or directory inside the workspace')
        files = workspace_files(args.directory.resolve(), args.output)
        print(json.dumps({'files': [r for _, r in files], 'bytes': sum(p.stat().st_size for p, _ in files), 'command': args.argv}, ensure_ascii=False), file=sys.stderr)
        if args.dry_run:
            return
    if not args.base:
        parser.error('--base or SUB2API_BASE_URL is required')
    key = os.getenv('SUB2API_API_KEY') or getpass.getpass('Platform API key: ')
    client = Client(args.base, key)
    endpoint = '/compute/v1/jobs'
    if args.action in ['run', 'submit']:
        job_id = str(uuid.UUID(args.id)) if args.id else str(uuid.uuid4())
        print('Job ID (retain for retries): ' + job_id, file=sys.stderr)
        existing = None
        if args.id:
            try:
                existing = client.call(endpoint + '/' + job_id)
            except urllib.error.HTTPError as e:
                if e.code != 404:
                    raise
        if existing:
            result = existing
        else:
            body = dict(id=job_id, hardware=args.hardware, timeout_seconds=args.timeout)
            if args.action == 'submit':
                body.update(title=args.file.name, code=args.file.read_text())
            else:
                with tempfile.TemporaryDirectory(prefix='sub2api-') as directory:
                    archive = Path(directory) / 'workspace.zip'
                    with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as z:
                        for p, rel in files:
                            z.write(p, rel)
                    input_path = client.upload(archive, 'workspace-' + job_id + '.zip')
                body.update(title=args.directory.resolve().name, workspace=dict(input_path=input_path, command=args.argv, outputs=args.output))
            result = client.call(endpoint, body)
        if args.action == 'run' and args.wait:
            args.action, args.id = 'wait', job_id
    elif args.action == 'resources':
        result = client.call('/compute/v1/status')
    elif args.action == 'list':
        result = client.call(endpoint)
    else:
        args.id = str(uuid.UUID(args.id))
        result = client.call(endpoint + '/' + args.id + ('/cancel' if args.action == 'cancel' else ''), {} if args.action == 'cancel' else None)
    if args.action == 'wait':
        while True:
            result = client.call(endpoint + '/' + args.id)
            print(result['status'], file=sys.stderr)
            if result['status'] not in ['queued', 'provisioning', 'running', 'writing']:
                break
            time.sleep(5)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    if isinstance(result, dict) and result.get('status') in ['failed', 'cancelled', 'unknown']:
        raise SystemExit(1)


if __name__ == '__main__':
    try:
        main()
    except urllib.error.HTTPError as e:
        raise SystemExit(f'HTTP {e.code}: ' + e.read(4096).decode(errors='replace'))
    except (ValueError, OSError) as e:
        raise SystemExit(str(e))
