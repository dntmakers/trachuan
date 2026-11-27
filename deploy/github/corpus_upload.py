import argparse
import contextvars
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile
import time

from corpus_dispatch import dispatch_command
from corpus_manifest import identity, release_files, validated_inputs
from corpus_previous import verified_previous
from corpus_production import require_settled
from manifest import positive
from repositories import require_listed, write_repository


ATTEMPT = contextvars.ContextVar('aws_attempt', default=1)
TRANSIENT = frozenset({'429', '500', '502', '503', '504', 'InternalError', 'InternalFailure', 'InternalServerError',
                       'ServiceUnavailable', 'SlowDown', 'Throttling', 'ThrottlingException',
                       'TooManyRequestsException', 'RequestTimeout', 'RequestTimeoutException'})
PERMANENT = frozenset({'401', '403', 'AccessDenied', 'InvalidAccessKeyId', 'SignatureDoesNotMatch',
                       'BadDigest', 'EntityTooLarge', 'InvalidPart', 'NoSuchUpload',
                       'NoCredentials', 'Configuration', 'NoRegion', 'ParamValidation'})
NETWORK_PREFIXES = ('Could not connect to the endpoint URL:', 'Connection was closed before we received a valid response',
                    'Read timeout on endpoint URL:', 'Connect timeout on endpoint URL:')


class AWSError(ValueError):
    def __init__(self, operation, code, retryable=False):
        self.code = code
        self.retryable = retryable
        super().__init__(f'aws {operation} failed: {code}: request failed')


def aws_run(command, operation):
    env = dict(os.environ, AWS_CLI_ERROR_FORMAT='json')
    if operation == 'upload-part':
        env.update(AWS_MAX_ATTEMPTS='1', AWS_RETRY_MODE='standard')
    try:
        result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=3600, env=env)
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired, OSError) as error:
        code = 'UnknownError'
        if isinstance(error, subprocess.TimeoutExpired):
            code = 'NetworkTimeout'
        elif isinstance(error, OSError):
            code = 'CommandStartError'
        else:
            stderr = error.stderr or ''
            try:
                parsed = json.loads(stderr)
                candidate = parsed.get('Code') if isinstance(parsed, dict) else None
                if isinstance(candidate, str) and candidate in TRANSIENT | PERMANENT:
                    code = candidate
            except (ValueError, TypeError):
                if any(stderr.strip().startswith(prefix) for prefix in NETWORK_PREFIXES):
                    code = 'NetworkError'
        failure = AWSError(operation, code, code in TRANSIENT or code in {'NetworkError', 'NetworkTimeout'})
        print(f'aws {operation} attempt {ATTEMPT.get()}: {code}: request failed', file=sys.stderr, flush=True)
        raise failure from None
    return result.stdout


def elapsed_clock():
    if sys.platform == 'darwin':
        return time.clock_gettime(time.CLOCK_MONOTONIC_RAW)
    if hasattr(time, 'CLOCK_BOOTTIME'):
        return time.clock_gettime(time.CLOCK_BOOTTIME)
    raise ValueError('sleep-aware clock unavailable')


def aws_call(command, args):
    return json.loads(aws_run([*command, '--output', 'json', *args], args[0]))


def require_cli():
    version = aws_run(['aws', '--version'], 'version').strip()
    match = re.match(r'^aws-cli/2\.(\d+)\.(\d+)(?:\s|$)', version)
    if match is None or tuple(map(int, match.groups())) < (34, 0):
        raise ValueError('AWS CLI v2.34.0 or later is required')
    print(version, flush=True)


def require_empty_prefix(command, bucket, prefix):
    count = aws_call(command, ['list-objects-v2', '--bucket', bucket, '--prefix', prefix,
                               '--max-keys', '1', '--query', 'KeyCount'])
    if type(count) is not int or count != 0:
        raise ValueError('Release prefix already holds objects; choose a new rel')


def etag(result):
    value = result.get('ETag')
    if not isinstance(value, str) or not value:
        raise ValueError('upload did not return an ETag')
    return value


def require_validated(digest, size, expected, key):
    if digest != expected[0] or size != expected[1]:
        raise ValueError('local file changed after validation: ' + key.rsplit('/', 1)[1])


