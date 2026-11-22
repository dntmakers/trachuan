import subprocess
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from updater.deploy_corpus_database import Database
from updater.deploy_corpus_kind import FRAMEWORKS, LAW
from updater.deploy_host import Host
from updater.deploy_io import command, CommandError, StopRequested, stop, terminate


ID = 'a' * 64


class SmokeContainerTest(unittest.TestCase):
    def run_server(self, failure=None, listings=None, remove_failure=False):
        calls = []
        listings = iter(listings if listings is not None else [ID.encode() + b'\n', b''])

        def runner(args, **kwargs):
            calls.append((args, kwargs))
            if args[:2] == ['podman', 'run']:
                if failure:
                    raise failure
                return b'{}'
            if args[:2] == ['podman', 'ps']:
                result = next(listings)
                if isinstance(result, Exception):
                    raise result
                return result
            if args[:2] == ['podman', 'rm']:
                if remove_failure:
                    raise CommandError('remove failed')
                return b''
            raise AssertionError(args)

        host = Host(SimpleNamespace(host=Path('/host')), runner, user_prefix=[])
        db = Database(host, {}, LAW)
        try:
            db.server('image', 'release-check')
        except Exception as error:
            result = error
        else:
            result = None
        return calls, result

    def assert_cleanup(self, calls):
        run, query, remove, confirm = calls
        label = run[0][run[0].index('--label') + 1]
        self.assertRegex(label, r'^trachuan\.updater=[0-9a-f]{32}$')
        self.assertEqual(query[0], ['podman', 'ps', '-a', '--no-trunc', '--filter', 'label=' + label,
                                    '--format', '{{.ID}}'])
        self.assertEqual(remove[0], ['podman', 'rm', '--force', '--time', '0', ID])
        self.assertEqual(confirm[0], query[0])
        for _, kwargs in calls[1:]:
            self.assertTrue(kwargs['cancel'])
            self.assertGreater(kwargs['timeout'], 0)
            self.assertLessEqual(kwargs['timeout'], 10)

    def test_stop_cleans_up_and_confirms(self):
        calls, result = self.run_server(StopRequested('interrupted'))
        self.assertIsInstance(result, StopRequested)
        self.assert_cleanup(calls)

    def test_timeout_cleans_up_and_confirms(self):
        calls, result = self.run_server(CommandError('timeout', reason='timeout'))
        self.assertEqual(result.reason, 'timeout')
        self.assert_cleanup(calls)

    def test_name_collision_does_not_remove_unowned_container(self):
        error = CommandError('run failed', returncode=125)
        calls, result = self.run_server(error, listings=[b''])
        self.assertIs(result, error)
        self.assertEqual(len(calls), 2)
        self.assertEqual(calls[1][0][:2], ['podman', 'ps'])

    def test_failed_or_invalid_queries_do_not_prove_absence(self):
        for listing in (CommandError('wrapper failed', returncode=1), b'invalid\n', b'\n',
                        b'a' * 12 + b'\n', (ID + '\n' + ID + '\n').encode()):
            with self.subTest(listing=listing):
                calls, result = self.run_server(listings=[listing])
                self.assertEqual(result.detail, 'smoke container teardown failed')
                self.assertEqual(FRAMEWORKS.cause(result), result.detail)
                self.assertFalse(any(args[:2] == ['podman', 'rm'] for args, _ in calls))

    def test_confirmation_requires_successful_empty_query(self):
        for listing in (CommandError('wrapper failed', returncode=1), b'invalid\n', ID.encode() + b'\n'):
            with self.subTest(listing=listing):
                _, result = self.run_server(listings=[ID.encode() + b'\n', listing])
                self.assertEqual(result.detail, 'smoke container teardown failed')

    def test_stuck_cleanup_process_is_reported(self):
        failure = CommandError('process did not exit', detail='process did not exit')
        _, result = self.run_server(listings=[failure])
        self.assertEqual(result.detail, 'smoke container teardown failed: process did not exit')
        self.assertEqual(FRAMEWORKS.cause(result), result.detail)

    def test_remove_failure_is_reported_even_if_absent(self):
        calls, result = self.run_server(remove_failure=True)
        self.assertEqual(result.detail, 'smoke container teardown failed')
        self.assert_cleanup(calls)

    def test_unique_labels(self):
        first, _ = self.run_server()
        second, _ = self.run_server()
        self.assertNotEqual(first[0][0][5], second[0][0][5])

    def test_cleanup_commands_bypass_stop_but_not_validation(self):
        label = 'label=trachuan.updater=' + 'a' * 32
        stop.request()
        self.addCleanup(stop.clear)
        with mock.patch('updater.deploy_io.subprocess.Popen', side_effect=OSError('synthetic')) as popen:
            for args in (['podman', 'rm', '--force', '--time', '0', ID],
                         ['podman', 'ps', '-a', '--no-trunc', '--filter', label, '--format', '{{.ID}}']):
                with self.assertRaises(CommandError):
                    command(args, timeout=10, cancel=True)
            self.assertEqual(popen.call_count, 2)
            with self.assertRaises(ValueError):
                command(['podman', 'rm', '--force', '--time', '0', 'other'], timeout=10, cancel=True)

    def test_cleanup_uses_one_deadline(self):
        for durations, expected, failed in (([4, 3, 0], [10, 6, 3], False),
                                            ([10], [10], True), ([4, 6], [10, 6], True),
                                            ([4, 3, 3], [10, 6, 3], True)):
            with self.subTest(durations=durations):
                current = [0]
                calls = []
                elapsed = iter(durations)
                def runner(args, **kwargs):
                    if args[:2] == ['podman', 'run']:
                        return b'{}'
                    calls.append(kwargs['timeout'])
                    current[0] += next(elapsed)
                    return ID.encode() + b'\n' if len(calls) == 1 else b''
                host = Host(SimpleNamespace(host=Path('/host')), runner, user_prefix=[])
                db = Database(host, {}, LAW)
                clock = SimpleNamespace(monotonic=lambda: current[0])
                with mock.patch('updater.deploy_smoke_container.time', clock, create=True):
                    if failed:
                        with self.assertRaisesRegex(CommandError, 'smoke container teardown failed'):
                            db.server('image', 'release-check')
                    else:
                        self.assertEqual(db.server('image', 'release-check'), b'{}')
                self.assertEqual(calls, expected)

    def test_cleanup_allows_only_finite_positive_remaining_timeout(self):
        label = 'label=trachuan.updater=' + 'a' * 32
        query = ['podman', 'ps', '-a', '--no-trunc', '--filter', label, '--format', '{{.ID}}']
        caddy = ['systemctl', '--user', 'stop', 'trachuan-caddy.service']
        with mock.patch('updater.deploy_io.subprocess.Popen', side_effect=OSError('synthetic')) as popen:
            for timeout in (0.1, 5, 10):
                with self.assertRaises(CommandError):
                    command(query, timeout=timeout, cancel=True)
            self.assertEqual(popen.call_count, 3)
            for timeout in (0, -1, 10.1, float('nan'), float('inf'), True):
                with self.subTest(timeout=timeout), self.assertRaises(ValueError):
                    command(query, timeout=timeout, cancel=True)
            for timeout in (1, 9.9):
                with self.subTest(timeout=timeout), self.assertRaises(ValueError):
                    command(caddy, timeout=timeout, cancel=True)

    def test_terminate_wait_after_kill_is_bounded_and_reported(self):
        process = mock.Mock(pid=123, returncode=None)
        process.wait.side_effect = subprocess.TimeoutExpired('test', 10)
        with mock.patch('updater.deploy_io.os.killpg'), mock.patch('updater.deploy_io.os.waitid', return_value=True):
            with self.assertRaisesRegex(CommandError, 'process did not exit'):
                terminate(process)
        self.assertIsNotNone(process.wait.call_args.kwargs['timeout'])
