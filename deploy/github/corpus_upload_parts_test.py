import hashlib
import io
import contextlib
import importlib
import json
from pathlib import Path
import tempfile
import unittest
import subprocess
from unittest.mock import patch


class CorpusUploadPartsTest(unittest.TestCase):
    def setUp(self):
        self.module = importlib.import_module('corpus_upload')
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / 'corpus.dump'
        self.path.write_bytes(b'abcdefg')

    def test_small_upload_uses_its_returned_etag_and_local_size(self):
        calls = []

        def client(args):
            calls.append(args)
            return {'ETag': '"own-upload"'}

        result = self.module.upload_file(self.path, 'vn/20990102_1/corpus.dump', client)
        self.assertEqual(result, {'ContentLength': 7, 'ETag': '"own-upload"'})
        self.assertIn('put-object', calls[0])
        self.assertIn('--content-length', calls[0])

    def expected(self):
        data = self.path.read_bytes()
        return hashlib.sha256(data).hexdigest(), len(data)

    def test_law_upload_without_expected_values_sends_the_file_itself(self):
        calls = []

        def client(args):
            calls.append(args)
            return {'ETag': '"own-upload"'}

        self.module.upload_file(self.path, 'vn/20990102_1/corpus.dump', client)
        self.assertEqual(calls[0][calls[0].index('--body') + 1], str(self.path))

    def test_expected_small_upload_sends_a_checked_private_copy(self):
        bodies = []

        def client(args):
            body = Path(args[args.index('--body') + 1])
            bodies.append((body, body.read_bytes()))
            return {'ETag': '"own-upload"'}

        result = self.module.upload_file(self.path, 'frameworks/20990102/report.json', client, expected=self.expected())
        self.assertEqual(result, {'ContentLength': 7, 'ETag': '"own-upload"'})
        self.assertNotEqual(bodies[0][0], self.path)
        self.assertEqual(bodies[0][1], b'abcdefg')
        self.assertFalse(bodies[0][0].exists())

    def test_changed_small_file_is_never_sent(self):
        digest, size = self.expected()
        for expected in (('0' * 64, size), (digest, size + 1)):
            calls = []
            with self.subTest(expected=expected), self.assertRaisesRegex(ValueError, 'report.json'):
                self.module.upload_file(self.path, 'frameworks/20990102/report.json', calls.append, expected=expected)
            self.assertEqual(calls, [])

    def multipart_client(self, calls):
        def client(args):
            calls.append(args)
            if 'create-multipart-upload' in args:
                return {'UploadId': 'synthetic-upload'}
            return {'ETag': '"part"'}
        return client

    def test_expected_multipart_upload_completes_when_the_stream_matches(self):
        calls = []
        result = self.module.upload_file(self.path, 'frameworks/20990102/frameworks.dump', self.multipart_client(calls),
                                         part_size=3, expected=self.expected())
        self.assertEqual(result['ContentLength'], 7)
        self.assertTrue(any('complete-multipart-upload' in call for call in calls))

    def test_changed_size_starts_no_multipart_upload(self):
        calls = []
        with self.assertRaisesRegex(ValueError, 'frameworks.dump'):
            self.module.upload_file(self.path, 'frameworks/20990102/frameworks.dump', self.multipart_client(calls),
                                    part_size=3, expected=(self.expected()[0], 8))
        self.assertEqual(calls, [])

    def test_changed_multipart_stream_aborts_before_completion(self):
        calls = []
        with self.assertRaisesRegex(ValueError, 'frameworks.dump'):
            self.module.upload_file(self.path, 'frameworks/20990102/frameworks.dump', self.multipart_client(calls),
                                    part_size=3, expected=('0' * 64, 7))
        self.assertIn('abort-multipart-upload', calls[-1])
        self.assertFalse(any('complete-multipart-upload' in call for call in calls))

    def test_bucket_defaults_to_law_and_can_be_named(self):
        calls = []

        def client(args):
            calls.append(args)
            return {'ETag': '"own-upload"'}

        self.module.upload_file(self.path, 'vn/20990102_1/corpus.dump', client)
        self.module.upload_file(self.path, 'frameworks/20990102/frameworks.dump', client, bucket='trachuan-frameworks-releases')
        self.assertEqual([args[args.index('--bucket') + 1] for args in calls],
                         ['trachuan-releases', 'trachuan-frameworks-releases'])

    def test_receipts_compare_size_and_etag_in_the_named_bucket(self):
        requests = []

        def client(args):
            requests.append(args)
            return {'ContentLength': 7, 'ETag': '"own"'}

        receipts = {'frameworks/20990102/report.json': {'ContentLength': 7, 'ETag': '"own"'}}
        self.module.verify_receipts(receipts, client, bucket='trachuan-frameworks-releases')
        self.assertEqual(requests, [['head-object', '--bucket', 'trachuan-frameworks-releases',
                                     '--key', 'frameworks/20990102/report.json']])
        for change in ({'ContentLength': 8}, {'ETag': '"other"'}):
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'report.json'):
                self.module.verify_receipts({key: value | change for key, value in receipts.items()}, client)

    def test_multipart_uses_completion_etag_and_orders_every_uploaded_part(self):
        calls, data = [], []

        def client(args):
            calls.append(args)
            if 'create-multipart-upload' in args:
                return {'UploadId': 'synthetic-upload'}
            if 'upload-part' in args:
                data.append(Path(args[args.index('--body') + 1]).read_bytes())
                return {'ETag': f'"part-{len(data)}"'}
            if 'complete-multipart-upload' in args:
                parts = json.loads(args[args.index('--multipart-upload') + 1])['Parts']
                self.assertEqual(parts, [dict(PartNumber=n, ETag=f'"part-{n}"') for n in range(1, 4)])
                return {'ETag': '"own-multipart"'}
            self.fail(str(args))

        result = self.module.upload_file(self.path, 'vn/20990102_1/corpus.dump', client, part_size=3)
        self.assertEqual(b''.join(data), self.path.read_bytes())
        self.assertEqual(result, {'ContentLength': 7, 'ETag': '"own-multipart"'})
        self.assertEqual(len(calls), 5)

    def test_requires_aws_cli_234_or_newer(self):
        for version, accepted in [('2.33.99', False), ('2.34.0', True), ('2.40.0', True),
                                  ('1.99.99', False), ('2.34', False), ('2.34.0junk', False)]:
            with self.subTest(version=version), patch('corpus_upload.aws_run', return_value=f'aws-cli/{version} Python/test'):
                if accepted:
                    self.module.require_cli()
                else:
                    with self.assertRaisesRegex(ValueError, '2.34.0'):
                        self.module.require_cli()

    def test_part_uses_one_cli_attempt_even_when_environment_overrides_it(self):
        with patch.dict('os.environ', {'AWS_MAX_ATTEMPTS': '9', 'AWS_RETRY_MODE': 'adaptive'}), \
                patch('subprocess.run', return_value=subprocess.CompletedProcess([], 0, stdout='{}')) as run:
            self.module.aws_call(['aws'], ['upload-part'])
            self.module.aws_call(['aws'], ['head-object'])
        part_env = run.call_args_list[0].kwargs['env']
        self.assertEqual(part_env['AWS_MAX_ATTEMPTS'], '1')
        self.assertEqual(part_env['AWS_RETRY_MODE'], 'standard')
        head_env = run.call_args_list[1].kwargs['env']
        self.assertEqual(head_env['AWS_MAX_ATTEMPTS'], '9')
        self.assertEqual(head_env['AWS_RETRY_MODE'], 'adaptive')

    def test_aws_error_json_retries_only_transient_classes_and_logs_each_attempt(self):
        for code, retry in [('SlowDown', True), ('Throttling', True), ('RequestTimeout', True),
                            ('500', True), ('502', True), ('503', True), ('504', True), ('429', True),
                            ('InternalError', True), ('InternalServerError', True), ('ServiceUnavailable', True),
                            ('ThrottlingException', True), ('TooManyRequestsException', True),
                            ('AccessDenied', False), ('InvalidAccessKeyId', False),
                            ('SignatureDoesNotMatch', False), ('401', False), ('403', False),
                            ('BadDigest', False), ('EntityTooLarge', False), ('InvalidPart', False),
                            ('NoSuchUpload', False), ('a' * 64, False)]:
            with self.subTest(code=code):
                calls = []
                log = io.StringIO()

                def run(command, **kwargs):
                    self.assertEqual(kwargs['env']['AWS_CLI_ERROR_FORMAT'], 'json')
                    args = command
                    operation = next(a for a in args if a in ('create-multipart-upload', 'upload-part', 'abort-multipart-upload'))
                    calls.append(operation)
                    if operation == 'create-multipart-upload':
                        return subprocess.CompletedProcess(args, 0, stdout='{"UploadId":"u"}')
                    failure_code = 'AccessDenied' if operation == 'abort-multipart-upload' else code
                    raise subprocess.CalledProcessError(1, args, stderr=json.dumps({'Code': failure_code, 'Message': 'SECRET'}))

                def client(args):
                    return self.module.aws_call(['aws'], args)

                with patch('subprocess.run', side_effect=run), patch('time.sleep'), contextlib.redirect_stderr(log):
                    with self.assertRaises(self.module.AWSError) as error:
                        self.module.upload_file(self.path, 'key', client, part_size=3)
                self.assertEqual(error.exception.code, code if code != 'a' * 64 else 'UnknownError')
                attempts = 4 if retry else 1
                self.assertEqual(calls.count('upload-part'), attempts)
                self.assertEqual(log.getvalue().count('aws upload-part attempt '), attempts)
                for attempt in range(1, attempts + 1):
                    self.assertIn(f'aws upload-part attempt {attempt}:', log.getvalue())
                self.assertIn('aws abort-multipart-upload attempt 1: AccessDenied', log.getvalue())
                self.assertNotIn('SECRET', log.getvalue())
                if code == 'a' * 64:
                    self.assertNotIn(code, log.getvalue())

    def test_network_failure_retries_and_logs_recovery(self):
        calls, parts, log = [], [], io.StringIO()

        def run(command, **kwargs):
            operation = next(a for a in command if a in ('create-multipart-upload', 'upload-part', 'complete-multipart-upload'))
            calls.append(operation)
            if operation == 'create-multipart-upload':
                return subprocess.CompletedProcess(command, 0, stdout='{"UploadId":"u"}')
            if operation == 'upload-part':
                parts.append((command[command.index('--upload-id') + 1], command[command.index('--part-number') + 1],
                              Path(command[command.index('--body') + 1]).read_bytes()))
                if calls.count(operation) == 1:
                    raise subprocess.CalledProcessError(1, command, stderr='Could not connect to the endpoint URL: "https://secret"')
            if operation == 'complete-multipart-upload':
                receipts = json.loads(command[command.index('--multipart-upload') + 1])['Parts']
                self.assertEqual(receipts[0], {'PartNumber': 1, 'ETag': 'etag'})
            return subprocess.CompletedProcess(command, 0, stdout='{"ETag":"etag"}')

        with patch('subprocess.run', side_effect=run), patch('time.sleep'), contextlib.redirect_stderr(log):
            self.module.upload_file(self.path, 'key', lambda args: self.module.aws_call(['aws'], args), part_size=3)
        self.assertEqual(parts[:2], [('u', '1', b'abc'), ('u', '1', b'abc')])
        self.assertIn('aws upload-part attempt 1', log.getvalue())
        self.assertIn('network', log.getvalue().lower())
        self.assertNotIn('secret', log.getvalue())
        self.assertEqual(calls.count('upload-part'), 4)

    def test_darwin_deadline_clock_counts_sleep(self):
        with patch('corpus_upload.sys.platform', 'darwin'), patch('time.clock_gettime', return_value=7) as clock:
            self.assertEqual(self.module.elapsed_clock(), 7)
        clock.assert_called_once_with(self.module.time.CLOCK_MONOTONIC_RAW)

    def test_sleep_during_completion_exceeds_deadline(self):
        calls = []
        with patch('corpus_upload.elapsed_clock', side_effect=[0, 0, 0, 0, 0, 50000]), \
                self.assertRaisesRegex(ValueError, 'deadline'):
            self.module.upload_file(self.path, 'key', self.multipart_client(calls), part_size=3)
        self.assertTrue(any(c[0] == 'complete-multipart-upload' for c in calls))

    def test_sleep_aware_clock_is_used_for_deadline(self):
        calls = []
        with patch('corpus_upload.elapsed_clock', side_effect=[0, 0, 50000]), self.assertRaisesRegex(ValueError, 'deadline'):
            self.module.upload_file(self.path, 'key', self.multipart_client(calls), part_size=3)
        self.assertFalse(any(c[0] == 'complete-multipart-upload' for c in calls))

    def test_part_retries_and_completes_with_final_etag(self):
        calls = []
        failures = 0

        def client(args):
            nonlocal failures
            calls.append(args)
            if args[0] == 'create-multipart-upload':
                return {'UploadId': 'upload'}
            if args[0] == 'upload-part':
                if failures < 3:
                    failures += 1
                    raise self.module.AWSError('upload-part', 'NetworkError', True)
                return {'ETag': 'final'}
            if args[0] == 'complete-multipart-upload':
                parts = json.loads(args[args.index('--multipart-upload') + 1])['Parts']
                self.assertEqual(parts[0], {'PartNumber': 1, 'ETag': 'final'})
                return {'ETag': 'complete'}
            self.fail(str(args))

        with patch('time.sleep') as sleep:
            self.module.upload_file(self.path, 'key', client, part_size=3)
        self.assertEqual(failures, 3)
        self.assertEqual(sleep.call_count, 3)
        self.assertEqual([c[c.index('--part-number') + 1] for c in calls if c[0] == 'upload-part'],
                         ['1', '1', '1', '1', '2', '3'])

    def test_part_gives_up_after_three_retries_and_preserves_error(self):
        calls = []

        def client(args):
            calls.append(args)
            if args[0] == 'create-multipart-upload':
                return {'UploadId': 'upload'}
            if args[0] == 'upload-part':
                raise self.module.AWSError('upload-part', 'SlowDown', True)
            raise RuntimeError('abort denied')

        with patch('time.sleep'), self.assertRaisesRegex(ValueError, 'SlowDown'):
            self.module.upload_file(self.path, 'key', client, part_size=3)
        self.assertEqual(sum(c[0] == 'upload-part' for c in calls), 4)
        self.assertEqual(calls[-1][0], 'abort-multipart-upload')

    def test_aws_failure_reports_operation_and_bounded_stderr_without_command(self):
        failure = subprocess.CalledProcessError(1, ['aws', 'sensitive-argument'], stderr='network error ' + 'x' * 5000)
        with patch('subprocess.run', side_effect=failure), self.assertRaises(ValueError) as error:
            self.module.aws_call(['aws', 'sensitive-argument'], ['head-object'])
        self.assertIn('head-object', str(error.exception))
        self.assertIn('UnknownError', str(error.exception))
        self.assertNotIn('sensitive-argument', str(error.exception))
        self.assertLess(len(str(error.exception)), 4200)

    def test_version_missing_cli_reports_operation_without_path(self):
        with patch('subprocess.run', side_effect=FileNotFoundError('SECRET')), self.assertRaises(ValueError) as error:
            self.module.require_cli()
        self.assertIn('version', str(error.exception))
        self.assertIn('CommandStartError', str(error.exception))
        self.assertNotIn('SECRET', str(error.exception))

    def test_version_failure_uses_filtered_error(self):
        failure = subprocess.CalledProcessError(1, ['aws'], stderr='Authorization: SECRET')
        with patch('subprocess.run', side_effect=failure), self.assertRaises(ValueError) as error:
            self.module.require_cli()
        self.assertIn('version', str(error.exception))
        self.assertNotIn('SECRET', str(error.exception))

    def test_prefix_check_filters_aws_errors(self):
        failure = subprocess.CalledProcessError(1, ['aws'], stderr=json.dumps({'Code': 'AccessDenied', 'Message': 'Authorization: SECRET'}))
        with patch('subprocess.run', side_effect=failure), self.assertRaises(ValueError) as error:
            self.module.require_empty_prefix(['aws'], 'bucket', 'prefix/')
        self.assertIn('list-objects-v2', str(error.exception))
        self.assertIn('AccessDenied', str(error.exception))
        self.assertNotIn('SECRET', str(error.exception))

    def test_aws_error_filters_headers_and_signatures(self):
        failure = subprocess.CalledProcessError(1, ['aws'], stderr=json.dumps({'Code': 'AccessDenied', 'Message': 'Authorization: SECRET\nX-Amz-Signature=SECRET'}))
        with patch('subprocess.run', side_effect=failure), self.assertRaises(ValueError) as error:
            self.module.aws_call(['aws'], ['put-object'])
        self.assertIn('AccessDenied', str(error.exception))
        self.assertNotIn('SECRET', str(error.exception))
        self.assertNotIn('Authorization', str(error.exception))

    def test_missing_multipart_upload_stops_without_retry(self):
        calls = []

        def client(args):
            calls.append(args)
            if args[0] == 'create-multipart-upload':
                return {'UploadId': 'upload'}
            if args[0] == 'upload-part':
                raise self.module.AWSError('upload-part', 'NoSuchUpload')
            return {}

        with patch('time.sleep') as sleep, self.assertRaisesRegex(ValueError, 'NoSuchUpload'):
            self.module.upload_file(self.path, 'key', client, part_size=3)
        self.assertEqual(sum(c[0] == 'upload-part' for c in calls), 1)
        sleep.assert_not_called()
        self.assertFalse(any(c[0] == 'complete-multipart-upload' for c in calls))

    def test_multipart_deadline_prevents_completion(self):
        calls = []
        with patch('corpus_upload.elapsed_clock', side_effect=[0, 0, 50000]), self.assertRaisesRegex(ValueError, 'deadline'):
            self.module.upload_file(self.path, 'key', self.multipart_client(calls), part_size=3)
        self.assertFalse(any(c[0] == 'complete-multipart-upload' for c in calls))
        self.assertEqual(calls[-1][0], 'abort-multipart-upload')

    def test_failed_part_aborts_without_completing_the_upload(self):
        calls = []

        def client(args):
            calls.append(args)
            if 'create-multipart-upload' in args:
                return {'UploadId': 'synthetic-upload'}
            if 'upload-part' in args:
                raise ValueError('synthetic failed part')
            return {}

        with self.assertRaisesRegex(ValueError, 'failed part'):
            self.module.upload_file(self.path, 'vn/20990102_1/corpus.dump', client, part_size=3)
        self.assertIn('abort-multipart-upload', calls[-1])
        self.assertFalse(any('complete-multipart-upload' in call for call in calls))


if __name__ == '__main__':
    unittest.main()
