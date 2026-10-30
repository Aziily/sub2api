import importlib.util
from pathlib import Path
import tempfile
import unittest
import json
import stat
import zipfile
from unittest.mock import patch

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

    def test_extract_rejects_traversal_symlink_and_duplicate_names(self):
        for names in [['../outside'], ['/absolute'], ['a', 'A'], ['a\\b'], ['a:b'], ['link']]:
            with self.subTest(names=names), tempfile.TemporaryDirectory() as d:
                root = Path(d)
                with zipfile.ZipFile(root/'result.zip', 'w') as z:
                    for name in names:
                        item = zipfile.ZipInfo(name)
                        if name == 'link':
                            item.external_attr = (stat.S_IFLNK | 0o777) << 16
                        z.writestr(item, 'payload')
                with self.assertRaises(ValueError):
                    cli.extract_outputs(root/'result.zip', root/'files')
                self.assertFalse((root/'files').exists())

    def test_fetch_roundtrip_and_refuses_overwrite(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            with zipfile.ZipFile(root/'source.zip', 'w') as z:
                z.writestr('outputs/result.json', '{"sum":55}')
            client = cli.Client('https://example.com', 'not-to-be-saved')
            def download(remote, target, limit):
                target.write_bytes((root/'source.zip').read_bytes() if remote.endswith('.zip') else b'log')
                return {'bytes': target.stat().st_size}
            job = {'id': 'job', 'status': 'succeeded', 'output_path': '/Colab-job', 'workspace': {'input_path':'/input.zip'}}
            with patch.object(client, 'download', side_effect=download) as transfer:
                result = client.fetch(job, root/'result')
                self.assertEqual(json.loads((root/'result/files/outputs/result.json').read_text()), {'sum':55})
                self.assertNotIn(client.key, (root/'result/job.json').read_text())
                self.assertEqual(len(result['files']), 4)
                with self.assertRaises(ValueError): client.fetch(job, root/'result')
                self.assertEqual(transfer.call_count, 4)

    def test_fetch_failure_does_not_publish_partial_results(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            client = cli.Client('https://example.com','key')
            with patch.object(client, 'download', side_effect=OSError('interrupted')):
                with self.assertRaises(OSError):
                    client.fetch({'id':'job','status':'succeeded','output_path':'/output'}, root/'result')
            self.assertFalse((root/'result').exists())
            self.assertEqual(list(root.iterdir()), [])

    def test_receipt_records_no_key_and_is_excluded_from_submission(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            cli.record_submission(root, 'https://example.com', 'job')
            self.assertEqual(json.loads((root/'.sub2api/jobs/job.json').read_text()), {'id':'job','base':'https://example.com'})
            self.assertEqual(cli.workspace_files(root, ['outputs']), [])

if __name__ == '__main__': unittest.main()
