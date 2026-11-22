import importlib.util
import json
from datetime import datetime, timezone
from types import SimpleNamespace
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from updater.deploy_io import atomic, command, CommandError, decode, describe, failure_reason
from updater.deploy_result import Result
from updater.deploy_corpus_result import ReleaseResult
from watch import observations, storage


SPEC = importlib.util.spec_from_file_location('updater_metrics', Path(__file__).parents[1] / 'metrics/publish-metrics.py')
METRICS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(METRICS)


class IOTest(unittest.TestCase):
    def test_atomic_rename_failure_leaves_previous_bytes_and_no_temporary_file(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'state.json'
            path.write_bytes(b'previous')
            with mock.patch('updater.deploy_io.os.replace', side_effect=OSError('synthetic')):
                with self.assertRaises(OSError):
                    atomic(path, b'next')
            self.assertEqual(path.read_bytes(), b'previous')
            self.assertEqual(list(Path(directory).iterdir()), [path])

    def test_prior_surrogate_timestamp_is_normalized_for_normal_and_overflow_receipts(self):
        for oversized in (False, True):
            with self.subTest(oversized=oversized), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                path, log = root / 'release.json', root / 'deploy.log'
                path.write_bytes(b'{"last_success_at":"2026-01-01\\ud80000:00:00+00:00",'
                                 b'"waiting_since":"2026-01-01\\ud80000:00:00+00:00"}')
                result = ReleaseResult(SimpleNamespace(result=root / 'result.json', log=log), lambda: 1767312000)
                if oversized:
                    with self.assertRaisesRegex(ValueError, 'result too large'):
                        result.write('failed', 'result', 'x' * 100000)
                else:
                    result.write('failed', 'result', 'failed')
                value = json.loads(path.read_bytes())
                self.assertEqual(value['last_success_at'], '2026-01-01T00:00:00Z')
                self.assertEqual(value['waiting_since'], '2026-01-01T00:00:00Z')
                observed = observations.result(path, 'release')
                self.assertEqual(observed['last_success_at'], 1767225600)
                self.assertEqual(observed['waiting_since'], 1767225600)

    def test_overflow_uses_valid_previous_floor_when_state_is_unavailable(self):
        with tempfile.TemporaryDirectory() as directory:
            path, log = Path(directory) / 'result.json', Path(directory) / 'deploy.log'
            path.write_text('{"last_success_at":"2026-01-01T00:00:00Z","release_floor":17}')
            result = Result(path, log, clock=lambda: '2026-01-02T00:00:00Z')
            with self.assertRaisesRegex(ValueError, 'result too large'):
                result.write('failed', 'result', 'x' * 100000)
            self.assertIn(('deploy_release_floor', 17), METRICS.deploy(path, datetime(2026, 1, 2, tzinfo=timezone.utc)))

    def test_overlong_release_identity_produces_watchable_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'result.json'
            log = Path(directory) / 'deploy.log'
            result = Result(path, log, clock=lambda: '2026-01-02T00:00:00Z')
            with self.assertRaisesRegex(ValueError, 'result too large'):
                result.write('ok', 'complete', 'ok', manifest={'release': 'v' * 257})
            self.assertEqual(observations.result(path, 'deploy')['status'], 'failed')

    def test_receipt_limit_matches_both_readers_at_boundary(self):
        with tempfile.TemporaryDirectory() as directory:
            path, log = Path(directory) / 'result.json', Path(directory) / 'deploy.log'
            result = Result(path, log, clock=lambda: '2026-01-02T00:00:00Z')
            base = {'status': 'failed', 'started_at': '2026-01-02T00:00:00Z', 'message': ''}
            size = len(json.dumps(base, sort_keys=True, separators=(',', ':')).encode()) + 1
            value = dict(base, message='x' * (65536 - size))
            result.publish(value)
            self.assertEqual(path.stat().st_size, 65536)
            self.assertEqual(observations.result(path, 'deploy')['status'], 'failed')
            self.assertIn(('deploy_failed', 1), METRICS.deploy(path, datetime(2026, 1, 2, tzinfo=timezone.utc)))
            value['message'] += 'x'
            with self.assertRaisesRegex(ValueError, 'result too large'):
                result.publish(value)
            self.assertLess(path.stat().st_size, 65536)
            self.assertEqual(observations.result(path, 'deploy')['status'], 'failed')

    def test_receipt_limit_matches_watch_and_metrics(self):
        from updater import deploy_result
        self.assertEqual(deploy_result.RESULT_LIMIT, 65536)
        self.assertEqual(storage.MAX_RESULT_BYTES, deploy_result.RESULT_LIMIT)
        self.assertEqual(METRICS.MAX_RESULT_BYTES, deploy_result.RESULT_LIMIT)

    def test_valid_waiting_timestamp_without_hash_is_not_corruption(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path, log = root / 'release.json', root / 'release.log'
            path.write_text('{"waiting_since":"2026-01-01T00:00:00Z"}')
            result = ReleaseResult(SimpleNamespace(result=root / 'result.json', log=root / 'deploy.log'),
                                   lambda: 1767312000)
            result.write('failed', 'result', 'failed')
            self.assertEqual(observations.result(path, 'release')['waiting_since'], 1767225600)
            events = [json.loads(line) for line in log.read_bytes().splitlines()]
            self.assertFalse(any(row.get('level') == 'warning' for row in events))

    def test_invalid_previous_scalars_are_dropped_and_warned(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path, log = root / 'release.json', root / 'deploy.log'
            path.write_text('{"last_success_at":"invalid","waiting_since":{},'
                            '"waiting_manifest_hash":"not-a-hash","manifest_hash":[],"release_floor":"invalid"}')
            result = ReleaseResult(SimpleNamespace(result=root / 'result.json', log=log), lambda: 1767312000)
            result.write('failed', 'fetch', 'failed')
            observed = observations.result(path, 'release')
            self.assertIsNone(observed['last_success_at'])
            self.assertIsNone(observed['waiting_since'])
            self.assertIsNone(observed.get('waiting_manifest_hash'))
            events = [json.loads(line) for line in result.log.read_bytes().splitlines()]
            self.assertTrue(any(row.get('message') == 'previous result unreadable' for row in events))

    def test_overflow_receipts_are_readable_by_watch_and_metrics(self):
        for kind in ('deploy', 'secrets', 'release'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                path, log = root / 'result.json', root / 'deploy.log'
                prior = {'last_success_at': '2026-01-01T00:00:00Z'}
                if kind == 'release':
                    path = root / 'release.json'
                path.write_text(json.dumps(prior))
                state = SimpleNamespace(data={'release_floor': 17})
                if kind == 'release':
                    result = ReleaseResult(SimpleNamespace(result=root / 'result.json', log=log),
                                           lambda: 1767312000, integrity=lambda: ['record_rebuilt'])
                else:
                    result = Result(path, log, clock=lambda: '2026-01-02T00:00:00Z', state=state)
                with self.assertRaisesRegex(ValueError, 'result too large'):
                    result.write('failed', 'manifest', 'x' * 100000)
                observed = observations.result(path, kind)
                self.assertEqual(observed['status'], 'failed')
                self.assertNotIn('manifest_hash', observed)
                self.assertNotIn('waiting_manifest_hash', observed)
                self.assertNotIn('release', observed)
                self.assertNotIn('sequence', observed)
                self.assertIsNotNone(observed['started_at'])
                self.assertIsNotNone(observed['finished_at'])
                self.assertIsNotNone(observed['last_success_at'])
                now = datetime(2026, 1, 2, tzinfo=timezone.utc)
                if kind == 'deploy':
                    self.assertIn(('deploy_release_floor', 17), METRICS.deploy(path, now))
                    self.assertIn(('deploy_last_success_age_seconds', 86400), METRICS.deploy(path, now))
                elif kind == 'release':
                    self.assertEqual(observed['integrity'], ['record_rebuilt'])
                    self.assertIn(('release_integrity', 1), METRICS.release(path, now))
                else:
                    self.assertIn(('secrets_last_success_age_seconds', 86400), METRICS.job_result(
                        path, now, 'secrets', ('failed',), ('failed',)))

    def test_oversized_result_records_bounded_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'result.json'
            log = Path(directory) / 'deploy.log'
            with self.assertRaisesRegex(ValueError, 'result too large'):
                Result(path, log).write('ok', 'complete', 'x' * (1024 * 1024))
            value = json.loads(path.read_bytes())
            self.assertEqual((value['status'], value['step'], value['message']),
                             ('failed', 'result', 'result too large'))
            self.assertLessEqual(path.stat().st_size, 1024 * 1024)

    def test_corrupt_previous_result_is_logged_without_blocking_new_result(self):
        for raw in (b'{', b' ' * (1024 * 1024 + 1)):
            with self.subTest(size=len(raw)), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'result.json'
                log = Path(directory) / 'deploy.log'
                path.write_bytes(raw)
                Result(path, log).write('failed', 'manifest', 'rejected')
                self.assertEqual(json.loads(path.read_bytes())['status'], 'failed')
                events = [json.loads(line) for line in log.read_bytes().splitlines()]
                self.assertTrue(any(event.get('level') == 'warning' and
                                    event.get('message') == 'previous result unreadable' for event in events))

    def test_decode_wraps_recursion_error(self):
        with mock.patch('updater.deploy_io.json.loads', side_effect=RecursionError):
            with self.assertRaisesRegex(ValueError, 'invalid manifest JSON'):
                decode(b'[]', 'manifest')

    def test_oversized_previous_result_is_ignored(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'result.json'
            path.write_bytes(b'{"last_success_at":"' + b'x' * (1024 * 1024) + b'"}')
            result = Result(path, Path(directory) / 'deploy.log')
            self.assertEqual(result.previous, {})

    def test_invalid_previous_result_does_not_block_failure_reporting(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'result.json'
            log = Path(directory) / 'deploy.log'
            path.write_text('[]')
            result = Result(path, log, clock=lambda: '2026-01-01T00:00:00Z')
            result.write('failed', 'manifest', 'manifest rejected')
            self.assertEqual(json.loads(path.read_bytes())['status'], 'failed')
            self.assertEqual(path.stat().st_mode & 0o777, 0o640)

    def test_failure_preserves_last_success_without_logging_command_output(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'result.json'
            log = Path(directory) / 'deploy.log'
            Result(path, log, clock=lambda: '2026-01-01T00:00:00Z').write('ok', 'complete', 'activated')
            Result(path, log, clock=lambda: '2026-01-02T00:00:00Z').write('failed', 'manifest', 'manifest rejected')
            value = json.loads(path.read_bytes())
            self.assertEqual(value['last_success_at'], '2026-01-01T00:00:00Z')
            self.assertEqual(value['finished_at'], '2026-01-02T00:00:00Z')


    def test_current_refreshes_last_success(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'result.json'
            log = Path(directory) / 'deploy.log'
            Result(path, log, clock=lambda: '2026-01-01T00:00:00Z').write('ok', 'complete', 'activated')
            Result(path, log, clock=lambda: '2026-01-02T00:00:00Z').write('current', 'noop', 'already active')
            self.assertEqual(json.loads(path.read_bytes())['last_success_at'], '2026-01-02T00:00:00Z')

    def test_safe_sdk_code_is_extracted_without_other_error_output(self):
        with self.assertRaises(CommandError) as raised:
            command(['/bin/sh', '-c', 'printf "OCI_ERROR:NotAuthorizedOrNotFound\\n" >&2; exit 1'],
                    error_prefix='OCI_ERROR:')
        self.assertEqual(raised.exception.code, 'NotAuthorizedOrNotFound')
        self.assertNotIn('NotAuthorizedOrNotFound', raised.exception.detail)

    def test_failed_command_detail_omits_stderr_of_other_programs(self):
        with self.assertRaises(CommandError) as raised:
            command(['/bin/sh', '-c', 'echo password=hunter2 >&2; exit 3'])
        self.assertEqual(raised.exception.detail, 'sh -c: exit 3')

    def test_failed_command_detail_skips_runuser_prefix(self):
        with self.assertRaises(CommandError) as raised:
            command(['/usr/bin/env', '--', 'sh', '-c', 'echo boom >&2; exit 1'])
        self.assertEqual(raised.exception.detail, 'sh -c: exit 1')

    def fake_tool(self, directory, name, stderr):
        path = Path(directory) / name
        path.write_text('#!/bin/sh\nprintf "%s\\n" "$STDERR_TEXT" >&2\nexit 1\n')
        path.chmod(0o755)
        return path, dict(STDERR_TEXT=stderr)

    def test_oras_detail_keeps_single_line_bounded_stderr_tail(self):
        with tempfile.TemporaryDirectory() as directory:
            path, env = self.fake_tool(directory, 'oras', 'first line\nError: ' + 'x' * 400)
            with mock.patch.dict('os.environ', env), self.assertRaises(CommandError) as raised:
                command([str(path), 'repo', 'tags', 'ghcr.io/x/y'])
        detail = raised.exception.detail
        self.assertTrue(detail.startswith('oras repo tags: exit 1: '))
        self.assertNotIn('\n', detail)
        self.assertEqual(len(detail), len('oras repo tags: exit 1: ') + 300)

    def test_oras_detail_withholds_stderr_that_mentions_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            path, env = self.fake_tool(directory, 'oras', 'error decoding auth: user:s3cret')
            with mock.patch.dict('os.environ', env), self.assertRaises(CommandError) as raised:
                command([str(path), 'resolve', 'ghcr.io/x/y:production'])
        self.assertEqual(raised.exception.detail, 'oras resolve: exit 1: error output withheld')

    def test_oras_detail_withholds_credentials_beyond_the_tail_window(self):
        with tempfile.TemporaryDirectory() as directory:
            path, env = self.fake_tool(directory, 'oras', 'password=' + 'x' * 400)
            with mock.patch.dict('os.environ', env), self.assertRaises(CommandError) as raised:
                command([str(path), 'resolve', 'ghcr.io/x/y:production'])
        self.assertEqual(raised.exception.detail, 'oras resolve: exit 1: error output withheld')

    def test_detail_names_innermost_program_through_wrappers(self):
        args = ['runuser', '-u', 'trachuan', '--', 'podman', 'exec', '-i', 'trachuan-postgres', 'sh', '-c',
                'exec "$@"', 'sh', 'env', 'PGOPTIONS=-c statement_timeout=0', 'pg_restore', '--no-password']
        self.assertEqual(describe(args, b'pg_restore: error: could not execute query\n', 1), 'pg_restore: exit 1')
        self.assertEqual(describe(args[:-2] + ['psql', '-XqAt'], b'ERROR: password authentication failed', 2),
                         'psql: exit 2')
        self.assertEqual(describe(args, b'pg_restore: error: password=x', 1), 'pg_restore: exit 1')

    def test_ghcr_anonymous_denied_on_rollback_tags_is_classified(self):
        stderr = b'Error response from registry: denied: requested access to the resource is denied\n'
        self.assertEqual(failure_reason(['/usr/local/libexec/trachuan/bin/oras', 'repo', 'tags'], stderr), 'denied')
        self.assertEqual(failure_reason(['/usr/local/libexec/trachuan/bin/oras', 'resolve'], stderr), 'denied')
        self.assertEqual(failure_reason(['/usr/local/libexec/trachuan/bin/oras', 'manifest', 'fetch'], stderr), 'failed')
class ProcessControlTest(unittest.TestCase):
    def setUp(self):
        from updater.deploy_io import stop
        stop.clear()
        self.addCleanup(stop.clear)

    def test_signal_interrupts_child_wait(self):
        import os
        import signal
        import threading
        from updater import main
        from updater.deploy_io import StopRequested, interrupted
        old = signal.signal(signal.SIGTERM, main.interrupted)
        timer = threading.Timer(0.2, os.kill, args=(os.getpid(), signal.SIGTERM))
        timer.start()
        try:
            with self.assertRaises(StopRequested) as raised:
                command(['/bin/sleep', '5'], timeout=3, stdout_limit=10)
            self.assertTrue(interrupted(raised.exception))
        finally:
            timer.cancel()
            timer.join()
            signal.signal(signal.SIGTERM, old)

    def test_early_empty_select_is_not_timeout(self):
        import selectors
        original = selectors.DefaultSelector.select
        calls = []
        def select(selector, timeout=None):
            if not calls:
                calls.append(True)
                return []
            return original(selector, timeout)
        with mock.patch.object(selectors.DefaultSelector, 'select', select):
            self.assertEqual(command(['/bin/echo', 'ok'], timeout=2, stdout_limit=10), b'ok\n')

    def test_timeout_reports_program_without_arguments(self):
        for limit in (None, 10):
            with self.subTest(limit=limit), self.assertRaises(CommandError) as raised:
                command(['/bin/sleep', '5'], timeout=0.05, stdout_limit=limit)
            self.assertEqual(raised.exception.reason, 'timeout')
            self.assertEqual(raised.exception.detail, 'sleep: timeout')


if __name__ == '__main__':
    unittest.main()
