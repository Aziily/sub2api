# Runs only inside the isolated provider runtime, never on the gateway.
import os, pathlib, shutil, stat, subprocess, tempfile, urllib.request, zipfile
_cp_root = pathlib.Path(tempfile.mkdtemp(prefix='workspace-'))
_cp_archive = _cp_root.parent / (_cp_root.name + '.zip')
_cp_request = urllib.request.Request(_cp_config['url'], headers={'X-Compute-Transfer': _cp_config.pop('token')})
with urllib.request.urlopen(_cp_request, timeout=180) as response, _cp_archive.open('wb') as dst:
    shutil.copyfileobj(response, dst)
del _cp_request
with zipfile.ZipFile(_cp_archive) as archive:
    items = archive.infolist()
    if len(items) > 10000 or sum(i.file_size for i in items) > 256 * 1024 * 1024:
        raise ValueError('Workspace exceeds 10000 files or 256 MiB extracted')
    for item in items:
        p = pathlib.PurePosixPath(item.filename)
        if p.is_absolute() or '..' in p.parts or '\\' in item.filename or stat.S_ISLNK(item.external_attr >> 16):
            raise ValueError('Unsafe workspace path')
    archive.extractall(_cp_root)
_cp_archive.unlink()
_cp_exit = subprocess.run(_cp_config['command'], cwd=_cp_root).returncode
_cp_total = 0
_cp_seen = set()
with zipfile.ZipFile('/tmp/claudepool-artifacts.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
    for name in _cp_config['outputs']:
        target = _cp_root / name
        for p in ([target] if target.is_file() else target.rglob('*')):
            if p.is_symlink() or not p.is_file() or not p.resolve().is_relative_to(_cp_root):
                continue
            name = str(p.relative_to(_cp_root))
            if name in _cp_seen:
                continue
            _cp_seen.add(name)
            _cp_total += p.stat().st_size
            if _cp_total > 512 * 1024 * 1024 or len(_cp_seen) > 10000:
                raise ValueError('Output exceeds 512 MiB or 10000 files')
            archive.write(p, name)
print('Workspace command exit code:', _cp_exit)
if _cp_exit:
    raise RuntimeError('Workspace command failed: ' + str(_cp_exit))
