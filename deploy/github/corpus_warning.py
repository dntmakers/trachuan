import hashlib
import json
import re

from corpus_fixture_history import REPOSITORY, FixtureHistory, git, unique_object
from manifest import canonical


FIELDS = {'entry_id', 'question', 'target', 'acting', 'relation_kind'}
KINDS = {'repeals', 'amends_supplements', 'replaces', 'suspends'}


def scalar_json(value):
    try:
        return canonical(value)
    except UnicodeEncodeError:
        raise ValueError('invalid Unicode scalar') from None


def handle(value, country, allow_ambiguous=False):
    if not isinstance(value, str) or value.count('#') != 1 or any(ord(c) < 32 or 127 <= ord(c) <= 159 for c in value):
        raise ValueError('invalid warning handle')
    key, anchor = value.split('#')
    if not key.startswith(country + ':') or len(key) <= len(country) + 1 or any(c.isspace() for c in key):
        raise ValueError('invalid warning document key')
    if not allow_ambiguous and '~' in anchor:
        raise ValueError('ambiguous warning anchor')
    for part in anchor.split('/'):
        kind, separator, token = part.partition('-')
        if not separator or not token or not re.fullmatch('[a-z]+', kind) or part.lower() != part or any(c.isspace() for c in part):
            raise ValueError('invalid warning anchor')
    return key


def warning_cases(data, country):
    if len(data) > 65536:
        raise ValueError('warning fixture exceeds size limit')
    try:
        cases = json.loads(data.decode('utf-8'), object_pairs_hook=unique_object,
                           parse_constant=lambda value: (_ for _ in ()).throw(ValueError('invalid JSON constant')))
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise ValueError('invalid warning fixture JSON') from exc
    scalar_json(cases)
    if not isinstance(cases, list) or len(cases) > 20:
        raise ValueError('invalid warning fixture array')
    previous = ''
    for case in cases:
        if not isinstance(case, dict) or set(case) != FIELDS or any(not isinstance(value, str) for value in case.values()):
            raise ValueError('invalid warning fixture fields')
        entry, question = case['entry_id'], case['question']
        if not re.fullmatch('[a-z0-9][a-z0-9-]{2,63}', entry) or entry <= previous:
            raise ValueError('warning entry IDs must be unique and sorted')
        if not question.strip() or len(question) > 2000 or '\x00' in question:
            raise ValueError('invalid warning question')
        if handle(case['target'], country) == handle(case['acting'], country) or case['relation_kind'] not in KINDS:
            raise ValueError('invalid warning binding')
        previous = entry
    return cases


def validate_match(match, case, country, read=False):
    fields = {'carrier', 'dir', 'kind', 'key', 'in_release', 'source', 'target', 'exact'}
    if not isinstance(match, dict) or set(match) != fields:
        raise ValueError('invalid warning match fields')
    carrier_key = handle(match['carrier'], country, allow_ambiguous=True)
    if carrier_key != handle(case['target'], country) or (read and match['carrier'] != case['target']):
        raise ValueError('invalid warning carrier')
    if (match['dir'] != 'in' or match['kind'] != case['relation_kind'] or
            match['key'] != handle(case['acting'], country) or match['in_release'] is not True or
            match['source'] != case['acting'] or match['target'] != case['target'] or match['exact'] is not True):
        raise ValueError('warning match differs from fixture')


def validate_warnings(report, cases, country):
    records = report.get('warning_cases')
    if not isinstance(records, list) or len(records) != len(cases):
        raise ValueError('warning cases differ from fixture')
    not_run = report.get('not_run')
    if not isinstance(not_run, dict):
        raise ValueError('missing not_run')
    if not cases:
        if not_run.get('warning_cases') != 'not_run':
            raise ValueError('missing warning not_run marker')
        return True
    if 'warning_cases' in not_run:
        raise ValueError('unexpected warning not_run marker')
    matrix = [(mode, limit, detail) for mode in ('exact', 'ann') for limit in (8, 20) for detail in ('compact', 'full')]
    for row, case in zip(records, cases):
        if not isinstance(row, dict) or set(row) != {'entry_id', 'outcome', 'search', 'read'}:
            raise ValueError('invalid warning case fields')
        if row['entry_id'] != case['entry_id'] or row['outcome'] not in ('applied', 'redundant'):
            raise ValueError('warning case did not pass')
        search = row['search']
        if not isinstance(search, list) or len(search) != len(matrix):
            raise ValueError('invalid warning search matrix')
        for surface, identity in zip(search, matrix):
            if not isinstance(surface, dict) or set(surface) != {'mode', 'limit', 'detail', 'match'}:
                raise ValueError('invalid warning search fields')
            if (surface['mode'], surface['limit'], surface['detail']) != identity or type(surface['limit']) is not int:
                raise ValueError('invalid warning search order')
            validate_match(surface['match'], case, country)
        validate_match(row['read'], case, country, read=True)
    return True


WARNING_PATH = 'apps/corpus/internal/evaluation/testdata/vn/warning-cases.json'


def validate_authorized(report, country, *, commit=None, verified_report_hash=None, report_bytes=None, root=REPOSITORY):
    if (commit is None) == (verified_report_hash is None):
        raise ValueError('select exactly one warning authorization')
    if not isinstance(report, dict):
        raise ValueError('invalid report')
    report_json = scalar_json(report)
    if commit is not None and (not isinstance(commit, str) or not (commit == 'refs/heads/main' or re.fullmatch('[0-9a-f]{40}', commit))):
        raise ValueError('invalid trusted commit')
    if verified_report_hash is not None:
        if (not isinstance(verified_report_hash, str) or not re.fullmatch('[0-9a-f]{64}', verified_report_hash) or
                not isinstance(report_bytes, bytes) or hashlib.sha256(report_bytes).hexdigest() != verified_report_hash):
            raise ValueError('report differs from verified signed report')
        try:
            decoded = json.loads(report_bytes, object_pairs_hook=unique_object)
        except (UnicodeError, json.JSONDecodeError) as exc:
            raise ValueError('invalid signed report') from exc
        if scalar_json(decoded) != report_json:
            raise ValueError('report differs from verified signed report bytes')
    version = report.get('version')
    if verified_report_hash is not None and type(version) is int and version in (1, 2, 3):
        return
    if type(version) is not int or version != 4:
        raise ValueError('new release requires report version 4')
    hashes = report.get('fixture_hashes')
    digest = hashes.get('Warnings') if isinstance(hashes, dict) else None
    if not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest):
        raise ValueError('missing warning fixture hash')
    if verified_report_hash is not None:
        data = FixtureHistory(root, WARNING_PATH).fixture(digest)
    else:
        data = git(root, 'show', f'{commit}:{WARNING_PATH}')
        if hashlib.sha256(data).hexdigest() != digest:
            raise ValueError('warning fixture differs from trusted main')
    validate_warnings(report, warning_cases(data, country), country)
