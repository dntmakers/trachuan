import hashlib
import json
import re
from dataclasses import dataclass

from corpus_projection import VERSION, Hasher, canonical_payload
from corpus_projection_schema import RANKS, header, parse, record


@dataclass(frozen=True)
class Row:
    ordinal: int
    kind: str
    key: str
    key2: str | None
    payload: str


@dataclass(frozen=True)
class Projection:
    rows: list
    digest: str
    counts: dict


def _json(value):
    return (json.dumps(value, ensure_ascii=False, separators=(',', ':'))
            .replace('&', '\\u0026').replace('<', '\\u003c')
            .replace('>', '\\u003e').replace('\u2028', '\\u2028')
            .replace('\u2029', '\\u2029'))


def _identity(value, kind):
    if kind != 'relation':
        return (value['key'] + '\x00' + value.get('anchor', '')).encode()
    def end(name):
        return {k: value[name][k] for k in ('key', 'number', 'in_release', 'anchor', 'exact', 'named', 'node') if k in value[name]}
    encoded = (json.dumps({'type': '', 'kind': value['kind'], 'method': value['method'],
                           'source': end('source'), 'target': end('target')},
                          ensure_ascii=False, separators=(',', ':'))
               .replace('\u2028', '\\u2028').replace('\u2029', '\\u2029')).encode()
    if len(encoded) > 1024 * 1024:
        raise ValueError('relation identity too long')
    return encoded


def _records(source, position):
    source.seek(position)
    digest = hashlib.sha256()
    line_number = 0
    while True:
        raw = source.readline(1024 * 1024 + 2)
        if not raw:
            break
        line_number += 1
        digest.update(raw)
        line = raw[:-1] if raw.endswith(b'\n') else raw
        if len(line) > 1024 * 1024:
            raise ValueError('line too long')
        if line.endswith(b'\r'):
            line = line[:-1]
        try:
            value = parse(line)
            if line_number == 1:
                header(value)
            else:
                record(value)
        except (ValueError, TypeError, KeyError) as exc:
            raise ValueError('invalid sidecar line') from exc
        yield value, line
    if not line_number:
        raise ValueError('missing header')
    return digest.digest()


def _pass(source, position, visit):
    iterator = _records(source, position)
    first, _ = next(iterator)
    previous_rank = 0
    previous_id = b''
    while True:
        try:
            value, raw = next(iterator)
        except StopIteration as stop:
            return first, stop.value
        kind = value['type']
        identity = _identity(value, kind)
        rank = RANKS[kind]
        if rank < previous_rank or (rank == previous_rank and identity <= previous_id):
            raise ValueError('invalid record order')
        previous_rank, previous_id = rank, identity
        if first['baseline']:
            raise ValueError('baseline has records')
        visit(value, raw)


def _fields(value, names):
    return {name: value[name] for name in names if name in value}


def _raw_fields(raw):
    text = raw.decode('utf-8')
    decoder = json.JSONDecoder()
    position = len(text) - len(text.lstrip(' \t\r\n')) + 1
    result = {}
    while True:
        while text[position] in ' \t\r\n':
            position += 1
        if text[position] == '}':
            return result
        name, position = decoder.raw_decode(text, position)
        while text[position] in ' \t\r\n:':
            position += 1
        start = position
        _, position = decoder.raw_decode(text, position)
        token = text[start:position]
        compact = []
        quoted = escaped = False
        for char in token:
            if quoted:
                compact.append(char)
                if escaped:
                    escaped = False
                elif char == '\\':
                    escaped = True
                elif char == '"':
                    quoted = False
            elif char == '"':
                quoted = True
                compact.append(char)
            elif char not in ' \t\r\n':
                compact.append(char)
        result[name] = ''.join(compact)
        while position < len(text) and text[position] in ' \t\r\n,':
            position += 1


def _raw_object(fields):
    encoded = '{' + ','.join(_json(k) + ':' + fields[k] for k in sorted(fields)) + '}'
    return (encoded.replace('&', '\\u0026').replace('<', '\\u003c')
            .replace('>', '\\u003e').replace('\u2028', '\\u2028')
            .replace('\u2029', '\\u2029'))


