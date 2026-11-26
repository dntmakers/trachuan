import copy
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re

from .deploy_corpus_heading import validate_headings
from .deploy_corpus_sql import RELEASE
from .deploy_io import canonical, decode, read_json_bytes
from .deploy_verify import accepted, flags, match, positive


def release_name(value):
    match(RELEASE, value, 'corpus release')
    datetime.strptime(value[:8], '%Y%m%d')


def release_evidence(value):
    fields = ('findings_sha256', 'identity_sha256', 'changes_sha256')
    if not isinstance(value, dict) or set(value) != set(fields):
        raise ValueError('invalid corpus evidence fields')
    for field in fields:
        match('[0-9a-f]{64}', value[field], 'evidence ' + field)


def manifest(data, rules):
    obj = decode(data, 'corpus manifest')
    keys = {'kind', 'version', 'repository', 'commit', 'country', 'rel', 'database',
            'sequence', 'previous', 'dump', 'report', 'build_artifact_sha256',
            'schema_version', 'extensions', 'recipes'}
    optional = {'heading_review', 'heading_evidence', 'evidence', 'changes_projection'}
    if not isinstance(obj, dict) or not keys <= set(obj) or set(obj) - keys - optional:
        raise ValueError('invalid corpus manifest fields')
    validate_headings(obj)
    if 'evidence' in obj:
        release_evidence(obj['evidence'])
    accepted(obj['repository'], rules, 'corpus repository')
    for key, value in dict(kind='trachuan.corpus-release', version=1, country='vn').items():
        if type(obj[key]) is not type(value) or obj[key] != value:
            raise ValueError('invalid corpus ' + key)
    match('[0-9a-f]{40}', obj['commit'], 'corpus commit')
    release_name(obj['rel'])
    release_name(obj['previous'])
    if obj['database'] != 'corpus_' + obj['country'] + '_' + obj['rel']:
        raise ValueError('corpus database mismatch')
    for field in ('sequence', 'schema_version'):
        positive(obj[field], 'corpus ' + field)
    if obj['schema_version'] not in (4, 5, 6, 7, 8, 9):
        raise ValueError('unsupported corpus schema version')
    if (obj['schema_version'] >= 8) != ('heading_evidence' in obj):
        raise ValueError('heading evidence does not match schema version')
    if (obj['schema_version'] == 9) != ('changes_projection' in obj):
        raise ValueError('changes projection does not match schema version')
    if 'changes_projection' in obj:
        value = obj['changes_projection']
        if (not isinstance(value, dict) or set(value) != {'version', 'sha256'}
                or type(value['version']) is not int or value['version'] != 1):
            raise ValueError('invalid changes projection')
        match('[0-9a-f]{64}', value['sha256'], 'changes projection hash')
    dump = obj['dump']
    if not isinstance(dump, dict) or set(dump) != {'object', 'sha256', 'size'}:
        raise ValueError('invalid corpus dump fields')
    if dump['object'] != obj['country'] + '/' + obj['rel'] + '/corpus.dump':
        raise ValueError('corpus dump object mismatch')
    match('[0-9a-f]{64}', dump['sha256'], 'dump hash')
    positive(dump['size'], 'dump size')
    report = obj['report']
    if not isinstance(report, dict) or set(report) != {'sha256', 'quality_pass'} or report['quality_pass'] is not True:
        raise ValueError('corpus report quality_pass required')
    match('[0-9a-f]{64}', report['sha256'], 'report hash')
    match('[0-9a-f]{64}', obj['build_artifact_sha256'], 'build artifact hash')
    if not isinstance(obj['extensions'], dict) or set(obj['extensions']) != set(rules['extensions']):
        raise ValueError('unlisted or missing corpus extension')
    for version in obj['extensions'].values():
        match(r'[0-9]+(?:\.[0-9]+)+', version, 'extension version')
    recipes = obj['recipes']
    if not isinstance(recipes, dict) or set(recipes) != {'dense', 'chunk', 'lexical', 'retrieval'}:
        raise ValueError('invalid corpus recipes')
    if any(not isinstance(recipe, dict) or not recipe for recipe in recipes.values()):
        raise ValueError('invalid corpus recipe')
    match('[0-9a-f]{64}', recipes['dense'].get('query_hash'), 'dense query hash')
    if canonical(obj) != data:
        raise ValueError('corpus manifest is not canonical JSON')
    return obj


def verify(directory, rules, runner):
    path = Path(directory) / 'manifest.json'
    data = read_json_bytes(path, 'corpus manifest')
    obj = manifest(data, rules)
    runner(['/usr/local/libexec/trachuan/bin/cosign', 'verify-blob', '--bundle',
            str(Path(directory) / 'manifest.sigstore.json'),
            *flags(rules, 'corpus', obj['repository'], 'refs/heads/main', obj['commit']), str(path)])
    return hashlib.sha256(data).hexdigest(), obj


