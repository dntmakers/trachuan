import hashlib
import re


SUPPORTED_SCHEMA_VERSIONS = frozenset((4, 5, 6, 7, 8, 9))


def verified_schema(directory, lines, row, signed_manifest, decode):
    if signed_manifest is not None:
        if not isinstance(signed_manifest, dict):
            raise ValueError('invalid signed manifest')
        schema = signed_manifest.get('schema_version')
        if type(schema) is not int or schema not in SUPPORTED_SCHEMA_VERSIONS:
            raise ValueError('unsupported signed schema')
        if type(row.get('schema_version')) is not int or row['schema_version'] != schema:
            raise ValueError('release schema differs from signed schema')
        if schema < 8:
            return schema, None, None
    build_lines = [line for line in lines if line.endswith('  build.json')]
    if build_lines:
        if len(build_lines) != 1 or not re.fullmatch(r'[0-9a-f]{64}  build\.json', build_lines[0]):
            raise ValueError('invalid build.json manifest line')
        path = directory / 'build.json'
        if path.stat().st_size > 16 * 1024 * 1024:
            raise ValueError('build record exceeds size limit')
        with path.open('rb') as stream:
            data = stream.read(16 * 1024 * 1024 + 1)
        if len(data) > 16 * 1024 * 1024:
            raise ValueError('build record exceeds size limit')
        digest = hashlib.sha256(data).hexdigest()
        if build_lines[0] != digest + '  build.json':
            raise ValueError('build.json differs from build manifest')
        record = decode(data)
        if not isinstance(record, dict):
            raise ValueError('invalid build record')
        if signed_manifest is None:
            schema = record.get('schema_version')
        expected = (digest, len(data))
    else:
        raise ValueError('build manifest requires build.json')
    if type(schema) is not int or schema not in SUPPORTED_SCHEMA_VERSIONS:
        raise ValueError('unsupported build schema')
    if type(row.get('schema_version')) is not int or row['schema_version'] != schema:
        raise ValueError('release schema differs from verified schema')
    if signed_manifest is not None and signed_manifest.get('schema_version') != schema:
        raise ValueError('signed schema differs from verified schema')
    return schema, record, expected
