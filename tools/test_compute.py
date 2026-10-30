import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('compute', Path(__file__).with_name('compute.py'))
cli = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cli)

class WorkspaceTests(unittest.TestCase):
    def test_manifest_excludes_secrets_symlinks_and_outputs(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            for name in ['main.py', '.env', 'key.pem', 'credentials.json', 'data.txt']:
                (root/name).write_text('test')
            (root/'outputs').mkdir()
            (root/'outputs/model.bin').write_bytes(b'weights')
            (root/'linked').symlink_to(root/'main.py')
            (root/'.computeignore').write_text('data.txt\n')
            self.assertEqual([rel for _,rel in cli.workspace_files(root,['outputs'])], ['main.py'])
    def test_disallows_insecure_origin(self):
        for origin in ['http://example.com','https://key@example.com','https://example.com/path']:
            with self.assertRaises(ValueError):
                cli.Client(origin,'secret')

if __name__ == '__main__': unittest.main()
