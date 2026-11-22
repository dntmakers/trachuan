import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from updater.deploy_corpus_database import Database
from updater.deploy_corpus_kind import LAW
from updater.deploy_host import Host
from updater.deploy_io import command, CommandError, StopRequested, stop


class SmokeContainerTest(unittest.TestCase):
    def run_server(self, failure=None, exists=1, remove_failure=False):
        calls = []

        def runner(args, **kwargs):
            calls.append((args, kwargs))
            if args[:2] == ['podman', 'run']:
                if failure:
                    raise failure
                return b'{}'
            if args[:2] == ['podman', 'rm']:
                if remove_failure:
                    raise CommandError('remove failed')
                return b''
            if args[:3] == ['podman', 'container', 'exists']:
                if exists:
                    raise CommandError('exists failed', returncode=exists)
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
        run, remove, exists = calls
        name = run[0][run[0].index('--name') + 1]
        self.assertIn('--label', run[0])
        self.assertEqual(remove[0], ['podman', 'rm', '--force', '--time', '0', name])
        self.assertEqual(exists[0], ['podman', 'container', 'exists', name])
        self.assertTrue(remove[1]['cancel'])
        self.assertTrue(exists[1]['cancel'])
        self.assertEqual(remove[1]['timeout'], 10)
        self.assertEqual(exists[1]['timeout'], 10)

    def test_stop_cleans_up_and_confirms(self):
        calls, result = self.run_server(StopRequested('interrupted'))
        self.assertIsInstance(result, StopRequested)
        self.assert_cleanup(calls)

    def test_timeout_cleans_up_and_confirms(self):
        calls, result = self.run_server(CommandError('timeout', reason='timeout'))
        self.assertEqual(result.reason, 'timeout')
        self.assert_cleanup(calls)

    def test_failed_confirmation_is_reported(self):
        calls, result = self.run_server(exists=0)
        self.assertIsInstance(result, CommandError)
        self.assertEqual(result.detail, 'smoke container teardown failed')
        self.assertEqual(LAW.cause(result), 'smoke container teardown failed')
        from updater.deploy_corpus_kind import FRAMEWORKS
        self.assertEqual(FRAMEWORKS.cause(result), 'smoke container teardown failed')
        self.assert_cleanup(calls)

    def test_unique_names(self):
        first, _ = self.run_server()
        second, _ = self.run_server()
        self.assertNotEqual(first[0][0][3], second[0][0][3])

    def test_cleanup_commands_bypass_stop_but_not_validation(self):
        name = 'trachuan-updater-smoke-' + 'a' * 32
        stop.request()
        self.addCleanup(stop.clear)
        with mock.patch('updater.deploy_io.subprocess.Popen', side_effect=OSError('synthetic')) as popen:
            for args in (['podman', 'rm', '--force', '--time', '0', name],
                         ['podman', 'container', 'exists', name]):
                with self.assertRaises(CommandError):
                    command(args, timeout=10, cancel=True)
            self.assertEqual(popen.call_count, 2)
            with self.assertRaises(ValueError):
                command(['podman', 'rm', '--force', '--time', '0', 'other'], timeout=10, cancel=True)

    def test_remove_failure_is_reported_even_if_absent(self):
        calls, result = self.run_server(remove_failure=True)
        self.assertIsInstance(result, CommandError)
        self.assertIsNotNone(result.detail)
        self.assert_cleanup(calls)
