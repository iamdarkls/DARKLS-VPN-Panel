"""Source changes must never leave an old UI embedded in a release."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import build_assets


class AssetFreshnessTests(unittest.TestCase):
    def test_changed_or_added_source_requires_regeneration(self):
        with tempfile.TemporaryDirectory() as directory:
            web = Path(directory)
            (web / 'js').mkdir()
            (web / 'index.html').write_text('<html>test</html>')
            script = web / 'js/test.js'
            script.write_text('const text = "test";')
            with patch.object(build_assets, 'WEB', web), patch.object(build_assets, 'GENERATED', web / 'assets_generated.go'):
                self.assertFalse(build_assets.check())
                build_assets.generate()
                self.assertTrue(build_assets.check())
                script.write_text('const text = "changed";')
                self.assertFalse(build_assets.check())
                build_assets.generate()
                self.assertTrue(build_assets.check())
                (web / 'js/extra.js').write_text('const extra = 1;')
                self.assertFalse(build_assets.check())


if __name__ == '__main__':
    unittest.main()
