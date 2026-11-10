#!/usr/bin/env python3
"""Build a self-contained plugin archive; requires Go only on the build host."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile


def manifest(source, version, target):
    sections = re.split(r'(?=^\[\[)', source, flags=re.M)
    header = re.sub(r'^version = .*$', f'version = "{version}"', sections[0], flags=re.M)
    header = re.sub(r'^platforms = .*$', f'platforms = ["{target}"]', header, flags=re.M)
    kept = [header]
    for section in sections[1:]:
        if section.startswith('[[build]]'):
            continue
        platforms = re.search(r'^platforms = (.*)$', section, re.M)
        if platforms and f'"{target}"' not in platforms.group(1):
            continue
        kept.append(section)
    return ''.join(kept)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--os', choices=['linux', 'windows'], required=True)
    parser.add_argument('--arch', choices=['amd64', 'arm64'], required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', default='dist')
    args = parser.parse_args()
    version = args.version.removeprefix('v')
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?', version):
        parser.error('version must be a semantic version, e.g. v0.1.0')
    root = Path(__file__).resolve().parent.parent
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    name = f'herdr-workspace-enhancer_{version}_{args.os}_{args.arch}'
    with tempfile.TemporaryDirectory() as temp:
        package = Path(temp) / name
        (package / 'bin').mkdir(parents=True)
        binary = package / 'bin' / ('herdr-workspace-enhancer' + ('.exe' if args.os == 'windows' else ''))
        env = dict(os.environ, GOOS=args.os, GOARCH=args.arch, CGO_ENABLED='0')
        subprocess.run(['go', 'build', '-trimpath', '-ldflags=-s -w', '-o', str(binary), './cmd/herdr-workspace-enhancer'], cwd=root, env=env, check=True)
        binary.chmod(0o755)
        (package / 'herdr-plugin.toml').write_text(manifest((root / 'herdr-plugin.toml').read_text(), version, args.os), encoding='utf-8')
        for file in ['README.md', 'LICENSE']:
            shutil.copy2(root / file, package / file)
        if args.os == 'windows':
            archive = output / (name + '.zip')
            with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as z:
                for file in sorted(package.rglob('*')):
                    if file.is_file():
                        z.write(file, file.relative_to(package.parent))
        else:
            archive = output / (name + '.tar.gz')
            with tarfile.open(archive, 'w:gz') as tar:
                tar.add(package, arcname=name)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_name(archive.name + '.sha256').write_text(f'{digest}  {archive.name}\n', encoding='ascii')
    print(archive)


if __name__ == '__main__':
    main()
