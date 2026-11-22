import re
import sys
import time
from uuid import uuid4

from .deploy_io import CommandError


def cleanup_command(host, deadline, *args, **options):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise CommandError('smoke container cleanup deadline exceeded')
    result = host.user(*args, timeout=remaining, cancel=True, **options)
    if time.monotonic() >= deadline:
        raise CommandError('smoke container cleanup deadline exceeded')
    return result


def candidates(host, label, deadline):
    output = cleanup_command(host, deadline, 'podman', 'ps', '-a', '--no-trunc', '--filter', 'label=' + label,
                             '--format', '{{.ID}}', stdout_limit=65)
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
        deadline = time.monotonic() + 10
        try:
            container = candidates(host, label, deadline)
            if container is not None:
                try:
                    cleanup_command(host, deadline, 'podman', 'rm', '--force', '--time', '0', container)
                except Exception as error:
                    failures.append(error)
                if candidates(host, label, deadline) is not None:
                    failures.append(None)
        except Exception as error:
            failures.append(error)
        if failures:
            detail = 'smoke container teardown failed'
            if any(isinstance(error, CommandError) and error.detail == 'process did not exit'
                   for error in [original, *failures]):
                detail += ': process did not exit'
            raise CommandError(detail, detail=detail)
