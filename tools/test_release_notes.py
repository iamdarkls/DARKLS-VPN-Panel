import unittest
from release_notes import release_notes


class ReleaseNotesTest(unittest.TestCase):
    def test_exact_section_only(self):
        text = '# Changes\n\n## 1.4.1\n\n- New.\n\n### Details\nMore.\n\n## 1.4.0\n\n- Old.\n'
        self.assertEqual(release_notes(text, '1.4.1'), '- New.\n\n### Details\nMore.\n')

    def test_missing_duplicate_empty_and_invalid_fail(self):
        for text, version in [('', '1.4.1'), ('## 1.4.1\n', '1.4.1'),
                              ('## 1.4.1\nA\n## 1.4.1\nB', '1.4.1'),
                              ('## 1.4.1\nA', '../1.4.1')]:
            with self.subTest(text=text, version=version), self.assertRaises(ValueError):
                release_notes(text, version)


if __name__ == '__main__':
    unittest.main()
