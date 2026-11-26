import argparse
import hashlib
import re
from pathlib import Path
import subprocess
import tempfile

from corpus_evidence import LIMITS, SIDECARS
from corpus_manifest import decode, identity, sha256
from manifest import require_match
from object_store import ObjectReader, require_free_space, require_pinned_cli, s3_endpoint


def download(country, rel, report_sha256, release_sha256, directory):
    identity(country, rel)
    require_match(r'[0-9a-f]{64}', report_sha256, 'report SHA-256')
    if release_sha256 != '':
        require_match(r'[0-9a-f]{64}', release_sha256, 'release SHA-256')
    endpoint = s3_endpoint()
    require_pinned_cli()
    directory.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory() as temporary:
        reader = ObjectReader('trachuan-releases', endpoint, temporary)
        limits = {'corpus.dump': 16 * 1024**3, 'MANIFEST.sha256': 1024**2,
                  'report.json': 16 * 1024**2, 'release.json': 16 * 1024**2,
                  'build.json': 16 * 1024**2} | LIMITS
        objects = {}
        for name, limit in limits.items():
            try:
                objects[name] = reader.head(f'{country}/{rel}/{name}', name, limit)
            except subprocess.CalledProcessError as exc:
                if name == 'build.json' or name in SIDECARS:
                    if re.search(r'\((?:404|NoSuchKey|NotFound)\)', exc.stderr or ''):
                        continue
                    raise ValueError('cannot read object: ' + name) from exc
                raise
        require_free_space(objects, directory, 'corpus')

        def fetch(name):
            expected, etag = objects[name]
            reader.get(f'{country}/{rel}/{name}', name, directory / name, expected, etag)

        fetch('report.json')
        if sha256(directory / 'report.json') != report_sha256:
            raise ValueError('report differs from the dispatched SHA-256')
        fetch('release.json')
        row_bytes = (directory / 'release.json').read_bytes()
        if release_sha256 and hashlib.sha256(row_bytes).hexdigest() != release_sha256:
            raise ValueError('release.json differs from the dispatched SHA-256')
        row = decode(row_bytes)
        if not isinstance(row, dict) or row.get('country') != country or row.get('rel') != rel:
            raise ValueError('downloaded release identity differs from request')
        fetch('MANIFEST.sha256')
        report = decode((directory / 'report.json').read_bytes())
        manifest = (directory / 'MANIFEST.sha256').read_bytes()
        if not isinstance(report, dict) or report.get('build_artifact_sha256') != hashlib.sha256(manifest).hexdigest():
            raise ValueError('build manifest differs from report')
        if not isinstance(row.get('run'), dict):
            raise ValueError('downloaded release row has no run')
        for name in objects:
            if name not in ('report.json', 'release.json', 'MANIFEST.sha256'):
                fetch(name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('country')
    parser.add_argument('rel')
    parser.add_argument('report_sha256')
    parser.add_argument('release_sha256')
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    download(args.country, args.rel, args.report_sha256, args.release_sha256, args.directory)


if __name__ == '__main__':
    main()
