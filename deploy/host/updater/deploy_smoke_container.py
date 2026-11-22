from uuid import uuid4

from .deploy_io import CommandError


def run(host, args, **options):
    name = 'trachuan-updater-smoke-' + uuid4().hex
    try:
        return host.user('podman', 'run', '--name', name, '--label', 'trachuan.updater=smoke', *args, **options)
    finally:
        failed = False
        try:
            host.user('podman', 'rm', '--force', '--time', '0', name, timeout=10, cancel=True)
        except Exception:
            failed = True
        try:
            host.user('podman', 'container', 'exists', name, timeout=10, cancel=True)
            failed = True
        except CommandError as error:
            if error.returncode != 1:
                failed = True
        except Exception:
            failed = True
        if failed:
            raise CommandError('smoke container teardown failed', detail='smoke container teardown failed')
