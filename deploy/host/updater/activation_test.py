import copy
import hashlib
import json
import os
import stat
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from updater import deploy_state as state_module

from updater import deploy_activate as a
from updater.deploy_io import CommandError, canonical, command
from updater.deploy_state import State
from updater.deploy_verify import policy
from updater.deploy_host import Host
from updater.deploy_store import Candidate, Paths, Store
from updater.verification_test import FIXTURES, FIXTURE_POLICY, archive


PROVISIONER = b'#!/usr/bin/env bash\nsynthetic provisioner\n'


class Crash(BaseException):
    pass


def sql_text(args, input=None):
    if input:
        return input
    return args[-1].encode() if args[-2:-1] == ['-c'] else b''


class ActivationFixture:
    def setUp(self):
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        self.paths = Paths(Path(self.work.name))
        for path in (self.paths.state, self.paths.host, self.paths.quadlet, self.paths.user_units,
                     self.paths.bundles, self.paths.result.parent, self.paths.log.parent):
            path.mkdir(parents=True, exist_ok=True)
        self.paths.host.joinpath('provision-activator.sh').write_bytes(PROVISIONER)
        self.rules = json.loads(json.dumps(FIXTURE_POLICY))
        self.store = Store(self.paths, self.rules, self.runner)
        self.schema = 7
        self.calls = []
        self.crash_step = None
        self.error_step = None
        self.live_override = {}
        self.old = self.candidate(14, b'old')
        self.new = self.candidate(15, b'new')
        self.state = State.bootstrap(self.paths.state / 'state.json', self.old.hash, 14, 7,
                                     self.old.manifest['host_bundle']['sha256'])
        self.state.accept('image', self.new.hash, 15)
        self.store.retain(self.old)
        self.store.retain(self.new)
        self.paths.postgres_unit.write_bytes(b'Image=' + self.old.manifest['images']['postgres'].encode() + b'\n')
        self.host = Host(self.paths, self.runner, user_prefix=[])
        self.engine = a.Activator(self.state, self.store, self.host)

    def candidate(self, sequence, value):
        directory = self.paths.state / str(sequence)
        directory.mkdir()
        data = archive(server=b'GOOGLE_CLIENT_ID=' + value)
        (directory / 'host-bundle.tar.gz').write_bytes(data)
        manifest = json.loads((FIXTURES / 'manifest.json').read_bytes())
        manifest['sequence'] = sequence
        manifest['host_bundle'] = {'sha256': hashlib.sha256(data).hexdigest(), 'size': len(data)}
        (directory / 'manifest.json').write_bytes(canonical(manifest))
        (directory / 'manifest.sigstore.json').write_bytes(b'{}')
        return Candidate.read(directory, self.rules, self.runner)

    def runner(self, args, input=None, **kwargs):
        self.calls.append((args, input))
        journal = json.loads((self.paths.state / 'state.json').read_bytes()).get('journal') if (self.paths.state / 'state.json').exists() else None
        step = journal['step'] if journal else None
        if step == self.crash_step and step is not None:
            self.crash_step = None
            raise Crash()
        if step == self.error_step and step is not None:
            self.error_step = None
            raise CommandError('injected failure')
        if args[:3] == ['python3', '-I', '-c']:
            return command(args, input=input)
        if 'psql' in args:
            text = sql_text(args, input)
            if b'max(version_id)' in text:
                return (str(self.schema) + '\n').encode()
            if b'active_db' in text:
                return b'corpus_vn_20990101_1\n'
            if b'datdba' in text:
                return b'activator\n'
        if args[:1] == ['curl']:
            return b'200'
        if 'show' in args:
            return b'success\n'
        if 'image' in args and 'inspect' in args:
            return canonical([{'RepoDigests': [args[-1]]}])
        if 'container' in args and 'inspect' in args:
            if '{{.State.Running}}' in args:
                return b'true\n'
            name = args[-1].replace('trachuan-', '')
            images = dict(self.old.manifest['images'], **self.old.manifest['upstream'],
                          migrate=self.old.manifest['images']['server'], **self.live_override)
            return images[name].encode() + b'\n'
        return b''


