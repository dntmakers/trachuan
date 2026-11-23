from repositories_test_support import enable_dual_policy as setUpModule

import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import corpus_upload
from corpus_test_support import digest, save_fixture, write_fixture


class CorpusLLMGateUploadTest(unittest.TestCase):
    def test_upload_rejects_failing_classification_gate_before_clients(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            row, report = write_fixture(directory)
            fixture = b'[]\n'
            report['fixture_hashes']['Warnings'] = digest(fixture)
            report.update(version=4, scorer='agent-eval/6', warning_cases=[], not_run={'warning_cases': 'not_run'},
                          quality_pass=False, classification_gate=dict(
                status='fail', classification_set_sha256='a' * 64, policy_sha256='b' * 64,
                taxonomy_version='c' * 64, llm_rows=2, slices=[dict(
                    slice='seeded', status='fail', reasons=['recall below floor'])]))
            save_fixture(directory, row, report)
            with patch.dict(os.environ, {'OCI_S3_ENDPOINT': 'https://invalid.example'}), \
                    patch('corpus_warning.git', return_value=fixture), \
                    patch('corpus_upload.subprocess.run') as client, \
                    self.assertRaisesRegex(ValueError, 'report must have completed quality checks'):
                corpus_upload.upload('vn', report['rel'], directory, directory / 'report.json', 'dannyota/trachuan')
            client.assert_not_called()
