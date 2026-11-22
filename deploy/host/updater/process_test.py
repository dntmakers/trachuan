import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from updater import deploy_io as io


class ProcessTest(unittest.TestCase):
    def setUp(self):
        io.stop.clear()
        self.addCleanup(io.stop.clear)

    def test_timeout_is_the_cause_of_the_command_error(self):
        for limit in (None, 64):
            with self.subTest(stdout_limit=limit):
                with self.assertRaises(io.CommandError) as caught:
                    io.command(['/bin/sleep', '5'], timeout=0.05, stdout_limit=limit)
                self.assertIsInstance(caught.exception.__cause__, subprocess.TimeoutExpired)

    def test_only_cosign_certificate_identity_mismatch_is_an_identity_failure(self):
        for message, expected in [
                ('none of the expected identities matched what was in the certificate', 'identity'),
                ('no matching CertificateIdentity found, last error: expected SAN value to match regex', 'identity'),
                ('certificate chain verification failed', 'failed'),
                ('failed to download attestation', 'failed')]:
            self.assertEqual(io.failure_reason(['cosign', 'verify-attestation'], message.encode()), expected)

    def test_only_explicit_oras_repository_absence_is_classified_as_missing(self):
        real_popen = subprocess.Popen
        errors = [('Error response from registry: name unknown: repository name not known', 'not_found'),
                  ('Error response from registry: repository not found', 'not_found'),
                  ('Error: GET "https://ghcr.io/v2/example/release/tags/list": response status code 404: Not Found', 'not_found'),
                  ('Error response from registry: denied: requested access is denied', 'failed'),
                  ('Error: GET "https://ghcr.io/token": response status code 404: Not Found', 'failed'),
                  ('Error: connection refused', 'failed')]
        for stderr, expected in errors:
            def spawn(args, **kwargs):
                return real_popen([sys.executable, '-c', 'import sys; sys.stderr.write(sys.argv[1]); sys.exit(1)', stderr], **kwargs)
            with self.subTest(stderr=stderr), mock.patch.object(io.subprocess, 'Popen', side_effect=spawn):
                with self.assertRaises(io.CommandError) as raised:
                    io.command(['/usr/local/libexec/trachuan/bin/oras', 'repo', 'tags', '--format', 'json', 'ghcr.io/example/release'])
                self.assertEqual(getattr(raised.exception, 'reason', None), expected)

    def test_recorded_missing_tag_is_missing_only_for_resolve(self):
        oras = '/usr/local/libexec/trachuan/bin/oras'
        stderr = (Path(__file__).with_name('fixtures') / 'oras-resolve-missing-tag.txt').read_bytes()
        self.assertEqual(io.failure_reason([oras, 'resolve', 'ghcr.io/dannyota/trachuan-corpus-release:frameworks-production'], stderr),
                         'not_found')
        self.assertEqual(io.failure_reason([oras, 'manifest', 'fetch', 'ghcr.io/example/release@sha256:' + 'a' * 64], stderr), 'failed')
        for other in (b'Error response from registry: failed to resolve digest: ghcr.io/example/release:production: unauthorized\n',
                      b'Error response from registry: failed to resolve digest: ghcr.io/example/release:production: not found: retry\n',
                      b'Error: failed to resolve digest: ghcr.io/example/release:production: toomanyrequests\n'):
            with self.subTest(other=other):
                self.assertEqual(io.failure_reason([oras, 'resolve', 'ghcr.io/example/release:production'], other), 'failed')

    def test_stream_limit_stops_the_writer_before_it_can_finish(self):
        real_popen = subprocess.Popen
        children = []
        def spawn(*args, **kwargs):
            process = real_popen(*args, **kwargs)
            children.append(process)
            return process
        source = 'import os, signal; os.write(1, b"x" * 1048576); signal.pause()'
        with mock.patch.object(io.subprocess, 'Popen', side_effect=spawn):
            with self.assertRaisesRegex(io.CommandError, 'output exceeds'):
                io.command([sys.executable, '-c', source], stdout_limit=64)
        self.assertIn(children[0].returncode, (-signal.SIGTERM, -signal.SIGKILL))
        self.assertEqual(io.command([sys.executable, '-c', 'print("ok", end="")'], stdout_limit=2), b'ok')

    def test_timeout_kills_nested_child_before_returning(self):
        processes = []
        real_popen = subprocess.Popen
        observed = []
        reader, writer = os.pipe()
        def spawn(args, **kwargs):
            observed.append(kwargs)
            process = real_popen(args, pass_fds=(writer,), **kwargs)
            os.close(writer)
            communicate = process.communicate
            def expire(input=None, timeout=None):
                if not processes:
                    processes.extend(int(value) for value in process.stdout.readline().split())
                    raise subprocess.TimeoutExpired(args, timeout)
                return communicate(input=input, timeout=timeout)
            process.communicate = expire
            return process
        source = ('import os, signal, subprocess, sys\n'
                  'child = subprocess.Popen([sys.executable, "-c", "import signal; signal.pause()"], '
                  f'pass_fds=({writer},), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)\n'
                  f'os.close({writer})\n'
                  'print(os.getpid(), child.pid, flush=True)\n'
                  'signal.pause()\n')
        try:
            with mock.patch.object(io.subprocess, 'Popen', side_effect=spawn):
                with self.assertRaises(io.CommandError):
                    io.command([sys.executable, '-c', source], timeout=0.01)
            self.assertEqual(len(processes), 2)
            ready = select.select([reader], [], [], 0.08)[0]
            self.assertTrue(ready, 'grandchild still holds its pipe after timeout')
            self.assertEqual(os.read(reader, 1), b'')
            self.assertTrue(observed[0].get('start_new_session'))
        finally:
            for pid in processes:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            os.close(reader)


    def test_image_inspect_timeout_does_not_probe_or_pull(self):
        from updater.deploy_verify import pull_image
        error = io.CommandError('timed out', reason='timeout')
        runner = mock.Mock(side_effect=error)
        with self.assertRaises(io.CommandError) as caught:
            pull_image('example@sha256:abc', runner, [])
        self.assertIs(caught.exception, error)
        self.assertEqual(runner.call_count, 1)


    def test_signal_after_spawn_kills_and_reaps_child(self):
        from updater import main
        real = subprocess.Popen
        children = []
        def spawn(*args, **kwargs):
            child = real(*args, **kwargs)
            children.append(child)
            main.interrupted(signal.SIGTERM, None)
            return child
        with mock.patch.object(io.subprocess, 'Popen', side_effect=spawn):
            with self.assertRaises(io.StopRequested):
                io.command(['/bin/sleep', '5'], timeout=2)
        self.assertIn(children[0].returncode, (-signal.SIGTERM, -signal.SIGKILL))


    def test_second_signal_during_cleanup_does_not_interrupt_result(self):
        from updater import main
        real_killpg = os.killpg
        real_popen = subprocess.Popen
        def spawn(*args, **kwargs):
            process = real_popen(*args, **kwargs)
            main.interrupted(signal.SIGTERM, None)
            return process
        def killpg(pid, sig):
            main.interrupted(signal.SIGTERM, None)
            return real_killpg(pid, sig)
        with tempfile.TemporaryDirectory() as directory:
            result = io.Result(Path(directory) / 'result', Path(directory) / 'log')
            with mock.patch.object(io.os, 'killpg', side_effect=killpg), \
                    mock.patch.object(io.subprocess, 'Popen', side_effect=spawn):
                try:
                    io.command(['/bin/sleep', '5'], timeout=2)
                except io.StopRequested:
                    result.write('failed', 'process', 'interrupted')
            self.assertEqual(json.loads((Path(directory) / 'result').read_text())['status'], 'failed')

    def test_child_ignoring_sigterm_is_killed_after_grace(self):
        from updater import main
        real = subprocess.Popen
        children = []
        def spawn(*args, **kwargs):
            child = real(*args, **kwargs)
            children.append(child)
            self.assertEqual(child.stdout.readline(), b'ready\n')
            main.interrupted(signal.SIGTERM, None)
            return child
        source = 'import signal; signal.signal(signal.SIGTERM, signal.SIG_IGN); print("ready", flush=True); signal.pause()'
        with mock.patch.object(io.subprocess, 'Popen', side_effect=spawn):
            with self.assertRaises(io.StopRequested):
                io.command([sys.executable, '-c', source], timeout=2)
        self.assertEqual(children[0].returncode, -signal.SIGKILL)


    def test_termination_does_not_reap_before_final_group_signal(self):
        events = []
        process = mock.Mock(pid=123, returncode=None)
        process.wait.side_effect = lambda: events.append('reap')
        with mock.patch.object(io.os, 'killpg', side_effect=lambda pid, sig: events.append(sig)), \
                mock.patch.object(io.os, 'waitid', side_effect=lambda *args: events.append('observe') or object()):
            io.terminate(process)
        self.assertEqual(events, [signal.SIGTERM, 'observe', signal.SIGKILL, 'reap'])


    def test_cancellation_command_is_the_only_stop_flag_bypass(self):
        stop_command = ['systemctl', '--user', 'stop', 'trachuan-caddy.service']
        real = subprocess.Popen
        io.stop.request()
        with mock.patch.object(io.subprocess, 'Popen', side_effect=lambda args, **kw: real(['/usr/bin/true'], **kw)):
            self.assertEqual(io.command(stop_command, timeout=10, cancel=True), b'')
            with self.assertRaises(ValueError):
                io.command(['systemctl', '--user', 'start', 'trachuan-caddy.service'], timeout=10, cancel=True)
            with self.assertRaises(io.StopRequested):
                io.command(stop_command, timeout=10)


if __name__ == '__main__':
    unittest.main()
