import hashlib
import json
import re


KINDS = ('document_added', 'document_removed', 'document_validity',
         'provision_validity', 'provision_text', 'document_key')


class _Integer(str):
    pass


def _invalid_number(_value):
    raise ValueError('invalid number')


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate key')
        result[key] = value
    return result


def _string(value):
    try:
        value.encode('utf-8')
    except UnicodeError as exc:
        raise ValueError('invalid Unicode') from exc
    output = ['"']
    for char in value:
        if char == '"':
            output.append('\\"')
        elif char == '\\':
            output.append('\\\\')
        elif ord(char) < 32:
            output.append('\\u%04x' % ord(char))
        else:
            output.append(char)
    output.append('"')
    return ''.join(output)


def _canonical(value):
    if value is None:
        return 'null'
    if value is True:
        return 'true'
    if value is False:
        return 'false'
    if isinstance(value, _Integer):
        return '0' if value == '-0' else value
    if isinstance(value, str):
        return _string(value)
    if isinstance(value, list):
        return '[' + ','.join(_canonical(item) for item in value) + ']'
    if isinstance(value, dict):
        return '{' + ','.join(_string(key) + ':' + _canonical(value[key])
                              for key in sorted(value)) + '}'
    raise ValueError('invalid JSON value')


def canonical_payload(raw):
    try:
        text = raw.decode('utf-8') if isinstance(raw, bytes) else raw
        value = json.loads(text, object_pairs_hook=_object, parse_int=_Integer,
                           parse_float=_invalid_number, parse_constant=_invalid_number)
        if not isinstance(value, dict):
            raise ValueError('payload is not an object')
        return _canonical(value)
    except (UnicodeError, RecursionError, TypeError) as exc:
        raise ValueError('invalid JSON') from exc


def _field(value, optional=False):
    if value is None and optional:
        return ''
    if not isinstance(value, str) or not value or any(c in value for c in '\t\n\x00'):
        raise ValueError('invalid field')
    try:
        value.encode('utf-8')
    except UnicodeError as exc:
        raise ValueError('invalid field') from exc
    return value


class Hasher:
    def __init__(self, version, country, release, predecessor=None,
                 predecessor_identity_hash=None, baseline=False, output=None):
        if type(version) is not int or version != 1:
            raise ValueError('unknown projection version')
        if type(baseline) is not bool:
            raise ValueError('invalid baseline')
        country = _field(country)
        release = _field(release)
        predecessor = _field(predecessor, optional=True)
        identity = _field(predecessor_identity_hash, optional=True)
        if identity and not re.fullmatch('[0-9a-f]{64}', identity):
            raise ValueError('invalid predecessor identity hash')
        self.baseline = baseline
        self.ordinal = 0
        self.counts = dict.fromkeys(KINDS, 0)
        self._hash = hashlib.sha256()
        self._output = output
        self._write('\t'.join(('v1', country, release, predecessor, identity,
                               '1' if baseline else '0')) + '\n')

    def _write(self, line):
        data = line.encode('utf-8')
        if self._output is not None:
            self._output.write(data)
        self._hash.update(data)

    def add(self, ordinal, kind, key, key2, payload):
        if self.baseline or type(ordinal) is not int or ordinal != self.ordinal + 1:
            raise ValueError('invalid ordinal or baseline')
        if kind not in self.counts:
            raise ValueError('unknown kind')
        key = _field(key, optional=True)
        key2 = _field(key2, optional=True)
        payload = _field(payload)
        if canonical_payload(payload.encode('utf-8')) != payload:
            raise ValueError('noncanonical payload')
        self._write('\t'.join((str(ordinal), kind, key, key2, payload)) + '\n')
        self.ordinal = ordinal
        self.counts[kind] += 1

    @property
    def digest(self):
        return self._hash.hexdigest()


def project(source, country, row_limit=128 * 1024 * 1024):
    from corpus_projection_stream import project_stream
    return project_stream(source, country, row_limit)
