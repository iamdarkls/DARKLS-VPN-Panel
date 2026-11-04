import hashlib
from pathlib import Path
import tempfile
import unittest
from tested_package import verify


class TestedPackageTests(unittest.TestCase):
    def test_rejects_other_commit_run_and_modified_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / 'release.zip'
            archive.write_bytes(b'package')
            expected = dict(commit='exact-commit', tree='exact-tree', run_id='123', version='2.2.4', channel='stable')
            evidence = {**expected, 'sha256': hashlib.sha256(b'package').hexdigest()}
            verify(evidence, archive, expected)
            for key in expected:
                with self.assertRaises(ValueError):
                    verify({**evidence, key: 'different'}, archive, expected)
            archive.write_bytes(b'changed')
            with self.assertRaises(ValueError):
                verify(evidence, archive, expected)
