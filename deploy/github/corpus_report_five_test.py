import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from corpus_warning import validate_authorized
from corpus_test_support import digest

from corpus_comparison import versions
from corpus_manifest import validate_report
from corpus_revision_support import (CANDIDATE_REL, CURRENT_CASES, CURRENT_RANKS, PREVIOUS_REL,
                                     build_report, encode, history_for, link_period, set_records)
from corpus_test_support import recipe_fixture


class ReportFiveTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.fixture = encode(CURRENT_CASES)
        self.history = history_for(Path(self.temp.name) / 'repository', self.fixture)
        self.previous = build_report(4, PREVIOUS_REL, self.fixture, CURRENT_RANKS)
        self.current = build_report(4, CANDIDATE_REL, self.fixture, CURRENT_RANKS)
        self.current['version'] = 5
        self.current['rejections'] = []
        self.current['cohorts']['old_product']['case_count'] -= 3
        self.current['cohorts']['old_product']['rejection_count'] = 0
        self.current['retrieval_recipe'].update(version=4, query_gate='vn-restore/1')
        self.row, hashes = recipe_fixture(self.current['retrieval_recipe'])
        self.current['recipes'] = hashes
        for cohort in ('old_product', 'development'):
            set_records(self.current, cohort, 'mrr', tolerance=0)
        link_period(self.current, self.previous)

    def validate(self):
        validate_report(self.current, 'vn', CANDIDATE_REL, previous_report=self.previous,
                        release_row=self.row, history=self.history)

    def test_first_release_requires_five_and_resigning_keeps_four(self):
        data = b'[]\n'
        self.current['fixture_hashes']['Warnings'] = digest(data)
        with patch('corpus_warning.git', return_value=data):
            validate_authorized(self.current, 'vn', commit='refs/heads/main')
            with self.assertRaises(ValueError):
                validate_authorized(self.previous, 'vn', commit='refs/heads/main')
        import json
        previous_bytes = json.dumps(self.previous).encode()
        with patch('corpus_warning.FixtureHistory') as history:
            history.return_value.fixture.return_value = data
            validate_authorized(self.previous, 'vn', verified_report_hash=digest(previous_bytes), report_bytes=previous_bytes)

    def test_version_five_compares_with_version_four(self):
        self.validate()

    def test_recipe_four_needs_restoration_gate(self):
        report = copy.deepcopy(self.current)
        report['retrieval_recipe']['query_gate'] = 'vn-marks/1'
        with self.assertRaises(ValueError):
            versions(report)

    def test_wrong_recipe_version_is_refused(self):
        for report_version, recipe_version in ((5, 3), (4, 4), (4, 1), (4, 99)):
            with self.subTest(report_version=report_version, recipe_version=recipe_version):
                report = copy.deepcopy(self.current)
                report['version'] = report_version
                report['retrieval_recipe']['version'] = recipe_version
                row, report['recipes'] = recipe_fixture(report['retrieval_recipe'])
                with self.assertRaises(ValueError):
                    versions(report)
                with self.assertRaises(ValueError):
                    validate_report(report, 'vn', CANDIDATE_REL, release_row=row, history=self.history)

    def test_cross_version_precision_uses_each_report_denominator(self):
        for metric in self.previous['cohorts']['old_product']['metrics']:
            if metric['name'] == 'current_law_precision':
                metric.update(numerator=20., denominator=20, value=1.)
        for floors in (self.previous['floors'], self.previous['cohorts']['old_product']['floors']):
            for floor in floors:
                floor.update(numerator=20., denominator=20, value=1.)
        self.validate()

    def test_cross_version_rejects_invalid_previous_and_scored_membership(self):
        changes = [lambda: self.previous['rejections'][0].update(reason='other'),
                   lambda: self.previous['rejections'][0].update(extra=True),
                   lambda: self.previous['fixture_hashes'].update(Legacy='f' * 64),
                   lambda: self.current['cases'][0].update(case_id='other'),
                   lambda: self.previous['cohorts']['old_product'].update(expected_citations=99),
                   lambda: next(m for m in self.previous['cohorts']['old_product']['metrics']
                                if m['name'] == 'recall' and m['limit'] == 8).update(denominator=99)]
        for change in changes:
            with self.subTest(change=change):
                previous, current = copy.deepcopy(self.previous), copy.deepcopy(self.current)
                change()
                with self.assertRaises(ValueError):
                    self.validate()
                self.previous, self.current = previous, current

    def test_rejections_and_counts_are_checked(self):
        for mutate in (lambda r: r['rejections'].append(dict(case_id='r0', reason='diacritics_required', passed=True)),
                       lambda r: r['cohorts']['old_product'].update(rejection_count=1),
                       lambda r: r['cohorts']['old_product'].update(case_count=5),
                       lambda r: r['cohorts']['legacy'].update(rejection_count=1)):
            with self.subTest(mutate=mutate):
                original = copy.deepcopy(self.current)
                mutate(self.current)
                with self.assertRaises(ValueError):
                    self.validate()
                self.current = original


if __name__ == '__main__':
    unittest.main()