def uncalibrated(recipe):
    if not isinstance(recipe, dict) or not isinstance(recipe.get('abstention', {}), dict):
        raise ValueError('invalid retrieval recipe')
    obj = copy.deepcopy(recipe)
    obj.get('abstention', {}).pop('threshold', None)
    obj.get('abstention', {}).pop('calibration_sha256', None)
    return obj


def compatible(obj, report):
    if (not isinstance(report, dict) or not isinstance(report.get('schema_versions'), list)
            or not isinstance(report.get('dense_query_hash'), str)
            or not isinstance(report.get('running_query'), dict)
            or not isinstance(report['running_query'].get('recipe_hash'), str)
            or not isinstance(report.get('lexical_recipe'), dict)
            or not isinstance(report.get('retrieval_recipes'), list)):
        raise ValueError('invalid corpus release-check output')
    retrieval = [uncalibrated(recipe) for recipe in report['retrieval_recipes']]
    if obj['schema_version'] not in report['schema_versions']:
        raise ValueError('unsupported corpus schema version')
    recipes = obj['recipes']
    query = recipes['dense']['query_hash']
    return (query == report['dense_query_hash'] == report['running_query']['recipe_hash']
            and recipes['dense']['query'] == report['running_query'].get('recipe')
            and recipes['lexical'] == report['lexical_recipe']
            and uncalibrated(recipes['retrieval']) in retrieval)


def decode_report(data):
    def unique(pairs):
        obj = {}
        for key, value in pairs:
            if key in obj:
                raise ValueError('duplicate field')
            obj[key] = value
        return obj
    try:
        return json.loads(data, object_pairs_hook=unique)
    except (ValueError, RecursionError) as error:
        raise ValueError('invalid release check JSON') from error


def projection(obj, report):
    if not isinstance(report, dict):
        raise ValueError('invalid changes projection report')
    if obj['schema_version'] < 9:
        if 'changes_projection' in report:
            raise ValueError('invalid changes projection report')
        return None
    value = report.get('changes_projection')
    fields = {'version', 'sha256', 'release', 'previous', 'baseline', 'counts'}
    kinds = {'document_added', 'document_removed', 'document_validity', 'provision_validity', 'provision_text', 'document_key'}
    if (not isinstance(value, dict) or set(value) != fields or type(value['version']) is not int
            or value['version'] != obj['changes_projection']['version']
            or value['sha256'] != obj['changes_projection']['sha256'] or value['release'] != obj['rel']
            or value['previous'] != obj['previous'] or type(value['baseline']) is not bool
            or not isinstance(value['counts'], dict) or set(value['counts']) != kinds
            or any(type(count) is not int or count < 0 for count in value['counts'].values())
            or (value['baseline'] and any(value['counts'].values()))):
        raise ValueError('invalid changes projection report')
    return value['counts']


def toc(text, extensions, omit=None):
    kept, seen = [], set()
    allowed = ('SCHEMA ', 'TABLE ', 'TABLE DATA ', 'SEQUENCE ', 'SEQUENCE SET ',
               'SEQUENCE OWNED BY ', 'DEFAULT ', 'CONSTRAINT ', 'FK CONSTRAINT ',
               'INDEX ', 'INDEX ATTACH ', 'STATISTICS ', 'COMMENT ', 'ACL ',
               'DEFAULT ACL ', 'ENCODING ', 'STDSTRINGS ', 'SEARCHPATH ')
    for line in text.splitlines():
        if not line or line.startswith(';'):
            continue
        found = re.fullmatch(r'[0-9]+; [0-9]+ [0-9]+ (.+)', line)
        if not found:
            raise ValueError('invalid dump TOC')
        entry = found[1]
        if entry.startswith('EXTENSION '):
            parts = entry.split()
            if len(parts) < 3 or parts[1] != '-' or parts[2] not in extensions or parts[2] in seen:
                raise ValueError('unlisted or duplicate dump extension')
            seen.add(parts[2])
        elif entry.startswith('COMMENT - EXTENSION '):
            parts = entry.split()
            if len(parts) < 4 or parts[3] not in extensions:
                raise ValueError('unlisted extension comment')
        elif omit is not None and omit(entry):
            continue
        elif not entry.startswith(allowed) or ' EXTENSION ' in entry:
            raise ValueError('unsafe dump TOC object')
        else:
            kept.append(line)
    if seen != set(extensions):
        raise ValueError('missing dump extensions')
    return '\n'.join(kept) + '\n'
