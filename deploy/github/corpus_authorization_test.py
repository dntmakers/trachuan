from repositories_test_support import enable_dual_policy as setUpModule

import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import corpus_manifest
import corpus_upload
from corpus_fixture_history import FixtureHistory, git as fixture_git
from corpus_test_support import git, write_fixture, write_release_fixture
from corpus_warning import WARNING_PATH, validate_authorized


class AuthorizationBoundaryTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.directory = self.root / 'release'
        self.directory.mkdir()
        self.repository = self.root / 'repository'
        self.row, self.report = write_release_fixture(self.directory, self.repository)
        self.report_path = self.directory / 'report.json'
        self.previous = json.loads((self.directory / 'previous-report.json').read_bytes())
        self.old_hash = hashlib.sha256(self.report_path.read_bytes()).hexdigest()
        case = dict(entry_id='effect-example', question='Question?', target='vn:vbpl:1#dieu-5',
                    acting='vn:vbpl:2#dieu-2', relation_kind='repeals')
        (self.repository / WARNING_PATH).write_text(json.dumps([case]))
        git(self.repository, 'add', WARNING_PATH)
        git(self.repository, 'commit', '--quiet', '-m', 'add warning case')
        self.commit = git(self.repository, 'rev-parse', 'HEAD').decode().strip()
        history = patch('corpus_manifest.FixtureHistory', side_effect=lambda: FixtureHistory(self.repository))
        history.start()
        self.addCleanup(history.stop)
        warning_history = patch('corpus_warning.FixtureHistory', side_effect=lambda root, path: FixtureHistory(self.repository, path))
        warning_history.start()
        self.addCleanup(warning_history.stop)
        warning_git = patch('corpus_warning.git', side_effect=lambda root, *args: fixture_git(self.repository, *args))
        warning_git.start()
        self.addCleanup(warning_git.stop)

    def test_signed_report_object_must_match_json_types(self):
        data = self.report_path.read_bytes()
        altered = self.report | {'quality_pass': 1}
        with patch('corpus_warning.FixtureHistory', side_effect=AssertionError('history read')), \
                self.assertRaisesRegex(ValueError, 'report differs'):
            validate_authorized(altered, 'vn', verified_report_hash=self.old_hash, report_bytes=data)

    def test_authorization_requires_exactly_one_trust_source(self):
        data = self.report_path.read_bytes()
        for kwargs in ({}, {'commit': 'refs/heads/main', 'verified_report_hash': self.old_hash, 'report_bytes': data},
                       {'verified_report_hash': self.old_hash},
                       {'verified_report_hash': '0' * 64, 'report_bytes': data}):
            with self.subTest(kwargs=list(kwargs)), \
                    patch('corpus_warning.FixtureHistory', side_effect=AssertionError('history read')), \
                    patch('corpus_warning.git', side_effect=AssertionError('commit read')), \
                    self.assertRaises(ValueError):
                validate_authorized(self.report, 'vn', **kwargs)

    def test_validate_cli_rejects_old_fixture_at_local_main(self):
        args = ['corpus_manifest.py', 'validate', 'vn', self.report['rel'], str(self.directory), str(self.report_path)]
        with patch('sys.argv', args), self.assertRaisesRegex(ValueError, 'warning fixture differs'):
            corpus_manifest.main()

    def test_upload_rejects_old_fixture_before_network(self):
        with patch.dict(os.environ, {'OCI_S3_ENDPOINT': 'https://invalid.example'}), \
                patch('corpus_upload.require_settled') as settled, \
                self.assertRaisesRegex(ValueError, 'warning fixture differs'):
            corpus_upload.upload('vn', self.report['rel'], self.directory, self.report_path, 'dannyota/trachuan')
        settled.assert_not_called()

    def test_upload_dry_run_rejects_old_fixture_at_local_main(self):
        args = ['corpus_upload.py', '--dry-run', '--repository', 'dannyota/trachuan', 'vn', self.report['rel'],
                str(self.directory), str(self.report_path)]
        with patch('sys.argv', args), self.assertRaisesRegex(ValueError, 'warning fixture differs'):
            corpus_upload.main()

    def test_unsigned_manifest_creation_rejects_old_fixture_at_workflow_commit(self):
        args = ['corpus_manifest.py', 'create', 'vn', self.report['rel'], str(self.directory), str(self.report_path)]
        env = {'GITHUB_REF': 'refs/heads/main', 'GITHUB_SHA': self.commit, 'GITHUB_RUN_NUMBER': '1'}
        with patch.dict(os.environ, env), patch('sys.argv', args), patch('corpus_manifest.workflow', return_value='dannyota/trachuan'), \
                patch('corpus_previous.check_remote', return_value=self.previous), \
                patch('corpus_registry.existing_release', return_value=None), \
                self.assertRaisesRegex(ValueError, 'warning fixture differs'):
            corpus_manifest.main()

    def test_signed_version_two_can_be_rebuilt_without_warning_history(self):
        _, report = write_fixture(self.directory)
        signed_hash = hashlib.sha256(self.report_path.read_bytes()).hexdigest()
        previous = json.loads((self.directory / 'previous-report.json').read_bytes())
        args = ['corpus_manifest.py', 'create', 'vn', report['rel'], str(self.directory), str(self.report_path)]
        env = {'GITHUB_REF': 'refs/heads/main', 'GITHUB_SHA': self.commit, 'GITHUB_RUN_NUMBER': '1'}
        signed = dict(country='vn', rel=report['rel'], report={'sha256': signed_hash})
        with patch.dict(os.environ, env), patch('sys.argv', args), \
                patch('corpus_manifest.workflow', return_value='dannyota/trachuan'), \
                patch('corpus_previous.check_remote', return_value=previous), \
                patch('corpus_registry.existing_release', return_value=signed), \
                patch('corpus_warning.FixtureHistory', side_effect=AssertionError('warning history read')):
            corpus_manifest.main()
            manifest = json.loads((self.directory / 'manifest.json').read_bytes())
            self.assertEqual(manifest['report']['sha256'], signed_hash)

    def test_signed_manifest_creation_accepts_only_exact_verified_report(self):
        args = ['corpus_manifest.py', 'create', 'vn', self.report['rel'], str(self.directory), str(self.report_path)]
        env = {'GITHUB_REF': 'refs/heads/main', 'GITHUB_SHA': self.commit, 'GITHUB_RUN_NUMBER': '1'}
        signed = dict(country='vn', rel=self.report['rel'], report={'sha256': self.old_hash})
        with patch.dict(os.environ, env), patch('sys.argv', args), patch('corpus_manifest.workflow', return_value='dannyota/trachuan'), \
                patch('corpus_previous.check_remote', return_value=self.previous), \
                patch('corpus_registry.existing_release', return_value=signed):
            corpus_manifest.main()
            manifest = json.loads((self.directory / 'manifest.json').read_bytes())
            self.assertEqual(manifest['report']['sha256'], self.old_hash)
            signed['report']['sha256'] = '0' * 64
            with patch('corpus_warning.FixtureHistory', side_effect=AssertionError('warning history read')), \
                    patch('corpus_manifest.FixtureHistory', side_effect=AssertionError('development history read')), \
                    self.assertRaisesRegex(ValueError, 'signed.*report|report.*signed'):
                corpus_manifest.main()
