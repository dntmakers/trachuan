from corpus_period import PERIOD


def no_research_override(value):
    if isinstance(value, dict):
        for key, child in value.items():
            if key in {'cap_override', 'article_cap_override', 'historical_policy', 'non_current_tail'} and child is not None:
                raise ValueError('research override is not releasable')
            if key == 'not_yet_eligible' and child is not False:
                raise ValueError('research override is not releasable')
            no_research_override(child)
    elif isinstance(value, list):
        for child in value:
            no_research_override(child)
    elif value == 'research_override':
        raise ValueError('research override is not releasable')


def non_regression_contract():
    comparisons = [dict(mode=mode, arms='hybrid', limit=limit, name=name, gating=gating)
                   for mode in ('exact', 'ann')
                   for name, limit, gating in (('recall', 20, True), ('mrr', 8, True),
                                               ('abstention_balanced_accuracy', 8, False))]
    return {'legacy': [], 'old_product': comparisons, 'development': comparisons,
            'development_period': [entry.copy() for entry in PERIOD]}


def check_comparisons(records, expected):
    if not isinstance(records, list):
        raise ValueError('invalid non_regression')
    seen = {}
    for record in records:
        if not isinstance(record, dict) or type(record.get('gating')) is not bool:
            raise ValueError('invalid non_regression gating')
        if any(not isinstance(record.get(field), str) for field in ('mode', 'arms', 'name')) or type(record.get('limit')) is not int:
            raise ValueError('invalid non_regression identity')
        key = tuple(record[field] for field in ('mode', 'arms', 'limit', 'name'))
        if key in seen:
            raise ValueError('duplicate non_regression entry')
        seen[key] = record
        if record['gating'] and record.get('status') != 'pass':
            raise ValueError('gating non_regression did not pass')
    for entry in expected:
        if entry['gating']:
            key = tuple(entry[field] for field in ('mode', 'arms', 'limit', 'name'))
            if key not in seen or seen[key]['gating'] is not True or seen[key].get('status') != 'pass':
                raise ValueError('required non_regression gate missing or not passing')
