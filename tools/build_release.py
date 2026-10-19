#!/usr/bin/env python3
"""Build the Linux runtime package accepted by SSH and authenticated panel upload."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]
RUNTIME_FILES = (
    'darkls-panel',
    'install-update.sh',
    'reload-dns.sh',
    'devices-sync.sh',
    'watch.sh',
)
VERSION_PATTERN = re.compile(r'\d{1,4}\.\d{1,4}\.\d{1,4}')


def release_version(raw):
    """Validate before a version is used in a linker flag or archive filename."""
    if not VERSION_PATTERN.fullmatch(raw):
        raise argparse.ArgumentTypeError('invalid release version')
    return raw


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    # One default source for both local builds and the release workflow.
    parser.add_argument('--version', type=release_version,
                        default=(ROOT / 'release-version.txt').read_text().strip())
    parser.add_argument('--arch', default='arm64', choices=['arm64', 'amd64'])
    parser.add_argument('--output-dir', type=Path, default=ROOT / 'dist')
    return parser.parse_args(argv)


def build_binary(root, version, arch):
    binary = root / 'release' / 'darkls-panel'
    env = {**os.environ, 'CGO_ENABLED': '0', 'GOOS': 'linux', 'GOARCH': arch}
    subprocess.run([
        os.environ.get('GO_BIN', 'go'), 'build', '-buildvcs=false', '-trimpath',
        '-gcflags=all=-l', '-ldflags=-s -w -buildid= -X darkls-panel/internal/panel.version=' + version,
        '-o', str(binary), './cmd/darkls-panel',
    ], cwd=root, env=env, check=True)
    return binary


def write_manifest(runtime_dir, version, arch):
    files = {}
    for name in RUNTIME_FILES:
        data = (runtime_dir / name).read_bytes()
        files[name] = {'sha256': hashlib.sha256(data).hexdigest(), 'size': len(data)}
    manifest = {'format': 1, 'version': version, 'arch': arch, 'files': files}
    (runtime_dir / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    return manifest


def pack_runtime(runtime_dir, output_dir, version):
    """Only the six runtime entries belong in the router's update archive."""
    output_dir.mkdir(parents=True, exist_ok=True)
    archive = output_dir / f'DARKLS-VPN-Panel-v{version}.zip'
    with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as target:
        for name in (*RUNTIME_FILES, 'manifest.json'):
            target.write(runtime_dir / name, 'darkls-panel-update/' + name)
    return archive


def main(argv=None):
    args = parse_args(argv)
    binary = build_binary(ROOT, args.version, args.arch)
    write_manifest(binary.parent, args.version, args.arch)
    archive = pack_runtime(binary.parent, args.output_dir, args.version)
    print(json.dumps({
        'path': str(archive.resolve()),
        'bytes': archive.stat().st_size,
        'binary_bytes': binary.stat().st_size,
        'sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
    }))


if __name__ == '__main__':
    main()
