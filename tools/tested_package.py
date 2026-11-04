#!/usr/bin/env python3
"""Bind a tested release archive to one commit/tree/workflow run before signing."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def verify(evidence, archive, expected):
    for key, value in expected.items():
        if evidence.get(key) != value:
            raise ValueError('Tested package mismatch: ' + key)
    if evidence.get('sha256') != hashlib.sha256(archive.read_bytes()).hexdigest():
        raise ValueError('Tested package checksum mismatch')


def main():
    mode = sys.argv[1]
    version = (ROOT / 'release-version.txt').read_text().strip()
    channel = (ROOT / 'release-channel.txt').read_text().strip()
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    tree = subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], cwd=ROOT, text=True).strip()
    if commit != os.environ['GITHUB_SHA']:
        raise ValueError('Checkout does not match workflow commit')
    expected = dict(commit=commit, tree=tree, run_id=os.environ['GITHUB_RUN_ID'], version=version, channel=channel)
    archive = ROOT / 'dist' / ('DARKLS-VPN-Panel-v' + version + '.zip')
    path = ROOT / 'dist' / 'tested-package.json'
    if mode == 'record':
        subprocess.run(['git', 'diff', '--exit-code', 'HEAD'], cwd=ROOT, check=True)
        if subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard'], cwd=ROOT, text=True).strip():
            raise ValueError('Untracked files in tested source tree')
        evidence = {**expected, 'sha256': hashlib.sha256(archive.read_bytes()).hexdigest()}
        path.write_text(json.dumps(evidence, sort_keys=True) + '\n')
    elif mode == 'verify':
        verify(json.loads(path.read_text()), archive, expected)
    else:
        raise ValueError('Expected record or verify')


if __name__ == '__main__':
    main()
