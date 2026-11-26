from repositories_test_support import enable_dual_policy as setUpModule

import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import corpus_upload

from corpus_fixture_history import FixtureHistory, git as fixture_git
from corpus_test_support import git, write_release_fixture
from corpus_warning import WARNING_PATH, warning_cases
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

    def test_new_release_pins_fixture_at_main_and_requires_version_five(self):
        current = json.dumps([CASE]).encode()
        old_hash = hashlib.sha256(b'[]').hexdigest()
        report = dict(version=5, fixture_hashes={'Warnings': old_hash}, warning_cases=[],
                      not_run={'warning_cases': 'not_run'})
        validate = getattr(corpus_warning, 'validate_authorized', lambda *args, **kwargs: None)
        with patch('corpus_warning.git', create=True, return_value=current):
            with self.assertRaises(ValueError):
                validate(report, 'vn', commit='refs/heads/main')
            report['version'] = 3
            with self.assertRaises(ValueError):
                validate(report, 'vn', commit='refs/heads/main')

    def test_upload_rejects_stale_warning_fixture_before_upload(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            directory, repository = root / 'release', root / 'repository'
            directory.mkdir()
            _, old = write_release_fixture(directory, repository)
            (repository / WARNING_PATH).write_text(json.dumps([CASE]))
            git(repository, 'add', WARNING_PATH)
            git(repository, 'commit', '--quiet', '-m', 'add warning case')
            with patch.dict('os.environ', {'OCI_S3_ENDPOINT': 'https://invalid.example'}), \
                    patch('corpus_upload.require_listed'), \
                    patch('corpus_manifest.FixtureHistory', side_effect=lambda: FixtureHistory(repository)), \
                    patch('corpus_warning.git', side_effect=lambda root, *args: fixture_git(repository, *args)), \
                    patch('corpus_upload.require_settled') as settled:
                with self.assertRaisesRegex(ValueError, 'warning fixture differs'):
                    corpus_upload.upload('vn', old['rel'], directory, directory / 'report.json', 'repo')
                settled.assert_not_called()

    def test_control_characters_are_rejected_in_fixture_and_carrier_handles(self):
        for control in ('\x00', '\x7f', '\x85', '\x9f'):
            for field in ('target', 'acting'):
                for location in ('key', 'anchor'):
                    bad = copy.deepcopy(CASE)
                    bad[field] = bad[field].replace('#', control + '#', 1) if location == 'key' else bad[field] + control
                    with self.subTest(control=repr(control), field=field, location=location), self.assertRaises(ValueError):
                        warning_cases(json.dumps([bad]).encode(), 'vn')
            for carrier in ('vn:vbpl:1' + control + '#dieu-5', 'vn:vbpl:1#dieu-5' + control):
                match = dict(carrier=carrier, dir='in', kind='repeals', key='vn:vbpl:2',
                             in_release=True, source=CASE['acting'], target=CASE['target'], exact=True)
                with self.subTest(control=repr(control), carrier=repr(carrier)), self.assertRaises(ValueError):
                    corpus_warning.validate_match(match, CASE, 'vn')

    def test_fixture_rejects_lone_surrogates_without_echoing_values(self):
        for field in ('question', 'target', 'acting'):
            case = dict(CASE)
            case[field] += 'synthetic-sensitive\ud800'
            data = json.dumps([case]).encode()
            with self.subTest(field=field):
                with self.assertRaises(ValueError) as caught:
                    warning_cases(data, 'vn')
                self.assertNotIsInstance(caught.exception, UnicodeEncodeError)
                self.assertNotIn('synthetic-sensitive', str(caught.exception))
                self.assertNotIn('d800', str(caught.exception))
        case = dict(CASE, **{'\udfff': 'synthetic-sensitive'})
        with self.assertRaises(ValueError) as caught:
            warning_cases(json.dumps([case]).encode(), 'vn')
        self.assertNotIsInstance(caught.exception, UnicodeEncodeError)
        self.assertNotIn('synthetic-sensitive', str(caught.exception))
        self.assertNotIn('dfff', str(caught.exception))

    def test_fixture_accepts_paired_supplementary_character(self):
        case = dict(CASE, question='Question \U0001f680', target='vn:vbpl:1#dieu-\U0001f680')
        data = json.dumps([case]).encode()
        self.assertIn(b'\\ud83d\\ude80', data)
        self.assertEqual(warning_cases(data, 'vn'), [case])

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
