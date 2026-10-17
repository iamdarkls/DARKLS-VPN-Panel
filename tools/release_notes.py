#!/usr/bin/env python3
"""Extract the exact version's changelog for a GitHub Release; fail if absent."""
import argparse
from pathlib import Path
import re


def release_notes(text, version):
    if not re.fullmatch(r'\d{1,4}\.\d{1,4}\.\d{1,4}', version):
        raise ValueError('Invalid release version')
    sections = re.split(r'^## (.+)\s*$', text, flags=re.MULTILINE)
    matches = [sections[i + 1].strip() for i in range(1, len(sections), 2)
               if sections[i].strip() == version]
    if len(matches) != 1 or not matches[0]:
        raise ValueError('Expected exactly one nonempty changelog section for ' + version)
    return matches[0] + '\n'


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    notes = release_notes((Path(__file__).resolve().parents[1] / 'CHANGELOG.md').read_text(), args.version)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(notes)
