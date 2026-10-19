"""Verify the updater contract without compiling a binary or touching release/."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('build_release', ROOT / 'build_release.py')
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


class RuntimePackageTests(unittest.TestCase):
    def test_default_version_matches_release_workflow(self):
        self.assertEqual(build.parse_args([]).version,
                         (ROOT / 'release-version.txt').read_text().strip())

    def test_invalid_version_cannot_escape_archive_directory(self):
        for raw in ('../outside', '1.2.3/extra', '1.2.3 -X private=value', '1.2', '1.2.3\n'):
            with self.subTest(raw=raw), self.assertRaises(argparse.ArgumentTypeError):
                build.release_version(raw)

    def test_compact_archive_matches_its_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            runtime = root / 'runtime'
            runtime.mkdir()
            for name in build.RUNTIME_FILES:
                (runtime / name).write_bytes(('test ' + name).encode())
            # Source, credentials and arbitrary neighbouring files stay out.
            (runtime / 'auth.json').write_text('{"private":"test-only"}')
            manifest = build.write_manifest(runtime, '1.2.3', 'arm64')
            archive = build.pack_runtime(runtime, root / 'dist', '1.2.3')
            with zipfile.ZipFile(archive) as package:
                expected = {'darkls-panel-update/' + n for n in (*build.RUNTIME_FILES, 'manifest.json')}
                self.assertEqual(set(package.namelist()), expected)
                self.assertEqual(json.loads(package.read('darkls-panel-update/manifest.json')), manifest)
                for name, entry in manifest['files'].items():
                    content = package.read('darkls-panel-update/' + name)
                    self.assertEqual(len(content), entry['size'])
                    self.assertEqual(hashlib.sha256(content).hexdigest(), entry['sha256'])


if __name__ == '__main__':
    unittest.main()
