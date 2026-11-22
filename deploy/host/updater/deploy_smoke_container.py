import re
import sys
from uuid import uuid4

from .deploy_io import CommandError


def candidates(host, label):
    output = host.user('podman', 'ps', '-a', '--no-trunc', '--filter', 'label=' + label,
                       '--format', '{{.ID}}', timeout=10, cancel=True, stdout_limit=65)
    if output == b'':
        return None
    if re.fullmatch(rb'[0-9a-f]{64}\n', output):
        return output[:-1].decode('ascii')
    raise CommandError('invalid smoke container listing')


def run(host, args, **options):
    token = uuid4().hex
    name = 'trachuan-updater-smoke-' + token
    label = 'trachuan.updater=' + token
    try:
        return host.user('podman', 'run', '--name', name, '--label', label, *args, **options)
    finally:
        failures = []
        original = sys.exc_info()[1]
        try:
            container = candidates(host, label)
            if container is not None:
                try:
                    host.user('podman', 'rm', '--force', '--time', '0', container, timeout=10, cancel=True)
                except Exception as error:
                    failures.append(error)
                if candidates(host, label) is not None:
                    failures.append(None)
        except Exception as error:
            failures.append(error)
        if failures:
            detail = 'smoke container teardown failed'
            if any(isinstance(error, CommandError) and error.detail == 'process did not exit'
                   for error in [original, *failures]):
                detail += ': process did not exit'
            raise CommandError(detail, detail=detail)
