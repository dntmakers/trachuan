from datetime import datetime, timezone
from contextlib import ExitStack
import fcntl
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import signal
import selectors
import time
import tempfile


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()


def decode(data, what):
    try:
        return json.loads(data)
    except (ValueError, RecursionError) as error:
        raise ValueError('invalid ' + what + ' JSON') from error


def atomic(path, data, mode=0o600):
    path = Path(path)
    descriptor, name = tempfile.mkstemp(prefix='.' + path.name + '.', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'wb') as output:
            os.fchmod(output.fileno(), mode)
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(name, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        Path(name).unlink(missing_ok=True)


def now():
    return datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')


def remove(path):
    path = Path(path)
    try:
        path.unlink()
    except FileNotFoundError:
        return
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


class Busy(Exception):
    pass


class Lock:
    def __init__(self, path, create=True):
        self.path, self.create = path, create

    def __enter__(self):
        descriptor = os.open(self.path, (os.O_CREAT if self.create else 0) | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        self.file = os.fdopen(descriptor, 'r+')
        try:
            fcntl.flock(self.file, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            self.file.close()
            raise Busy('lock busy') from error
        return self

    def __exit__(self, *args):
        self.file.close()


class StopRequested(Exception):
    pass


class StopState:
    def __init__(self):
        self.flag = False

    def request(self):
        self.flag = True

    def requested(self):
        return self.flag

    def clear(self):
        self.flag = False


stop = StopState()


def check_stop():
    if stop.requested():
        raise StopRequested('updater interrupted')


class CommandError(Exception):
    def __init__(self, message, reason='failed', returncode=None, detail=None, code=None, safe=None):
        super().__init__(message)
        self.reason = reason
        self.returncode = returncode
        self.detail = detail
        self.code = code
        self.safe = safe


PROGRAMS = ('pg_restore', 'pg_dump', 'psql', 'createdb', 'dropdb', 'oras', 'cosign', 'podman', 'systemctl', 'curl')
STDERR_TAIL = ('oras', 'cosign')


def describe(args, stderr, returncode):
    names = [Path(arg).name for arg in args]
    known = [index for index, name in enumerate(names) if name in PROGRAMS]
    start = known[-1] if known else (args.index('--') + 1 if '--' in args else 0)
    program = list(args[start:])
    words = [Path(program[0]).name]
    for arg in program[1:3]:
        if not re.fullmatch('-?[a-z][a-z-]*', arg):
            break
        words.append(arg)
    detail = ' '.join(words) + ': exit ' + str(returncode)
    if words[0] not in STDERR_TAIL:
        return detail
    text = ' '.join(stderr.decode('utf-8', errors='replace').split())
    if re.search('auth|basic|bearer|password|secret|token|credential', text, re.IGNORECASE):
        return detail + ': error output withheld'
    return detail + ': ' + text[-300:]


def safe_line(pattern, stderr):
    if pattern is None:
        return None
    found = [match[1] for line in stderr.decode('utf-8', errors='replace').split('\n')
             if (match := pattern.fullmatch(line))]
    return found[-1] if found else None


def failure_reason(args, stderr):
    text = stderr.decode('utf-8', errors='replace').lower()
    if Path(args[0]).name == 'oras' and (args[1:3] == ['repo', 'tags'] or args[1:2] == ['resolve']):
        if re.search(r'(?:^|[:\n]\s*)(?:name unknown|repository not found)(?::|$)', text):
            return 'not_found'
        if re.search(r'get "[^"\n]+/v2/[^"\n]+/tags/list(?:\?[^"\n]*)?": response status code 404:', text):
            return 'not_found'
        if args[1:2] == ['resolve'] and re.search(
                r'^error response from registry: failed to resolve digest: [a-z0-9._:/@-]+: not found$', text, re.M):
            return 'not_found'
        if 'denied: requested access to the resource is denied' in text:
            return 'denied'
    if Path(args[0]).name == 'cosign' and 'verify-attestation' in args:
        if 'none of the expected identities matched what was in the certificate' in text:
            return 'identity'
        if 'no matching certificateidentity found' in text and ('expected san value' in text or 'expected san value to match regex' in text):
            return 'identity'
    return 'failed'


def bounded_output(process, limit, timeout, input=None, check=check_stop):
    deadline = time.monotonic() + timeout
    output, errors = bytearray(), bytearray()
    pending = memoryview(input) if input else None
    def finish_input(selector=None):
        if selector is not None:
            selector.unregister(process.stdin)
        process.stdin.close()
        process.stdin = None
    with selectors.DefaultSelector() as selector:
        selector.register(process.stdout, selectors.EVENT_READ, output)
        selector.register(process.stderr, selectors.EVENT_READ, errors)
        if process.stdin is not None:
            if pending:
                os.set_blocking(process.stdin.fileno(), False)
                selector.register(process.stdin, selectors.EVENT_WRITE, 'input')
            else:
                finish_input()
        while selector.get_map():
            check()
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired('host command', timeout)
            ready = selector.select(min(remaining, 0.2))
            check()
            if not ready:
                continue
            for key, _ in ready:
                if key.data == 'input':
                    try:
                        pending = pending[os.write(key.fd, pending[:65536]):]
                    except BlockingIOError:
                        continue
                    except BrokenPipeError:
                        pending = None
                    if not pending:
                        finish_input(selector)
                    continue
                size = min(65536, limit + 1 - len(output)) if key.data is output else 65536
                chunk = os.read(key.fd, size)
                if not chunk:
                    selector.unregister(key.fileobj)
                elif key.data is output:
                    output.extend(chunk)
                    if len(output) > limit:
                        raise CommandError('command output exceeds byte limit')
                else:
                    errors.extend(chunk[:max(0, 65536 - len(errors))])
        while True:
            check()
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired('host command', timeout)
            try:
                process.wait(timeout=min(remaining, 0.2))
                check()
                if time.monotonic() > deadline:
                    raise subprocess.TimeoutExpired('host command', timeout)
                break
            except subprocess.TimeoutExpired:
                continue
    return bytes(output), bytes(errors)


def terminate(process):
    if process.returncode is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    deadline = time.monotonic() + 0.2
    exited = False
    while time.monotonic() < deadline:
        if os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT) is not None:
            exited = True
            break
        time.sleep(min(0.02, max(0, deadline - time.monotonic())))
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    except PermissionError:
        if not (exited and sys.platform == 'darwin'):
            raise
    process.wait()


def command(args, input=None, timeout=300, stdout_limit=None, error_prefix=None, input_file=None, safe=None, cancel=False):
    if input is not None and input_file is not None:
        raise ValueError('conflicting command input')
    cleanup_name = args[-1] if args else ''
    container_cleanup = (re.fullmatch(r'trachuan-updater-smoke-[0-9a-f]{32}', cleanup_name) and
                         (args[-6:-1] == ['podman', 'rm', '--force', '--time', '0'] or
                          args[-4:-1] == ['podman', 'container', 'exists']))
    if cancel and ((args[-4:] != ['systemctl', '--user', 'stop', 'trachuan-caddy.service']
                    and not container_cleanup) or timeout != 10 or input is not None or input_file is not None):
        raise ValueError('invalid cancellation command')
    check = (lambda: None) if cancel else check_stop
    check()
    process = None
    finished = False
    try:
        with ExitStack() as stack:
            source = stack.enter_context(open(input_file, 'rb')) if input_file is not None else None
            try:
                process = subprocess.Popen(
                    args, stdin=source if source is not None else subprocess.PIPE if input is not None else subprocess.DEVNULL,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
                check()
                if stdout_limit is None:
                    deadline = time.monotonic() + timeout
                    pending = input
                    while True:
                        check()
                        remaining = deadline - time.monotonic()
                        if remaining <= 0:
                            raise subprocess.TimeoutExpired(args[0], timeout)
                        try:
                            output, errors = process.communicate(input=pending, timeout=min(remaining, 0.2))
                            check()
                            if time.monotonic() > deadline:
                                raise subprocess.TimeoutExpired(args[0], timeout)
                            break
                        except subprocess.TimeoutExpired:
                            pending = None
                else:
                    if type(stdout_limit) is not int or stdout_limit < 0:
                        raise ValueError('invalid streaming command limits')
                    output, errors = bounded_output(process, stdout_limit, timeout, input, check)
                if process.returncode:
                    code = None
                    if error_prefix:
                        found = re.fullmatch(re.escape(error_prefix.encode()) + rb'([A-Za-z][A-Za-z0-9_]{0,79})\n', errors)
                        code = found[1].decode() if found else 'SDKError'
                    finished = True
                    raise CommandError('command failed', reason=failure_reason(args, errors),
                                       returncode=process.returncode, code=code,
                                       detail=describe(args, errors, process.returncode),
                                       safe=safe_line(safe, errors))
                finished = True
            finally:
                if process is not None:
                    if not finished:
                        terminate(process)
                    for stream in (process.stdin, process.stdout, process.stderr):
                        if stream is not None:
                            stream.close()
    except subprocess.TimeoutExpired as error:
        raise CommandError('command timed out', reason='timeout',
                           detail=Path(args[0]).name + ': timeout') from error
    except (OSError, subprocess.SubprocessError) as error:
        raise CommandError('command failed') from error
    if len(output) > 16 * 1024 * 1024:
        raise CommandError('command output too large')
    return output


def interrupted(error):
    seen = set()
    while error is not None and id(error) not in seen:
        if isinstance(error, (InterruptedError, StopRequested)):
            return True
        seen.add(id(error))
        error = error.__cause__ or error.__context__
    return False


class Result:
    def __init__(self, path, log, clock=now, state=None):
        self.path, self.log, self.clock = Path(path), Path(log), clock
        self.state = state
        self.started = clock()
        self.previous = {}
        try:
            self.previous = json.loads(self.path.read_bytes())
            if not isinstance(self.previous, dict):
                self.previous = {}
        except (OSError, ValueError):
            pass

    def write(self, status, step, message, digest=None, manifest=None):
        manifest = manifest or {}
        finished = self.clock()
        success = finished if status in ('ok', 'verified', 'bootstrapped', 'current') else self.previous.get('last_success_at')
        value = dict(status=status, manifest_hash=digest, release=manifest.get('release'),
                     sequence=manifest.get('sequence'), step=step, message=message,
                     release_floor=self.state.data.get('release_floor') if self.state else None,
                     started_at=self.started, finished_at=finished, last_success_at=success)
        atomic(self.path, canonical(value) + b'\n', 0o640)
        self.append(value)

    def append(self, value):
        descriptor = os.open(self.log, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o640)
        with os.fdopen(descriptor, 'ab') as output:
            output.write(canonical(dict(timestamp=self.clock(), **value)) + b'\n')
            output.flush()
            os.fsync(output.fileno())
