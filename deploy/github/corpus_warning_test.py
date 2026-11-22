import copy
import hashlib
import json
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import corpus_upload

from corpus_warning import warning_cases
import corpus_warning


CASE = dict(entry_id='effect-example', question='Question?', target='vn:vbpl:1#dieu-5',
            acting='vn:vbpl:2#dieu-30/khoan-2', relation_kind='repeals')


class WarningFixtureTest(unittest.TestCase):
    def test_empty_and_valid(self):
        self.assertEqual(warning_cases(b'[]', 'vn'), [])
        self.assertEqual(warning_cases(json.dumps([CASE]).encode(), 'vn'), [CASE])

    def test_warning_gate(self):
        match = dict(carrier=CASE['target'], dir='in', kind='repeals', key='vn:vbpl:2',
                     in_release=True, source=CASE['acting'], target=CASE['target'], exact=True)
        search = [dict(mode=mode, limit=limit, detail=detail, match=copy.deepcopy(match))
                  for mode in ('exact', 'ann') for limit in (8, 20) for detail in ('compact', 'full')]
        report = dict(warning_cases=[dict(entry_id=CASE['entry_id'], outcome='applied', search=search, read=match)], not_run={})
        validate = getattr(corpus_warning, 'validate_warnings', lambda *args: False)
        self.assertIs(validate(report, [CASE], 'vn'), True)
        sibling = copy.deepcopy(report)
        sibling['warning_cases'][0]['search'][0]['match']['carrier'] = 'vn:vbpl:1#dieu-5~2'
        self.assertIs(validate(sibling, [CASE], 'vn'), True)
        for field in ('kind', 'key', 'target', 'carrier'):
            bad = copy.deepcopy(report)
            bad['warning_cases'][0]['read'][field] = 'vn:vbpl:999#dieu-1'
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate(bad, [CASE], 'vn')
        for change in ('stale', 'missing', 'order', 'duplicate'):
            bad = copy.deepcopy(report)
            row = bad['warning_cases'][0]
            if change == 'stale':
                row.update(outcome='stale', reason='changed', search=[], read=None)
            elif change == 'missing':
                del bad['warning_cases']
            elif change == 'order':
                row['search'][0], row['search'][1] = row['search'][1], row['search'][0]
            else:
                bad['warning_cases'].append(copy.deepcopy(row))
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate(bad, [CASE], 'vn')

    def test_new_release_pins_fixture_at_main_and_requires_version_four(self):
        current = json.dumps([CASE]).encode()
        old_hash = hashlib.sha256(b'[]').hexdigest()
        report = dict(version=4, fixture_hashes={'Warnings': old_hash}, warning_cases=[],
                      not_run={'warning_cases': 'not_run'})
        validate = getattr(corpus_warning, 'validate_authorized', lambda *args, **kwargs: None)
        with patch('corpus_warning.git', create=True, return_value=current):
            with self.assertRaises(ValueError):
                validate(report, 'vn')
            report['version'] = 3
            with self.assertRaises(ValueError):
                validate(report, 'vn')

    def test_upload_rejects_stale_warning_fixture_before_upload(self):
        old = dict(version=4, fixture_hashes={'Warnings': hashlib.sha256(b'[]').hexdigest()},
                   warning_cases=[], not_run={'warning_cases': 'not_run'})
        inputs = SimpleNamespace(row={}, report=old, sidecars=None)
        with patch.dict('os.environ', {'OCI_S3_ENDPOINT': 'https://invalid.example'}), \
                patch('corpus_upload.require_listed'), patch('corpus_upload.validated_inputs', return_value=inputs), \
                patch('corpus_warning.git', return_value=json.dumps([CASE]).encode()), \
                patch('corpus_upload.require_settled') as settled:
            with self.assertRaisesRegex(ValueError, 'warning fixture differs'):
                corpus_upload.upload('vn', '20990102_1', None, None, 'repo')
            settled.assert_not_called()

    def test_invalid_fixture(self):
        variants = [b'{}', b'[' + b' ' * 65536 + b']', b'[{"entry_id":"x","entry_id":"y"}]',
                    json.dumps([CASE, CASE]).encode(), json.dumps([CASE | {'extra': 1}]).encode(),
                    json.dumps([CASE | {'question': ''}]).encode(),
                    json.dumps([CASE | {'target': 'vn:vbpl:1#dieu-5~2'}]).encode(),
                    json.dumps([CASE | {'acting': 'vn:vbpl:1#dieu-2'}]).encode(),
                    json.dumps([CASE | {'relation_kind': 'cites'}]).encode()]
        for data in variants:
            with self.subTest(data=data[:100]), self.assertRaises(ValueError):
                warning_cases(data, 'vn')
