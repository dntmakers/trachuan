from repositories_test_support import enable_dual_policy as setUpModule

from pathlib import Path
import json
import tempfile
import unittest

import corpus_test_support
from corpus_fixture_history import FixtureHistory
from corpus_manifest import validate_report
from corpus_warning import validate_authorized


class ReleaseFixtureTest(unittest.TestCase):
    def test_release_fixture_uses_version_four_and_synthetic_main(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'release').mkdir()
            build = getattr(corpus_test_support, 'write_release_fixture', lambda *args: None)
            result = build(root / 'release', root / 'repository')
            self.assertIsNotNone(result)
            row, report = result
            self.assertEqual(report['version'], 4)
            self.assertEqual(report['warning_cases'], [])
            self.assertEqual(report['not_run']['warning_cases'], 'not_run')
            previous = json.loads((root / 'release' / 'previous-report.json').read_bytes())
            validate_report(report, 'vn', report['rel'], previous_report=previous, release_row=row,
                            history=FixtureHistory(root / 'repository'))
            validate_authorized(report, 'vn', commit='refs/heads/main', root=root / 'repository')
