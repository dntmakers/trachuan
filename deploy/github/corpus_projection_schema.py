import json

from corpus_projection import canonical_payload


BINDINGS = ('rel', 'dump_sha256', 'build_artifact_sha256', 'report_sha256',
            'predecessor_rel', 'predecessor_identity_sha256')
RANKS = {name: i for i, name in enumerate(('document', 'document_key',
    'document_legal', 'document_metadata', 'document_provenance', 'provision',
    'provision_classification', 'provision_validity', 'relation'), 1)}


def parse(raw):
    try:
        return json.loads(canonical_payload(raw))
    except (ValueError, RecursionError) as exc:
        raise ValueError('invalid JSON object') from exc


def _object(value, required, optional=None):
    optional = optional or {}
    if not isinstance(value, dict) or set(value) - (set(required) | set(optional)) or set(required) - set(value):
        raise ValueError('invalid object schema')
    for key, kind in {**required, **optional}.items():
        if key in value:
            _value(value[key], kind)


def _value(value, kind):
    if kind == 'string':
        valid = isinstance(value, str)
    elif kind == 'nullable':
        valid = value is None or isinstance(value, str)
    elif kind == 'bool':
        valid = type(value) is bool
    elif kind == 'int':
        valid = type(value) is int and -(2**63) <= value < 2**63
    elif kind == 'positive':
        valid = type(value) is int and 0 < value < 2**63
    elif kind in ('strings', 'validities'):
        valid = isinstance(value, list)
        if valid:
            for item in value:
                _value(item, 'string' if kind == 'strings' else 'validity')
    else:
        schemas = {
            'string_pair': ({'before': 'string', 'after': 'string'}, {}),
            'date_pair': ({'before': 'nullable', 'after': 'nullable'}, {}),
            'bool_pair': ({'before': 'bool', 'after': 'bool'}, {}),
            'validity': ({'status_class': 'string', 'eff_from': 'nullable', 'eff_to': 'nullable', 'method': 'string'}, {}),
            'end': ({'key': 'nullable', 'number': 'string', 'in_release': 'bool', 'anchor': 'nullable'}, {'exact': 'bool', 'named': 'string', 'node': 'string'}),
        }
        if kind not in schemas:
            raise ValueError('unknown schema')
        _object(value, *schemas[kind])
        return
    if not valid:
        raise ValueError('invalid value')


def header(value):
    if not isinstance(value, dict) or set(value) != {'schema', 'bindings', 'baseline'}:
        raise ValueError('invalid header')
    _value(value['schema'], 'int')
    _value(value['baseline'], 'bool')
    _object(value['bindings'], dict.fromkeys(BINDINGS, 'string'))
    if value['schema'] != 1 or any(any(c in v for c in '\t\n\x00') for v in value['bindings'].values()):
        raise ValueError('invalid header')


def record(value):
    if not isinstance(value, dict):
        raise ValueError('invalid record')
    kind = value.get('type')
    if kind not in RANKS:
        raise ValueError('unknown record kind')
    required = {'type': 'string', 'key': 'string'}
    optional = {}
    if kind == 'document':
        required.update(change='string', cause='string')
        optional['replaced_by'] = 'string'
    elif kind == 'document_key':
        required['before'] = 'string'
    elif kind == 'document_legal':
        optional.update(status_class='string_pair', eff_from='date_pair', eff_to='date_pair', is_current='bool_pair', validity_conflict='bool_pair')
    elif kind == 'document_metadata':
        required['number_norm'] = 'string_pair'
    elif kind == 'document_provenance':
        optional.update(text_source='string_pair', text_complete='bool_pair', baseline='bool_pair')
    elif kind.startswith('provision'):
        required['anchor'] = 'string'
        optional.update(provenance='bool', ambiguous='bool')
        if kind == 'provision':
            required.update(change='string', text_sha256='date_pair')
        else:
            required.update(added='validities' if kind == 'provision_validity' else 'strings', removed='validities' if kind == 'provision_validity' else 'strings')
    else:
        required = {'type': 'string', 'kind': 'string', 'method': 'string', 'source': 'end', 'target': 'end', 'change': 'string', 'count': 'positive'}
    _object(value, required, optional)
    for name in ('key', 'before', 'anchor', 'cause', 'change', 'kind', 'method', 'replaced_by'):
        if name in value:
            text = value[name]
            if not text or (name in ('key', 'before', 'replaced_by') and any(c in text for c in '\t\n\x00')) or (name in ('key', 'before', 'anchor', 'replaced_by') and len(text) > 512):
                raise ValueError('invalid record field')
    if kind in ('document', 'provision', 'relation') and value['change'] not in (('added', 'removed', 'changed') if kind == 'provision' else ('added', 'removed')):
        raise ValueError('invalid change')
    if kind == 'document' and value['change'] == 'added' and 'replaced_by' in value:
        raise ValueError('invalid replacement')
    fields = {'document_legal': ('status_class', 'eff_from', 'eff_to', 'is_current', 'validity_conflict'), 'document_provenance': ('text_source', 'text_complete', 'baseline'), 'document_metadata': ('number_norm',)}
    if kind in fields and not any(k in value for k in fields[kind]):
        raise ValueError('missing changed field')
    if kind == 'provision_validity':
        for item in value['added'] + value['removed']:
            if any(not item[k] for k in ('status_class', 'method')) or any(item[k] == '' for k in ('eff_from', 'eff_to')):
                raise ValueError('invalid validity')
    return kind
