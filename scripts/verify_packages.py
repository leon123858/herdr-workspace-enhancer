"""Validate distributable plugin archives without executing their binaries."""
import hashlib
from pathlib import Path, PurePosixPath
import sys
import tarfile
import tomllib
import zipfile


def verify(path):
    if path.suffix == '.zip':
        with zipfile.ZipFile(path) as archive:
            files = {i.filename: archive.read(i) for i in archive.infolist() if not i.is_dir()}
    else:
        with tarfile.open(path) as archive:
            files = {i.name: archive.extractfile(i).read() for i in archive.getmembers() if i.isfile()}
            for i in archive.getmembers():
                if '/bin/' in i.name and i.isfile():
                    assert i.mode & 0o111, 'Linux binary is not executable'
    for name in files:
        assert not PurePosixPath(name).is_absolute() and '..' not in PurePosixPath(name).parts
    manifests = [name for name in files if name.endswith('/herdr-plugin.toml')]
    assert len(manifests) == 1
    name = manifests[0]
    prefix = name.removesuffix('herdr-plugin.toml')
    manifest = tomllib.loads(files[name].decode())
    assert 'build' not in manifest, 'Download package must not require a compiler'
    assert len(manifest['platforms']) == 1
    for section in ('actions', 'panes'):
        assert manifest[section]
        for entry in manifest[section]:
            assert entry['platforms'] == manifest['platforms']
            assert prefix + entry['command'][0].removeprefix('./') in files
    assert prefix + 'README.md' in files and prefix + 'LICENSE' in files
    expected = path.with_name(path.name + '.sha256').read_text().split()[0]
    assert hashlib.sha256(path.read_bytes()).hexdigest() == expected
    print('Verified', path.name)


if __name__ == '__main__':
    paths = [p for p in Path(sys.argv[1] if len(sys.argv) > 1 else 'dist').iterdir()
             if p.name.endswith(('.tar.gz', '.zip'))]
    assert paths, 'No packages found'
    for path in paths:
        verify(path)
