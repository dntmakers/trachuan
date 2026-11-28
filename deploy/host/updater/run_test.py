from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
import stat
from pathlib import Path
import unittest
from unittest import mock

from updater import deploy_run as r
from updater import deploy_registry
from updater import deploy_state
from updater import activation_test as fixture
from updater.activation_test import Crash
from updater.deploy_io import canonical
from updater.deploy_io import CommandError
from updater.deploy_state import State
from updater.deploy_host import pins
from updater.io_test import METRICS
from watch import observations, signals


class RunTest(fixture.ActivationFixture, unittest.TestCase):
    def setUp(self):
        super().setUp()
        self.paths.lock.parent.mkdir(parents=True, exist_ok=True)
        self.paths.backup_lock.parent.mkdir(parents=True, exist_ok=True)
        self.paths.backup_lock.touch()
        self.paths.mode.parent.mkdir(parents=True, exist_ok=True)
        self.paths.mode.write_text('verify\n')
        self.paths.policy.write_bytes(canonical(self.rules))
        self.registry = mock.Mock()
        self.registry.fetch.return_value = self.new.directory
        self.registry.latest_rollback.return_value = None
        self.app = r.Updater(self.paths, self.runner, user_prefix=[])
        self.app.phases = ('secrets', 'images')
        self.app.registry = self.registry
        self.verify_images = mock.patch('updater.deploy_run.Verifier.images')
        self.images = self.verify_images.start()
        self.addCleanup(self.verify_images.stop)
        secret_phase = mock.patch('updater.deploy_run.Secrets.run', return_value=True)
        secret_phase.start()
        self.addCleanup(secret_phase.stop)

    def test_post_rename_sync_failure_never_reopens_ingress_without_durable_barrier(self):
        self.paths.mode.write_text('activate\n')
        original_fsync = os.fsync
        state_dir = os.stat(self.state.path.parent)
        failed, later_starts = [], []
        runner = self.app.host.runner
        def command(args, **kwargs):
            if failed and args == ['systemctl', '--user', 'start', 'trachuan-caddy.service']:
                later_starts.append(args)
            return runner(args, **kwargs)
        def fsync(fd):
            info = os.fstat(fd)
            if (stat.S_ISDIR(info.st_mode) and (info.st_dev, info.st_ino) == (state_dir.st_dev, state_dir.st_ino)
                    and json.loads(self.state.path.read_bytes())['journal'] is not None
                    and json.loads(self.state.path.read_bytes())['journal']['status'] == 'activated'):
                failed.append(True)
                raise OSError('state directory sync failed')
            return original_fsync(fd)
        self.app.host.runner = command
        with mock.patch('updater.deploy_io.os.fsync', side_effect=fsync):
            self.assertEqual(self.app.run(), 1)
        self.assertGreaterEqual(len(failed), 2)
        self.assertTrue(self.paths.hold.exists())
        self.assertEqual(later_starts, [])
        self.assertEqual(State(self.state.path).data['journal']['status'], 'activated')
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'failed')

    def test_interruption_stops_caddy_even_when_hold_raises(self):
        self.app.host.unverified_ingress = True
        with mock.patch.object(self.app.host, 'hold', side_effect=ValueError('hold failed')), \
                mock.patch.object(self.app.host, 'cancel_edge') as cancel:
            self.assertEqual(self.app.failure(mock.Mock(), None, r.StopRequested('stopped')), 1)
        cancel.assert_called_once()

    def test_final_activation_save_failure_recovers_from_persisted_journal(self):
        self.paths.mode.write_text('activate\n')
        original = State.save
        failed = []
        def save(state):
            journal = state.data['journal'] or {}
            if journal.get('status') == 'activated' and state.data['active'] == self.new.hash and not failed:
                failed.append(True)
                raise ValueError('state too large')
            return original(state)
        with mock.patch.object(State, 'save', save):
            self.assertEqual(self.app.run(), 1)
        self.assertTrue(failed)
        state = State(self.state.path)
        self.assertEqual(state.data['active'], self.old.hash)
        self.assertEqual(state.data['journal']['status'], 'rolled_back')

    def test_status_rejects_wrong_result_shape(self):
        self.paths.result.write_bytes(b'[]')
        with self.assertRaisesRegex(ValueError, 'invalid result'):
            self.app.status()

    def test_oversized_success_receipt_stays_failed_through_handler(self):
        self.paths.result.write_bytes(canonical({'last_success_at': '2026-01-01T00:00:00Z'}))
        self.state.data['release_floor'] = 10
        self.state.save()
        with mock.patch('updater.deploy_result.RESULT_LIMIT', 256):
            self.assertEqual(self.app.run(), 1)
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual((result['status'], result['step'], result['message']),
                         ('failed', 'result', 'result too large'))
        observations.result(self.paths.result, 'deploy')
        metrics = METRICS.deploy(self.paths.result, datetime(2026, 1, 2, tzinfo=timezone.utc))
        self.assertIn(('deploy_release_floor', 10), metrics)
        self.assertIn(('deploy_last_success_age_seconds', 86400), metrics)

    def test_oversized_failure_receipt_does_not_escape_failure_handler(self):
        self.images.side_effect = ValueError('x' * (1024 * 1024))
        self.assertEqual(self.app.run(), 1)
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual((result['status'], result['step'], result['message']),
                         ('failed', 'result', 'result too large'))

    def test_bad_registry_shape_records_failure(self):
        def registry_command(args, **kwargs):
            return b'sha256:' + b'a' * 64 if 'resolve' in args else b'null'
        self.app.registry = deploy_registry.Registry(self.rules, registry_command)
        self.assertEqual(self.app.run(), 1)
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'failed')

    def test_state_size_failure_records_result_and_preserves_state(self):
        previous = self.state.path.read_bytes()
        manifest = dict(self.new.manifest, sequence=16)
        (self.new.directory / 'manifest.json').write_bytes(canonical(manifest))
        with mock.patch.object(deploy_state, 'STATE_LIMIT', len(previous), create=True):
            self.assertEqual(self.app.run(), 1)
        self.assertEqual(self.state.path.read_bytes(), previous)
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'failed')
        self.assertIn('state too large', json.loads(self.paths.result.read_bytes())['message'])

    def test_bad_manifest_records_failure(self):
        for data, message in ((b'[' * 100000 + b']' * 100000, 'invalid release'),
                              (b'\xff', 'invalid release manifest JSON'),
                              (b' ' * (1024 * 1024 + 1), 'release manifest too large')):
            with self.subTest(size=len(data)):
                (self.new.directory / 'manifest.json').write_bytes(data)
                self.assertEqual(self.app.run(), 1)
                result = json.loads(self.paths.result.read_bytes())
                self.assertEqual(result['status'], 'failed')
                self.assertEqual(result['step'], 'manifest')
                self.assertIn(message, result['message'])

    def test_verify_keeps_pending_without_activation_or_install(self):
        self.assertEqual(self.app.run(), 0)
        state = State(self.state.path)
        self.assertEqual(state.data['active'], self.old.hash)
        self.assertEqual(state.decision('image', self.new.hash, 15), 'pending')
        self.assertIsNone(state.data['journal'])
        self.assertFalse(any('systemctl' in args or 'install.sh' in ' '.join(args) for args, _ in self.calls))
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'verified')

    def test_activate_completes_pending_and_seen_manifest_is_noop(self):
        self.paths.mode.write_text('activate\n')
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(State(self.state.path).data['active'], self.new.hash)
        before = self.state.path.read_bytes()
        self.calls.clear()
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(self.state.path.read_bytes(), before)
        self.assertFalse(any('systemctl' in args or 'pull' in args for args, _ in self.calls))

    def test_failed_activation_recovers_and_noop_does_not_clear_alarm(self):
        self.paths.mode.write_text('activate\n')
        self.error_step = 'services'
        self.assertEqual(self.app.run(), 1)
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'rolled_back')
        self.assertEqual(State(self.state.path).data['active'], self.old.hash)
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'rolled_back')

    def test_both_locks_end_run_without_state_result_or_commands(self):
        for path in (self.paths.lock, self.paths.backup_lock):
            with self.subTest(path=path), path.open('a') as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                before = self.state.path.read_bytes()
                self.calls.clear()
                self.assertEqual(self.app.run(), 0)
                self.assertEqual(self.state.path.read_bytes(), before)
                self.assertFalse(self.paths.result.exists())
                self.assertFalse(self.paths.log.exists())
                self.assertEqual(self.calls, [])

    def test_lost_state_records_failure_without_reset_or_host_command(self):
        self.state.path.unlink()
        self.calls.clear()
        self.assertEqual(self.app.run(), 1)
        self.assertFalse(self.state.path.exists())
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'failed')
        self.assertEqual(self.calls, [])

    def test_open_journal_recovers_before_any_registry_fetch(self):
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.registry.reset_mock()
        self.assertEqual(self.app.run(), 1)
        self.registry.fetch.assert_not_called()
        self.assertEqual(State(self.state.path).data['journal']['status'], 'rolled_back')

    def test_bootstrap_requires_verified_matching_installed_bundle_and_sets_verify_mode(self):
        self.state.path.unlink()
        self.paths.mode.unlink()
        self.registry.find.return_value = self.old.directory
        self.paths.host.joinpath('images.env').write_bytes(pins(self.old.manifest))
        for name in ('server.env', 'web.env'):
            self.paths.host.joinpath(name).write_bytes(self.old.files[name])
        self.assertEqual(self.app.bootstrap(self.old.hash), 0)
        self.assertEqual(State(self.state.path).data['active'], self.old.hash)
        self.assertEqual(self.paths.mode.read_text(), 'verify\n')
        self.assertEqual(self.paths.mode.stat().st_mode & 0o777, 0o644)

    def test_bootstrap_cleanup_failure_preserves_state_mode_and_timer(self):
        self.paths.mode.write_text('activate\n')
        before = self.state.path.read_bytes()
        self.registry.find.return_value = self.old.directory
        self.paths.host.joinpath('images.env').write_bytes(pins(self.old.manifest))
        for name in ('server.env', 'web.env'):
            self.paths.host.joinpath(name).write_bytes(self.old.files[name])
        self.calls.clear()
        with mock.patch('updater.deploy_run.FRAMEWORKS.discard', side_effect=OSError('synthetic')):
            self.assertEqual(self.app.bootstrap(self.old.hash), 1)
        self.assertEqual(self.state.path.read_bytes(), before)
        self.assertEqual(self.paths.mode.read_text(), 'activate\n')
        self.assertFalse(any('systemctl' in args for args, _ in self.calls))
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual((result['status'], result['step']), ('failed', 'cleanup'))

    def test_bootstrap_cleanup_failure_preserves_release_floor(self):
        for state_case, expected in (('valid', 15), ('lower', 9), ('missing', 12), ('invalid', 12)):
            with self.subTest(state=state_case):
                if state_case in ('valid', 'lower'):
                    self.state.data['release_floor'] = 15 if state_case == 'valid' else 9
                    self.state.save()
                elif state_case == 'missing':
                    self.state.path.unlink()
                else:
                    self.state.path.write_bytes(b'invalid')
                self.paths.result.write_bytes(canonical({'release_floor': 12}))
                with mock.patch('updater.deploy_run.FRAMEWORKS.discard', side_effect=OSError('synthetic')):
                    self.assertEqual(self.app.bootstrap(self.old.hash), 1)
                result = json.loads(self.paths.result.read_bytes())
                self.assertEqual((result['status'], result['step'], result['release_floor']),
                                 ('failed', 'cleanup', expected))
                metrics = METRICS.deploy(self.paths.result, datetime(2026, 1, 2, tzinfo=timezone.utc))
                self.assertIn(('deploy_release_floor', expected), metrics)

    def test_success_reports_enforced_floor_not_prior_receipt(self):
        self.state.data['release_floor'] = 9
        self.state.save()
        self.paths.result.write_bytes(canonical({'release_floor': 12}))
        self.assertEqual(self.app.run(), 0)
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual((result['status'], result['release_floor']), ('verified', 9))
        metrics = METRICS.deploy(self.paths.result, datetime(2026, 1, 2, tzinfo=timezone.utc))
        self.assertIn(('deploy_release_floor', 9), metrics)

    def test_failure_reports_enforced_floor_not_prior_receipt(self):
        self.state.data['release_floor'] = 9
        self.state.save()
        self.paths.result.write_bytes(canonical({'release_floor': 12}))
        self.images.side_effect = ValueError('synthetic failure')
        self.assertEqual(self.app.run(), 1)
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual((result['status'], result['release_floor']), ('failed', 9))
        metrics = METRICS.deploy(self.paths.result, datetime(2026, 1, 2, tzinfo=timezone.utc))
        self.assertIn(('deploy_release_floor', 9), metrics)

    def rollback_artifact(self, current, target, sequence=1):
        directory = self.paths.state / 'rollback-fixture'
        directory.mkdir(exist_ok=True)
        data = canonical(dict(kind='trachuan.rollback', version=1, current=current,
                              target=target, sequence=sequence))
        (directory / 'manifest.json').write_bytes(data)
        (directory / 'manifest.sigstore.json').write_bytes(b'{}')
        self.registry.latest_rollback.return_value = str(sequence)
        self.registry.fetch.side_effect = lambda repository, tag, path, kind: directory if kind == 'rollback' else self.new.directory
        return hashlib.sha256(data).hexdigest()

    def test_bad_rollback_manifest_records_failure(self):
        self.rollback_artifact(self.new.hash, self.old.hash)
        path = self.paths.state / 'rollback-fixture' / 'manifest.json'
        path.write_bytes(b'\xff')
        self.assertEqual(self.app.run(), 1)
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual(result['status'], 'failed')
        self.assertIn('invalid rollback manifest JSON', result['message'])

    def test_authorized_rollback_is_consumed_before_activation_and_not_replayed_by_timer(self):
        self.paths.mode.write_text('activate\n')
        self.assertEqual(self.app.run(), 0)
        authorization = self.rollback_artifact(self.new.hash, self.old.hash)
        self.assertEqual(self.app.run(), 0)
        state = State(self.state.path)
        self.assertEqual(state.data['active'], self.old.hash)
        self.assertIn(authorization, state.data['consumed'])
        self.assertEqual(state.data['journal']['authorization'], authorization)
        before = state.path.read_bytes()
        self.calls.clear()
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(state.path.read_bytes(), before)
        self.assertFalse(any('systemctl' in args or 'psql' in args for args, _ in self.calls))

    def test_authorized_rollback_result_tells_watch_the_activated_image_identity(self):
        def observe(state, now):
            data = observations.collect(self.paths.result.parent.parent,
                                        activity=lambda: 'active', get=lambda *args: (503, {}),
                                        disk=lambda path: 20, mem=lambda: 50)
            signals.observe(state, data, now)
        self.paths.mode.write_text('activate\n')
        self.assertEqual(self.app.run(), 0)
        state = signals.fresh()
        observe(state, 100000)
        self.assertEqual(signals.due(state, 100000), [])
        authorization = self.rollback_artifact(self.new.hash, self.old.hash)
        self.assertEqual(self.app.run(), 0)
        observe(state, 100060)
        pending = signals.due(state, 100060)
        self.assertEqual([(item['key'], item.get('identity')) for item in pending],
                         [('release_live', ['v0.1.1', 14])])
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual(result['status'], 'ok')
        self.assertEqual(result['manifest_hash'], authorization)
        self.assertEqual((result['release'], result['sequence']), ('v0.1.1', 14))
        self.assertEqual(State(self.state.path).data['active'], self.old.hash)
        self.assertEqual(self.app.run(), 0)
        observe(state, 100120)
        self.assertEqual(signals.due(state, 100120), pending)
        signals.accept(state, pending[0], 100120)
        self.registry.fetch.side_effect = None
        self.registry.fetch.return_value = self.old.directory
        self.registry.latest_rollback.return_value = None
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'current')
        observe(state, 100180)
        self.assertEqual(signals.due(state, 100180), [])

    def test_schema_change_refuses_signed_rollback_without_consuming_authorization(self):
        self.paths.mode.write_text('activate\n')
        self.assertEqual(self.app.run(), 0)
        authorization = self.rollback_artifact(self.new.hash, self.old.hash)
        self.schema = 8
        before = self.state.path.read_bytes()
        self.calls.clear()
        self.assertEqual(self.app.run(), 1)
        self.assertEqual(self.state.path.read_bytes(), before)
        self.assertNotIn(authorization, State(self.state.path).data['consumed'])
        self.assertFalse(any('systemctl' in args or 'install.sh' in ' '.join(args) for args, _ in self.calls))

    def test_failed_image_verification_keeps_pending_binding_and_does_not_start_journal(self):
        self.images.side_effect = ValueError('wrong image digest')
        self.assertEqual(self.app.run(), 1)
        state = State(self.state.path)
        self.assertEqual(state.data['active'], self.old.hash)
        self.assertIsNone(state.data['journal'])
        self.assertEqual(state.decision('image', self.new.hash, 15), 'pending')
        self.assertFalse(any('systemctl' in args for args, _ in self.calls))

    def test_caddy_stop_failure_records_failure_and_leaves_open_journal(self):
        self.crash_step = 'edge'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.error_step = 'edge'
        self.assertEqual(self.app.run(), 1)
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'failed')
        self.assertEqual(State(self.state.path).data['journal']['status'], 'started')

    def test_bootstrap_lock_contention_returns_failure_without_changing_state(self):
        before = self.state.path.read_bytes()
        with self.paths.backup_lock.open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.assertEqual(self.app.bootstrap(self.old.hash), 1)
        self.assertEqual(self.state.path.read_bytes(), before)
        self.assertFalse(self.paths.result.exists())

    def test_transient_fetch_alarm_clears_after_healthy_seen_manifest(self):
        self.registry.fetch.side_effect = CommandError('synthetic unavailable registry')
        self.assertEqual(self.app.run(), 1)
        self.registry.fetch.side_effect = None
        self.registry.fetch.return_value = self.old.directory
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'current')
        self.assertIsNotNone(json.loads(self.paths.result.read_bytes())['last_success_at'])

    def test_command_failure_names_command_and_stderr_tail_in_result(self):
        self.registry.fetch.side_effect = CommandError('command failed', detail='oras resolve: exit 1: Error: not found')
        self.assertEqual(self.app.run(), 1)
        result = json.loads(self.paths.result.read_bytes())
        self.assertEqual(result['step'], 'fetch')
        self.assertEqual(result['message'], 'command failed: oras resolve: exit 1: Error: not found')

    def test_unreadable_rollback_channel_logs_warning_and_still_verifies(self):
        self.registry.rollback_unreadable = True
        self.assertEqual(self.app.run(), 0)
        lines = [json.loads(line) for line in self.paths.log.read_text().splitlines()]
        self.assertIn('rollback channel unreadable; rollback authorizations withheld',
                      [line['message'] for line in lines if line['status'] == 'warning'])
        self.registry.latest_rollback.assert_called_with(False)

    def test_bootstrap_refuses_matching_pins_with_stale_running_image(self):
        self.state.path.unlink()
        self.registry.find.return_value = self.old.directory
        self.paths.host.joinpath('images.env').write_bytes(pins(self.old.manifest))
        for name in ('server.env', 'web.env'):
            self.paths.host.joinpath(name).write_bytes(self.old.files[name])
        self.live_override['server'] = 'ghcr.io/dannyota/trachuan-server@sha256:' + '0' * 64
        self.assertEqual(self.app.bootstrap(self.old.hash), 1)
        self.assertFalse(self.state.path.exists())

    def test_authorization_for_current_active_manifest_runs_before_new_production_release(self):
        self.paths.mode.write_text('activate\n')
        previous = self.candidate(13, b'previous')
        self.store.retain(previous)
        state = State.bootstrap(self.state.path, previous.hash, 13, 7,
                                self.old.manifest['host_bundle']['sha256'])
        state.accept('image', self.old.hash, 14)
        state.activate(self.old.hash, 7)
        self.rollback_artifact(self.old.hash, previous.hash)
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(State(self.state.path).data['active'], previous.hash)
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(State(self.state.path).data['active'], self.new.hash)

    def test_unconfirmed_backup_marker_skips_run_and_bootstrap_without_host_changes(self):
        marker = self.paths.backup_lock.with_name('active-job')
        marker.write_bytes(b'synthetic unconfirmed backup')
        before = self.state.path.read_bytes()
        for action, expected in ((self.app.run, 0), (lambda: self.app.bootstrap(self.old.hash), 1)):
            self.calls.clear()
            self.registry.reset_mock()
            self.assertEqual(action(), expected)
            self.assertEqual(self.state.path.read_bytes(), before)
            self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'skipped')
            self.registry.fetch.assert_not_called()
            self.registry.find.assert_not_called()
            self.assertEqual(self.calls, [])

    def test_crash_when_rollback_consumption_is_saved_also_has_activation_journal(self):
        self.paths.mode.write_text('activate\n')
        self.assertEqual(self.app.run(), 0)
        authorization = self.rollback_artifact(self.new.hash, self.old.hash)
        save = State.save
        def crash(state):
            save(state)
            if authorization in state.data['consumed']:
                raise Crash()
        with mock.patch.object(State, 'save', crash), self.assertRaises(Crash):
            self.app.run()
        state = State(self.state.path)
        self.assertIn(authorization, state.data['consumed'])
        self.assertEqual(state.data['journal']['authorization'], authorization)
        self.assertEqual(state.data['journal']['target'], self.old.hash)
        self.assertEqual(state.data['journal']['status'], 'started')
        self.assertEqual(self.app.run(), 1)
        self.assertEqual(State(self.state.path).data['journal']['status'], 'rolled_back')

    def test_stopped_run_reasserts_hold_and_stops_caddy_even_when_backup_is_pending(self):
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.schema = 8
        self.assertEqual(self.engine.recover(), 'stopped')
        self.paths.hold.unlink()
        self.paths.backup_lock.with_name('active-job').touch()
        self.calls.clear()
        self.assertEqual(self.app.run(), 0)
        self.assertTrue(self.paths.hold.exists())
        self.assertEqual([args for args, _ in self.calls],
                         [['systemctl', '--user', 'stop', 'trachuan-caddy.service']])
        self.assertEqual(json.loads(self.paths.result.read_bytes())['status'], 'skipped')

    def test_higher_release_can_activate_after_stopped_journal(self):
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.schema = 8
        self.assertEqual(self.engine.recover(), 'stopped')
        fixed = self.candidate(16, b'fixed')
        self.registry.fetch.return_value = fixed.directory
        self.paths.mode.write_text('activate\n')
        self.assertEqual(self.app.run(), 0)
        self.assertEqual(State(self.state.path).data['active'], fixed.hash)
        self.assertFalse(self.paths.hold.exists())

    def test_pending_retired_secret_counts_as_pending(self):
        (self.paths.state / 'secrets-state.json').write_text(json.dumps(dict(
            versions={}, pending={'server_signup_emails': 4}, last_success_at=None, held=False)))
        self.assertTrue(self.app.secret_pending())


if __name__ == '__main__':
    unittest.main()