def _context(value, raw, contexts, raw_contexts):
    kind, key = value['type'], value.get('key')
    if kind not in ('document_key', 'document_provenance'):
        return
    if key not in contexts and len(contexts) >= 10000:
        raise ValueError('source context limit')
    state = contexts.setdefault(key, {})
    raw_state = raw_contexts.setdefault(key, {})
    raw_values = _raw_fields(raw)
    if 'before' in value:
        state['before_key'] = value['before']
        raw_state['before_key'] = raw_values['before']
    names = ('text_source', 'text_complete', 'baseline')
    state.update(_fields(value, names))
    raw_state.update(_fields(raw_values, names))
    size = sum(len(v.encode()) for v in raw_state.values())
    encoded = _raw_object(raw_state)
    if size > 4096 or len(encoded.encode()) > 4096:
        raise ValueError('source context limit')


def _row_data(value, contexts):
    kind, key = value['type'], value.get('key')
    context = contexts.get(key)
    key2 = context.get('before_key') if context is not None else None
    if kind == 'document':
        yield 'document_' + value['change'], key, value.get('replaced_by'), _fields(value, ('key', 'cause', 'replaced_by'))
    elif kind == 'document_key':
        yield kind, key, value['before'], {'key': key, 'before_key': value['before']}
    elif kind == 'document_legal':
        payload = _fields(value, ('key', 'status_class', 'eff_from', 'eff_to', 'is_current', 'validity_conflict'))
        if context is not None:
            payload['source_change'] = context
        yield 'document_validity', key, key2, payload
    elif kind == 'provision':
        payload = _fields(value, ('key', 'anchor', 'change'))
        payload.update(provenance=value.get('provenance', False), ambiguous=value.get('ambiguous', False))
        yield 'provision_text', key, None, payload


def _make_row(ordinal, kind, key, key2, payload, raw, raw_contexts):
    item = {name: (raw['before'] if name == 'before_key' else
                   _raw_object(raw_contexts[key]) if name == 'source_change' else
                   raw[name] if name in raw else _json(value))
            for name, value in payload.items()}
    item['kind'] = _json(kind)
    if kind != 'document_removed' and (kind != 'provision_text' or (payload['change'] != 'removed' and not payload['ambiguous'])):
        handle = key
        if kind == 'provision_text':
            handle += '#' + payload['anchor']
        if len(handle) <= 512:
            item['handle'] = _json(handle)
    if len(_raw_object(item).encode()) > 4096:
        raise ValueError('projection item limit')
    text = canonical_payload(_json(payload).encode())
    return Row(ordinal, kind, key, key2, text)


def project_stream(source, country, row_limit):
    position = source.tell()
    contexts = {}
    raw_contexts = {}
    first, first_hash = _pass(source, position, lambda value, raw: _context(value, raw, contexts, raw_contexts))
    bindings = first['bindings']
    previous = bindings['predecessor_rel']
    identity = bindings['predecessor_identity_sha256']
    if not previous:
        raise ValueError('invalid predecessor binding')
    if first['baseline']:
        if identity != 'none':
            raise ValueError('invalid baseline binding')
    elif previous == 'none' or not re.fullmatch(r'sha256:[0-9a-f]{64}', identity):
        raise ValueError('invalid predecessor identity binding')
    if previous == 'none':
        previous = None
    if identity == 'none':
        identity = None
    else:
        identity = identity[7:]
    hasher = Hasher(VERSION, country, bindings['rel'], previous, identity, first['baseline'])
    rows = []
    total = len(('\t'.join(('v%d' % VERSION, country, bindings['rel'], previous or '', identity or '',
                            '1' if first['baseline'] else '0')) + '\n').encode())
    if total > row_limit:
        raise ValueError('projection size limit')
    def collect(value, raw):
        nonlocal total
        fields = _raw_fields(raw)
        for kind, key, key2, payload in _row_data(value, contexts):
            row = _make_row(len(rows) + 1, kind, key, key2, payload, fields, raw_contexts)
            line = '\t'.join((str(row.ordinal), row.kind, row.key or '', row.key2 or '', row.payload)) + '\n'
            total += len(line.encode())
            if total > row_limit:
                raise ValueError('projection size limit')
            hasher.add(row.ordinal, row.kind, row.key, row.key2, row.payload)
            rows.append(row)
    second, second_hash = _pass(source, position, collect)
    if first != second or first_hash != second_hash:
        raise ValueError('sidecar changed between passes')
    return Projection(rows, hasher.digest, hasher.counts)