class ActivationTest(ActivationFixture, unittest.TestCase):
    def test_terminal_recovery_barrier_failure_keeps_ingress_held(self):
        for terminal in ('activated', 'rolled_back'):
            with self.subTest(terminal=terminal):
                self.setUp()
                if terminal == 'activated':
                    self.engine.activate(self.new)
                else:
                    self.crash_step = 'migrate'
                    with self.assertRaises(Crash):
                        self.engine.activate(self.new)
                    self.assertEqual(self.engine.recover(), 'rolled_back')
                self.host.hold()
                self.calls.clear()
                with mock.patch('updater.deploy_activate.sync_dir', side_effect=OSError('sync unavailable')):
                    with self.assertRaisesRegex(OSError, 'sync unavailable'):
                        self.engine.recover()
                self.assertTrue(self.paths.hold.exists())
                self.assertFalse(any(args == ['systemctl', '--user', 'start', 'trachuan-caddy.service']
                                     for args, _ in self.calls))

    def test_terminal_recovery_syncs_state_directory_before_reopening_ingress(self):
        self.engine.activate(self.new)
        self.host.hold()
        directory = os.stat(self.state.path.parent)
        original_fsync, runner = os.fsync, self.host.runner
        synced, started = [], []
        def fsync(fd):
            info = os.fstat(fd)
            if stat.S_ISDIR(info.st_mode) and (info.st_dev, info.st_ino) == (directory.st_dev, directory.st_ino):
                synced.append(True)
            return original_fsync(fd)
        def command(args, **kwargs):
            if args == ['systemctl', '--user', 'start', 'trachuan-caddy.service']:
                self.assertTrue(synced)
                started.append(True)
            return runner(args, **kwargs)
        self.host.runner = command
        with mock.patch('updater.deploy_io.os.fsync', side_effect=fsync):
            self.assertIsNone(self.engine.recover())
        self.assertTrue(started)
        self.assertFalse(self.paths.hold.exists())

    def test_resume_rejects_nonterminal_state_before_ingress(self):
        self.calls.clear()
        with self.assertRaisesRegex(ValueError, 'terminal'):
            self.engine.resume_ingress()
        self.assertFalse(any(args == ['systemctl', '--user', 'start', 'trachuan-caddy.service']
                             for args, _ in self.calls))

    def test_resume_failure_stops_caddy_when_hold_write_fails(self):
        self.engine.activate(self.new)
        with mock.patch.object(self.host, 'probe', side_effect=[None, ValueError('edge failed')]), \
                mock.patch.object(self.host, 'hold', side_effect=OSError('hold unavailable')), \
                mock.patch.object(self.host, 'cancel_edge') as cancel:
            with self.assertRaises(ValueError):
                self.engine.resume_ingress()
        cancel.assert_called_once()

    def test_preflight_rejects_oversized_final_commit_before_service_change(self):
        projected = State(self.state.path, copy.deepcopy(self.state.data))
        projected.data['retained'] = [self.old.hash]
        projected.data['journal'] = dict(target=self.new.hash, previous=self.old.hash, baseline=7,
                                         restart_postgres=False, status='activated', step='complete', authorization=None)
        projected.save = lambda: None
        projected.activate(self.new.hash, 9999999999999999999)
        limit = len(canonical(projected.data))
        self.calls.clear()
        before = self.state.path.read_bytes()
        with mock.patch.object(state_module, 'STATE_LIMIT', limit):
            with self.assertRaisesRegex(ValueError, 'state too large'):
                self.engine.activate(self.new)
        self.assertEqual(self.state.path.read_bytes(), before)
        self.assertFalse(any('systemctl' in args or 'install.sh' in ' '.join(args) for args, _ in self.calls))

    def test_recovery_discards_unsaved_activated_state(self):
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.state.data['journal'].update(status='activated', step='complete')
        self.state.data['active'] = self.new.hash
        self.assertEqual(self.engine.recover(), 'rolled_back')
        self.assertEqual(State(self.state.path).data['active'], self.old.hash)

    def test_recovery_restores_previous_postgres_before_querying_schema(self):
        self.paths.postgres_unit.write_bytes(b'changed\n')
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        restored = []
        def runner(args, input=None, **kwargs):
            if 'restart' in args and 'trachuan-postgres.service' in args:
                wanted = b'Image=' + self.old.manifest['images']['postgres'].encode() + b'\n'
                self.assertEqual(self.paths.postgres_unit.read_bytes(), wanted)
                restored.append(True)
            if b'max(version_id)' in sql_text(args, input) and not restored:
                raise CommandError('new postgres never became ready')
            return self.runner(args, input=input, **kwargs)
        self.host.runner = runner
        self.calls.clear()
        self.assertEqual(self.engine.recover(), 'rolled_back')
        self.assertTrue(restored)
        query = next(i for i, (args, data) in enumerate(self.calls) if b'max(version_id)' in sql_text(args, data))
        wait = next(i for i, (args, data) in enumerate(self.calls) if '/usr/local/bin/wait-postgres.sh' in args)
        self.assertLess(wait, query)

    def test_schema_read_failure_after_postgres_restore_remains_stopped(self):
        self.paths.postgres_unit.write_bytes(b'changed\n')
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        def runner(args, input=None, **kwargs):
            if b'max(version_id)' in sql_text(args, input):
                raise CommandError('schema still unavailable')
            return self.runner(args, input=input, **kwargs)
        self.host.runner = runner
        self.calls.clear()
        self.assertEqual(self.engine.recover(), 'stopped')
        self.assertTrue(any('restart' in args and 'trachuan-postgres.service' in args for args, _ in self.calls))
        self.assertEqual(self.state.data['journal']['baseline'], 7)

    def test_activation_order_and_postgres_stays_up_for_same_unit(self):
        self.engine.activate(self.new)
        commands = [args for args, _ in self.calls]
        stop = next(i for i, args in enumerate(commands) if args[:2] == ['systemctl', '--user'] and 'stop' in args)
        migrate = next(i for i, args in enumerate(commands) if 'start' in args and 'trachuan-migrate.service' in args)
        ready = next(i for i, args in enumerate(commands) if args[-1] == 'http://127.0.0.1:8080/readyz')
        edge = next(i for i, args in enumerate(commands) if args[-1] == 'https://trachuan.vn/')
        self.assertLess(stop, migrate)
        self.assertLess(migrate, ready)
        self.assertLess(ready, edge)
        self.assertFalse(any('restart' in args and 'trachuan-postgres.service' in args for args in commands))
        self.assertEqual(self.state.data['active'], self.new.hash)
        self.assertEqual(self.state.data['journal']['status'], 'activated')
        self.assertEqual(self.paths.host.joinpath('server.env').read_bytes(), b'GOOGLE_CLIENT_ID=new')
        self.assertNotIn(b'REPLACE', self.paths.host.joinpath('images.env').read_bytes())

    def test_changed_postgres_unit_restarts_checks_backups_and_prewarms(self):
        self.paths.postgres_unit.write_bytes(b'changed\n')
        self.engine.activate(self.new)
        commands = [args for args, _ in self.calls]
        restart = next(i for i, args in enumerate(commands) if 'restart' in args and 'trachuan-postgres.service' in args)
        wait = next(i for i, args in enumerate(commands) if '/usr/local/bin/wait-postgres.sh' in args)
        check = next(i for i, args in enumerate(commands) if 'pgbackrest' in args and 'check' in args)
        prewarm = next(i for i, (_, data) in enumerate(self.calls) if data and b'pg_prewarm' in data)
        self.assertLess(restart, wait)
        self.assertLess(wait, check)
        self.assertLess(check, prewarm)

    def test_each_activation_step_recovers_using_original_baseline_after_crash(self):
        for step in ('install', 'configure', 'stop', 'postgres', 'migrate', 'services', 'loopback', 'edge'):
            with self.subTest(step=step):
                self.setUp()
                self.paths.postgres_unit.write_bytes(b'changed\n')
                self.crash_step = step
                with self.assertRaises(Crash):
                    self.engine.activate(self.new)
                state = State(self.paths.state / 'state.json')
                self.assertEqual(state.data['journal']['baseline'], 7)
                self.calls.clear()
                engine = a.Activator(state, self.store, self.host)
                self.assertEqual(engine.recover(), 'rolled_back')
                self.assertEqual(self.calls[0][0], ['systemctl', '--user', 'stop', 'trachuan-caddy.service'])
                self.assertEqual(state.data['active'], self.old.hash)
                self.assertIn(self.new.hash, State(state.path).data['failed'])
                self.assertEqual(state.data['journal']['baseline'], 7)

    def test_schema_change_stays_stopped_and_does_not_install_previous_bundle(self):
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.schema = 8
        self.calls.clear()
        state = State(self.state.path)
        self.assertEqual(a.Activator(state, self.store, self.host).recover(), 'stopped')
        self.assertEqual(state.data['journal']['baseline'], 7)
        self.assertIn(self.new.hash, state.data['failed'])
        self.assertFalse(any('install.sh' in ' '.join(args) for args, _ in self.calls))
        self.assertFalse(any('start' in args for args, _ in self.calls))

    def test_recovery_stops_inflight_migrate_before_reading_schema(self):
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.calls.clear()
        self.engine.recover()
        commands = [args for args, _ in self.calls]
        stop = next(i for i, args in enumerate(commands) if 'stop' in args and 'trachuan-migrate.service' in args)
        schema = next(i for i, (args, data) in enumerate(self.calls) if b'max(version_id)' in sql_text(args, data))
        self.assertLess(stop, schema)

    def test_rollback_rechecks_authorized_schema_before_journaling(self):
        self.schema = 8
        before = self.state.path.read_bytes()
        with self.assertRaisesRegex(ValueError, 'schema changed'):
            self.engine.activate(self.new, expected_schema=7)
        self.assertEqual(self.state.path.read_bytes(), before)

    def test_recovery_retries_crashed_rollback_with_original_baseline(self):
        self.crash_step = 'migrate'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.crash_step = 'rollback:services'
        with self.assertRaises(Crash):
            self.engine.recover()
        state = State(self.state.path)
        self.assertEqual(state.data['journal']['baseline'], 7)
        self.assertEqual(a.Activator(state, self.store, self.host).recover(), 'rolled_back')
        old_install = str(self.paths.bundles / self.old.manifest['host_bundle']['sha256'] / 'install.sh')
        self.assertTrue(any(old_install in args for args, _ in self.calls))

    def test_failure_during_rollback_keeps_journaled_baseline_and_caddy_stopped(self):
        self.crash_step = 'edge'
        with self.assertRaises(Crash):
            self.engine.activate(self.new)
        self.error_step = 'rollback:migrate'
        self.assertEqual(self.engine.recover(), 'stopped')
        self.assertEqual(self.state.data['journal']['baseline'], 7)
        self.assertEqual(self.calls[-1][0], ['systemctl', '--user', 'stop', 'trachuan-caddy.service'])

    def test_prune_rejects_wrong_retained_manifest_shape(self):
        path = self.store.directory / self.old.hash / 'manifest.json'
        for value in (None, [], {'host_bundle': None}, {'host_bundle': {'sha256': []}}):
            with self.subTest(value=value):
                path.write_bytes(canonical(value))
                with self.assertRaises(ValueError):
                    self.store.prune(self.state)

    def test_retention_keeps_five_and_never_discards_sequence_bindings(self):
        for sequence in range(16, 23):
            candidate = self.candidate(sequence, str(sequence).encode())
            self.store.retain(candidate)
            self.state.accept('image', candidate.hash, sequence)
            self.state.activate(candidate.hash, 7)
            self.store.prune(self.state)
        self.assertEqual(len(list(self.store.directory.iterdir())), 5)
        self.assertEqual(len(self.state.data['accepted']['image']), 9)
        self.assertEqual(len(self.state.data['retained']), 4)

    def test_materialized_bundle_images_env_comes_only_from_manifest(self):
        self.new.files['images.env'] = b'SERVER_IMAGE=REPLACE\n'
        directory = self.store.unpack(self.new)
        pins = (directory / 'images.env').read_bytes()
        self.assertNotIn(b'REPLACE', pins)
        self.assertEqual(len(pins.splitlines()), 5)
        for name, reference in dict(self.new.manifest['images'], **self.new.manifest['upstream']).items():
            self.assertIn((name.upper() + '_IMAGE=' + reference).encode(), pins.splitlines())


if __name__ == '__main__':
    unittest.main()
