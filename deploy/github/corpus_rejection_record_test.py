import copy
import unittest

from corpus_manifest import validate_report
from corpus_revision_support import CANDIDATE_REL, CURRENT_CASES, CURRENT_RANKS, PREVIOUS_REL, build_report, encode
from corpus_test_support import recipe_fixture

FIXTURE = encode(CURRENT_CASES)
MESSAGE = 'rejection record'


def version_two_pair():
    previous = build_report(2, PREVIOUS_REL, FIXTURE, CURRENT_RANKS)
    candidate = build_report(2, CANDIDATE_REL, FIXTURE, CURRENT_RANKS)
    return previous, candidate


def check(candidate, previous=None):
    validate_report(candidate, 'vn', candidate['rel'], previous_report=previous,
                    release_row=recipe_fixture(candidate['retrieval_recipe'])[0])


class VersionOneRejectionGateTest(unittest.TestCase):
    def candidate(self):
        candidate = version_two_pair()[1]
        candidate.update(version=1, scorer='agent-eval/4')
        candidate.pop('retrieval_recipe')
        return candidate

    def validate(self, candidate):
        validate_report(candidate, 'vn', candidate['rel'])

    def test_passing_rejections_are_accepted(self):
        self.validate(self.candidate())

    def test_three_rejections_are_required(self):
        candidate = self.candidate()
        candidate['rejections'].pop()
        candidate['cohorts']['old_product']['case_count'] -= 1
        with self.assertRaisesRegex(ValueError, 'rejection'):
            self.validate(candidate)

    def test_failed_or_invalid_passed_is_refused(self):
        changes = [('false', lambda row: row.update(passed=False)),
                   ('missing', lambda row: row.pop('passed')),
                   ('integer', lambda row: row.update(passed=1)),
                   ('text', lambda row: row.update(passed='true')),
                   ('null', lambda row: row.update(passed=None))]
        for name, change in changes:
            with self.subTest(name=name):
                candidate = self.candidate()
                change(candidate['rejections'][1])
                with self.assertRaisesRegex(ValueError, 'rejection'):
                    self.validate(candidate)


class VersionTwoRejectionRecordTest(unittest.TestCase):
    def test_a_passed_diacritics_refusal_is_accepted(self):
        previous, candidate = version_two_pair()
        check(candidate)
        check(candidate, previous)

    def test_a_record_that_is_not_a_passed_diacritics_refusal_is_refused(self):
        changes = [('failed', lambda row: row.update(passed=False)), ('passed one', lambda row: row.update(passed=1)),
                   ('passed text', lambda row: row.update(passed='true')), ('passed null', lambda row: row.update(passed=None)),
                   ('unsupported', lambda row: row.update(reason='unsupported_query')),
                   ('empty reason', lambda row: row.update(reason='')), ('reason null', lambda row: row.update(reason=None)),
                   ('extra field', lambda row: row.update(message='x')),
                   ('no reason', lambda row: row.pop('reason')), ('no passed', lambda row: row.pop('passed'))]
        for name, change in changes:
            with self.subTest(name=name):
                previous, candidate = version_two_pair()
                change(candidate['rejections'][1])
                with self.assertRaisesRegex(ValueError, MESSAGE):
                    check(candidate, previous)
                with self.assertRaisesRegex(ValueError, MESSAGE):
                    check(candidate)

    def test_the_rejections_are_three_distinct_nonempty_ids(self):
        changes = [('popped', lambda rows: rows.pop(), 2), ('extra', lambda rows: rows.append(dict(rows[0] | dict(case_id='x'))), 4),
                   ('duplicate', lambda rows: rows[2].update(case_id='r0'), 2),
                   ('empty id', lambda rows: rows[2].update(case_id=''), None),
                   ('id not text', lambda rows: rows[2].update(case_id=7), None),
                   ('row not an object', lambda rows: rows.__setitem__(2, 'r2'), None)]
        for name, change, count in changes:
            with self.subTest(name=name):
                candidate = version_two_pair()[1]
                change(candidate['rejections'])
                if count is not None:
                    candidate['cohorts']['old_product']['case_count'] = count + len(
                        {row['case_id'] for row in candidate['cases'] if row['cohort'] == 'old_product'})
                with self.assertRaises(ValueError):
                    check(candidate)

    def test_rejections_that_are_not_a_list_are_refused(self):
        for value in (None, {}, 'r0'):
            with self.subTest(value=value):
                candidate = version_two_pair()[1]
                candidate['rejections'] = value
                with self.assertRaises(ValueError):
                    check(candidate)


if __name__ == '__main__':
    unittest.main()
