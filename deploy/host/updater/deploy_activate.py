import copy

from . import deploy_state
from .deploy_floor import bundle_floor
from .deploy_io import canonical, check_stop, interrupted


class Activator:
    def __init__(self, state, store, host):
        self.state, self.store, self.host = state, store, host
        self.recovery_message = 'interrupted activation recovered'

    def secure_ingress(self):
        failure = None
        for action in (self.host.hold, self.host.cancel_edge):
            try:
                action()
            except Exception as error:
                failure = failure or error
        if failure is not None:
            raise failure

    def step(self, name):
        check_stop()
        self.state.data['journal']['step'] = name
        self.state.save()

    def housekeeping(self):
        for step, action in (('hold', self.host.clear_hold),
                             ('prune', lambda: self.store.prune(self.state))):
            try:
                action()
            except Exception as error:
                if interrupted(error):
                    raise
                try:
                    self.host.warning(step, 'post-activation housekeeping failed; retry later')
                except OSError:
                    pass

    def resume_ingress(self):
        try:
            self.host.probe()
            self.host.clear_hold()
            self.host.systemctl('start', 'caddy')
            self.host.probe(edge=True)
        except BaseException:
            try:
                self.secure_ingress()
            except Exception:
                pass
            raise

    def apply(self, candidate, restart, prefix=''):
        self.step(prefix + 'install')
        bundle_hash = candidate.manifest['host_bundle']['sha256']
        if self.state.data['installed_bundle'] != bundle_hash:
            self.state.data['installed_bundle'] = None
            self.state.save()
            self.host.install(self.store.unpack(candidate))
            self.state.data['installed_bundle'] = bundle_hash
            self.state.save()
        self.step(prefix + 'configure')
        self.host.configure(candidate)
        self.step(prefix + 'stop')
        self.host.systemctl('stop', 'caddy', 'web', 'server', 'migrate', 'queryembed')
        self.step(prefix + 'postgres')
        if restart:
            self.host.postgres()
        self.step(prefix + 'migrate')
        self.host.migrate()
        self.step(prefix + 'services')
        self.host.systemctl('start', 'server', 'web')
        self.step(prefix + 'loopback')
        self.host.probe()
        self.step(prefix + 'edge')
        self.host.clear_hold()
        self.host.systemctl('start', 'caddy')
        self.host.probe(edge=True)

    def preflight(self, candidate, baseline, restart, authorization, rollback):
        projected = deploy_state.State(self.state.path, copy.deepcopy(self.state.data))
        previous = projected.data['active']
        projected.data['retained'] = [previous] + [h for h in projected.data['retained'] if h != previous]
        projected.data['journal'] = dict(target=candidate.hash, previous=previous, baseline=baseline,
                                         restart_postgres=restart, status='started', step='started',
                                         authorization=authorization)
        projected.save = lambda: None
        if authorization:
            projected.authorize(authorization, rollback, baseline)
        projected.data['installed_bundle'] = candidate.manifest['host_bundle']['sha256']
        projected.data['journal'].update(status='activated', step='complete')
        projected.activate(candidate.hash, 9999999999999999999, authorization=authorization,
                           release_floor=bundle_floor(candidate.files))
        projected.validate()
        if len(canonical(projected.data)) + 1 > deploy_state.STATE_LIMIT:
            raise ValueError('state too large')

    def activate(self, candidate, expected_schema=None, authorization=None, rollback=None):
        self.state.check_floor(candidate.manifest['sequence'])
        previous = self.state.data['active']
        baseline = self.host.schema()
        if expected_schema is not None and baseline != expected_schema:
            raise ValueError('rollback schema changed before activation')
        restart = self.host.postgres_changed(candidate)
        self.preflight(candidate, baseline, restart, authorization, rollback)
        self.store.retain(candidate)
        self.state.data['retained'] = [previous] + [h for h in self.state.data['retained'] if h != previous]
        self.state.data['journal'] = dict(target=candidate.hash, previous=previous, baseline=baseline,
                                           restart_postgres=restart, status='started', step='started',
                                           authorization=authorization)
        if authorization:
            self.state.authorize(authorization, rollback, baseline)
        else:
            self.state.save()
        try:
            self.apply(candidate, restart)
            schema = self.host.schema()
            self.state.data['journal'].update(status='activated', step='complete')
            self.state.activate(candidate.hash, schema, authorization=authorization,
                                release_floor=bundle_floor(candidate.files))
        except BaseException:
            try:
                self.secure_ingress()
            except Exception:
                pass
            raise
        self.housekeeping()

    def enforce_stopped(self):
        journal = self.state.data['journal']
        if journal and journal['status'] == 'stopped':
            self.secure_ingress()
            return True
        return False

    def recover(self):
        self.state.data = deploy_state.State(self.state.path).data
        journal = self.state.data['journal']
        if self.enforce_stopped():
            return None
        if journal and journal['status'] in ('activated', 'rolled_back') and self.host.paths.hold.exists():
            self.resume_ingress()
            return None
        if not journal or journal['status'] not in ('started', 'recovering'):
            return None
        self.secure_ingress()
        self.state.mark_failed(journal['target'])
        journal['status'] = 'recovering'
        self.state.save()
        try:
            self.host.systemctl('stop', 'web', 'server', 'migrate', 'queryembed')
            if journal['previous'] not in self.state.data['retained']:
                raise ValueError('previous manifest not retained')
            self.state.check_floor(self.state.image_sequence(journal['previous']))
            candidate = self.store.load(journal['previous'])
            self.state.check_floor(candidate.manifest['sequence'])
            if journal['restart_postgres']:
                self.host.restore_postgres(candidate)
            if self.host.schema() != journal['baseline']:
                raise ValueError('recovery schema changed or previous manifest not retained')
            restart = journal['restart_postgres'] or self.host.postgres_changed(candidate)
            self.apply(candidate, restart, 'rollback:')
            journal.update(status='rolled_back', step='complete')
            self.state.activate(candidate.hash, journal['baseline'])
        except BaseException as error:
            try:
                self.secure_ingress()
            except Exception:
                pass
            if interrupted(error) or not isinstance(error, Exception):
                raise
            journal.update(status='stopped', step='recovery')
            self.state.save()
            if isinstance(error, ValueError):
                self.recovery_message = str(error)
            return 'stopped'
        self.housekeeping()
        return 'rolled_back'