def upload_file(path, key, client, part_size=64 * 1024**2, bucket='trachuan-releases', expected=None):
    size = path.stat().st_size
    positive(size, 'upload size')
    common = ['--bucket', bucket, '--key', key]
    if expected is not None and size != expected[1]:
        raise ValueError('local file changed after validation: ' + key.rsplit('/', 1)[1])

    def put(body):
        return dict(ContentLength=size, ETag=etag(client(['put-object', *common, '--body', str(body),
                                                          '--content-length', str(size)])))

    if size <= part_size:
        if expected is None:
            return put(path)
        data = path.read_bytes()
        require_validated(hashlib.sha256(data).hexdigest(), len(data), expected, key)
        with tempfile.TemporaryDirectory() as temporary:
            body = Path(temporary) / 'body'
            body.write_bytes(data)
            return put(body)
    deadline = elapsed_clock() + 12 * 60 * 60
    upload_id = client(['create-multipart-upload', *common]).get('UploadId')
    if not isinstance(upload_id, str) or not upload_id:
        raise ValueError('multipart upload did not return an upload id')
    multipart = [*common, '--upload-id', upload_id]
    parts, transferred, stream = [], 0, hashlib.sha256()
    try:
        with tempfile.TemporaryDirectory() as temporary, path.open('rb') as source:
            body = Path(temporary) / 'part'
            while chunk := source.read(part_size):
                stream.update(chunk)
                body.write_bytes(chunk)
                number = len(parts) + 1
                for attempt in range(4):
                    token = ATTEMPT.set(attempt + 1)
                    try:
                        if elapsed_clock() >= deadline:
                            raise ValueError('multipart upload deadline exceeded')
                        result = client(['upload-part', *multipart, '--part-number', str(number),
                                         '--body', str(body), '--content-length', str(len(chunk))])
                        break
                    except AWSError as error:
                        if attempt == 3 or not error.retryable:
                            raise
                        time.sleep(2 ** attempt)
                    finally:
                        ATTEMPT.reset(token)
                parts.append(dict(PartNumber=number, ETag=etag(result)))
                transferred += len(chunk)
        if transferred != size:
            raise ValueError('local upload size changed')
        if expected is not None:
            require_validated(stream.hexdigest(), transferred, expected, key)
        if elapsed_clock() >= deadline:
            raise ValueError('multipart upload deadline exceeded')
        result = client(['complete-multipart-upload', *multipart,
                         '--multipart-upload', json.dumps(dict(Parts=parts))])
        if elapsed_clock() >= deadline:
            raise ValueError('multipart upload deadline exceeded')
        return dict(ContentLength=size, ETag=etag(result))
    except BaseException:
        try:
            client(['abort-multipart-upload', *multipart])
        except Exception:
            pass
        raise


def verify_receipts(receipts, client, bucket='trachuan-releases'):
    for key, receipt in receipts.items():
        current = client(['head-object', '--bucket', bucket, '--key', key])
        if type(current.get('ContentLength')) is not int or current['ContentLength'] != receipt['ContentLength'] or current.get('ETag') != receipt['ETag']:
            raise ValueError('uploaded object size or ETag changed: ' + key.rsplit('/', 1)[1])


def upload(country, rel, directory, report, repository):
    require_listed(repository)
    identity(country, rel)
    command = ['aws', '--profile', 'trachuan-release-upload', '--endpoint-url',
               os.environ['OCI_S3_ENDPOINT'], '--no-cli-pager', 's3api']

    def client(args):
        return aws_call(command, args)

    inputs = validated_inputs(country, rel, directory, report)
    require_cli()
    require_empty_prefix(command, 'trachuan-releases', f'{country}/{rel}/')
    row, candidate, sidecars = inputs.row, inputs.report, inputs.sidecars
    if sidecars is not None:
        print('Open findings allowed: review {review}, info {info}'.format(**sidecars['open']))
        print('Findings against {rel}: new {new}, gone {gone}'.format(rel=sidecars['predecessor']['rel'], **sidecars['changed']))
    with tempfile.TemporaryDirectory() as temporary:
        require_settled(candidate, Path(temporary), repository)
        verified_previous(candidate, Path(temporary), client, repository, release_row=row, sidecars=sidecars)

    receipts = {}
    with tempfile.TemporaryDirectory() as temporary:
        for name in release_files(row, include_build='build.json' in inputs.expected):
            source = directory / name
            if name in inputs.data:
                source = Path(temporary) / name
                source.write_bytes(inputs.data[name])
            key = f'{country}/{rel}/{name}'
            receipts[key] = upload_file(source, key, client, expected=inputs.expected[name])
    verify_receipts(receipts, client)
    (directory / 'report.json').write_bytes(inputs.data['report.json'])
    subprocess.run(dispatch_command(repository, country, rel, inputs.expected['report.json'][0],
                                    inputs.expected['release.json'][0]), check=True)


def print_dispatch(country, rel, directory, report, repository):
    identity(country, rel)
    inputs = validated_inputs(country, rel, directory, report)
    print(' '.join(dispatch_command(repository, country, rel, inputs.expected['report.json'][0], inputs.expected['release.json'][0])))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--dry-run', action='store_true')
    parser.add_argument('country')
    parser.add_argument('rel')
    parser.add_argument('directory', type=Path)
    parser.add_argument('report', type=Path)
    parser.add_argument('--repository')
    args = parser.parse_args()
    repository = write_repository(args.repository)
    if args.dry_run:
        print_dispatch(args.country, args.rel, args.directory, args.report, repository)
    else:
        upload(args.country, args.rel, args.directory, args.report, repository)


if __name__ == '__main__':
    try:
        main()
    except AWSError as error:
        print(error, file=sys.stderr)
        raise SystemExit(1) from None
