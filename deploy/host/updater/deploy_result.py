from datetime import datetime, timezone
import os
import re
from pathlib import Path

from .deploy_io import atomic, canonical, decode, now, read_json_bytes


RESULT_LIMIT = 65536


def timestamp(value):
    if not isinstance(value, str) or len(value) > 64:
        return False
    try:
        parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
        return parsed.utcoffset() == timezone.utc.utcoffset(parsed)
    except (ValueError, OverflowError):
        return False


def previous_fields(value):
    if not isinstance(value, dict):
        return {}, True
    fields = {}
    invalid = False
    for key in ('last_success_at', 'waiting_since', 'waiting_manifest_hash', 'manifest_hash', 'mode', 'release_floor'):
        if key not in value or value[key] is None:
            continue
        item = value[key]
        if key in ('last_success_at', 'waiting_since'):
            valid = timestamp(item)
        elif key == 'mode':
            valid = isinstance(item, str) and item in ('new', 'retained')
        elif key == 'release_floor':
            valid = type(item) is int and 0 < item <= 2**63 - 1
        else:
            valid = isinstance(item, str) and re.fullmatch('[0-9a-f]{64}', item) is not None
        if valid:
            fields[key] = item
        else:
            invalid = True
    bad_waiting_hash = ('waiting_manifest_hash' in value and value['waiting_manifest_hash'] is not None
                        and 'waiting_manifest_hash' not in fields)
    bad_legacy_hash = ('manifest_hash' in value and value['manifest_hash'] is not None
                       and 'manifest_hash' not in fields and 'waiting_manifest_hash' not in fields)
    if bad_waiting_hash or bad_legacy_hash:
        fields.pop('waiting_since', None)
    return fields, invalid


def overflow_receipt(started, finished, previous, floor, integrity=None):
    if type(floor) is not int or not 0 < floor <= 2**63 - 1:
        floor = None
    value = dict(status='failed', step='result', message='result too large',
                 started_at=started, finished_at=finished,
                 last_success_at=previous.get('last_success_at'), release_floor=floor)
    if integrity is not None:
        value['integrity'] = integrity
        value['waiting_since'] = previous.get('waiting_since')
    return value


class ResultTooLarge(ValueError):
    pass


class Result:
    def __init__(self, path, log, clock=now, state=None):
        self.path, self.log, self.clock = Path(path), Path(log), clock
        self.state = state
        self.started = clock()
        self.previous = {}
        self.unreadable_previous = False
        try:
            value = decode(read_json_bytes(self.path, 'previous result', RESULT_LIMIT), 'previous result')
            self.previous, self.unreadable_previous = previous_fields(value)
        except FileNotFoundError:
            pass
        except (OSError, ValueError):
            self.unreadable_previous = True

    def write(self, status, step, message, digest=None, manifest=None):
        manifest = manifest or {}
        finished = self.clock()
        success = finished if status in ('ok', 'verified', 'bootstrapped', 'current') else self.previous.get('last_success_at')
        value = dict(status=status, manifest_hash=digest, release=manifest.get('release'),
                     sequence=manifest.get('sequence'), step=step, message=message,
                     release_floor=self.state.data.get('release_floor') if self.state else None,
                     started_at=self.started, finished_at=finished, last_success_at=success)
        self.publish(value)

    def publish(self, value):
        raw = canonical(value) + b'\n'
        release, sequence = value.get('release'), value.get('sequence')
        too_large = (len(raw) > RESULT_LIMIT
                     or (release is not None and (not isinstance(release, str) or not 0 < len(release) <= 256))
                     or (sequence is not None and (type(sequence) is not int or sequence < 1)))
        if too_large:
            value = self.overflow(value)
            raw = canonical(value) + b'\n'
        atomic(self.path, raw, 0o640)
        self.warn_previous()
        self.append(value)
        if too_large:
            raise ResultTooLarge('result too large')

    def overflow(self, value=None):
        floor = self.state.data.get('release_floor') if self.state else self.previous.get('release_floor')
        return overflow_receipt(self.started, self.clock(), self.previous, floor)

    def warn_previous(self):
        if self.unreadable_previous:
            self.append(dict(level='warning', step='result', message='previous result unreadable'))
            self.unreadable_previous = False

    def append(self, value):
        descriptor = os.open(self.log, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o640)
        with os.fdopen(descriptor, 'ab') as output:
            output.write(canonical(dict(timestamp=self.clock(), **value)) + b'\n')
            output.flush()
            os.fsync(output.fileno())
