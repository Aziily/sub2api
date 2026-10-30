#!/usr/bin/env python3
"""Sub2API workspace CLI. Python 3.10+, no third-party packages required."""
import argparse
import fnmatch
import getpass
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
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
ACTIVE = {'queued', 'provisioning', 'running', 'writing'}


def extract_outputs(archive, destination):
    """Validate the complete archive before extracting into a fresh directory."""
    with zipfile.ZipFile(archive) as z:
        items, seen = z.infolist(), set()
        if len(items) > 10000 or sum(i.file_size for i in items) > 512 * 1024 * 1024:
            raise ValueError('Outputs exceed 10000 files or 512 MiB expanded')
        for item in items:
            p = PurePosixPath(item.filename)
            mode = stat.S_IFMT(item.external_attr >> 16)
            normalized = str(p).casefold()
            if (p.is_absolute() or '..' in p.parts or not p.parts or
                    '\\' in item.filename or ':' in item.filename or
                    mode not in (0, stat.S_IFREG, stat.S_IFDIR) or normalized in seen):
                raise ValueError('Unsafe or conflicting output archive path')
            seen.add(normalized)
        destination.mkdir()
        z.extractall(destination)


def record_submission(root, base, job_id):
    """Retain retry information locally, never credentials or source code."""
    directory = root / '.sub2api' / 'jobs'
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    path = directory / (job_id + '.json')
    with path.open('w') as f:
        os.chmod(path, 0o600)
        json.dump({'id': job_id, 'base': base}, f)
    print('Local receipt: ' + str(path), file=sys.stderr)


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
            if p.is_symlink() or not p.is_file() or name.startswith('.') or any(fnmatch.fnmatch(name, x) for x in SECRET_PATTERNS):
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
        try:
            target = urllib.parse.urlsplit(ticket['upload_url'])
            if target.scheme != 'https' or target.hostname != 'www.googleapis.com' or target.username or target.password:
                raise ValueError('Unexpected Drive upload host')
            with archive.open('rb') as body:
                request = urllib.request.Request(ticket['upload_url'], data=body, method='PUT',
                    headers={'Content-Type': 'application/zip', 'Content-Length': str(size)})
                with self.opener.open(request, timeout=300) as response:
                    response.read()
            self.call('/drive/v1/uploads/' + str(ticket['upload_id']) + '/complete', {})
        except BaseException:
            try:
                self.call('/drive/v1/uploads/' + str(ticket['upload_id']), method='DELETE')
            except Exception:
                pass  # Preserve the transfer error; the reservation also expires server-side.
            raise
        return ticket['path']

    def download(self, remote, local, max_bytes):
        request = urllib.request.Request(self.base + '/drive/v1/download?' +
            urllib.parse.urlencode({'path': remote}), headers={'Authorization': 'Bearer ' + self.key})
        count, digest = 0, hashlib.sha256()
        with self.opener.open(request, timeout=300) as response, local.open('xb') as output:
            while True:
                block = response.read(1024 * 1024)
                if not block:
                    break
                count += len(block)
                if count > max_bytes:
                    raise ValueError('Downloaded file exceeds expected limit')
                output.write(block)
                digest.update(block)
        return {'bytes': count, 'sha256': digest.hexdigest()}

    def fetch(self, job, destination):
        if job['status'] in ACTIVE or not job.get('output_path'):
            raise ValueError('Outputs are not ready; use wait or status first')
        destination = destination.absolute()
        if destination.exists() or destination.is_symlink():
            raise ValueError('Result directory already exists; choose a new --directory')
        destination.parent.mkdir(parents=True, exist_ok=True)
        manifest = {}
        with tempfile.TemporaryDirectory(prefix='.compute-fetch-', dir=destination.parent) as tmp:
            stage = Path(tmp) / 'result'
            stage.mkdir()
            for name, limit in [('artifacts.zip', 512 * 1024 * 1024), ('stdout.txt', 4 * 1024 * 1024),
                                ('stderr.txt', 4 * 1024 * 1024), ('execution.json', 4 * 1024 * 1024)]:
                try:
                    manifest[name] = self.download(job['output_path'].rstrip('/') + '/' + name, stage/name, limit)
                except urllib.error.HTTPError as e:
                    if e.code == 404 and name == 'artifacts.zip' and (not job.get('workspace') or job['status'] != 'succeeded'):
                        continue  # Failed setup may produce logs without a workspace archive.
                    raise
            if (stage/'artifacts.zip').exists():
                extract_outputs(stage/'artifacts.zip', stage/'files')
            (stage/'job.json').write_text(json.dumps(job, ensure_ascii=False, indent=2))
            (stage/'downloads.json').write_text(json.dumps(manifest, indent=2))
            # An exclusive reservation prevents replacing existing user data.
            destination.mkdir()
            try:
                for item in stage.iterdir():
                    shutil.move(str(item), destination/item.name)
            except BaseException:
                print('Partial result directory retained: ' + str(destination), file=sys.stderr)
                raise
        return {'id': job['id'], 'directory': str(destination), 'files': manifest}


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
    run.add_argument('--fetch', type=Path, help='Wait and download outputs into a new local directory')
    run.add_argument('--dry-run', action='store_true', help='Print the exact file manifest without uploading or submitting')
    run.add_argument('argv', nargs=argparse.REMAINDER)
    submit = commands.add_parser('submit', help='Submit a standalone Python script')
    submit.add_argument('file', type=Path)
    submit.add_argument('--hardware', default='CPU')
    submit.add_argument('--timeout', type=int, default=180)
    submit.add_argument('--id')
    for name in ['status', 'cancel', 'wait', 'logs']:
        commands.add_parser(name).add_argument('id')
    fetch = commands.add_parser('fetch', help='Download logs and safely extract results to a new directory')
    fetch.add_argument('id')
    fetch.add_argument('--directory', type=Path, help='Default: ./results/JOB_UUID; must not exist')
    commands.add_parser('list')
    commands.add_parser('queue', help='Show recent jobs as a table (including completed jobs)')
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
        exclusions = list(args.output)
        if args.fetch:
            if args.fetch.exists() or args.fetch.is_symlink():
                parser.error('--fetch requires a new result directory')
            try:
                exclusions.append(args.fetch.resolve().relative_to(args.directory.resolve()).as_posix())
            except ValueError:
                pass
        files = workspace_files(args.directory.resolve(), exclusions)
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
        root = args.directory.resolve() if args.action == 'run' else args.file.resolve().parent
        record_submission(root, client.base, job_id)
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
        if args.action == 'run' and (args.wait or args.fetch):
            args.action, args.id = 'wait', job_id
    elif args.action == 'resources':
        result = client.call('/compute/v1/status')
    elif args.action in ['list', 'queue']:
        result = client.call(endpoint)
    else:
        args.id = str(uuid.UUID(args.id))
        result = client.call(endpoint + '/' + args.id + ('/cancel' if args.action == 'cancel' else ''), {} if args.action == 'cancel' else None)
    if args.action == 'wait':
        while True:
            result = client.call(endpoint + '/' + args.id)
            print(result['status'], file=sys.stderr)
            if result['status'] not in ACTIVE:
                break
            time.sleep(5)
    if args.action == 'logs':
        print(result.get('logs') or 'No logs available yet; full logs are published after execution.')
        if result.get('error'):
            print(result['error'], file=sys.stderr)
        return
    if args.action == 'queue':
        print('JOB ID                                STATE          HARDWARE       NAME')
        for job in result:
            title = ''.join(c if c.isprintable() else ' ' for c in job['title'])
            print(f"{job['id']}  {job['status']:<13}  {job['hardware']:<13}  {title}")
        return
    if args.action == 'fetch':
        result = client.fetch(result, args.directory or Path('results')/args.id)
    elif args.action == 'wait' and getattr(args, 'fetch', None) and result['status'] == 'succeeded':
        result['local_result'] = client.fetch(result, args.fetch)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    if isinstance(result, dict) and result.get('status') in ['failed', 'cancelled', 'unknown']:
        raise SystemExit(1)


if __name__ == '__main__':
    try:
        main()
    except urllib.error.HTTPError as e:
        raise SystemExit(f'HTTP {e.code}: ' + e.read(4096).decode(errors='replace'))
    except (ValueError, OSError, zipfile.BadZipFile) as e:
        raise SystemExit(str(e))
