from repositories_test_support import enable_dual_policy as setUpModule

import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from corpus_manifest import validate_inputs, validate_manifest
from unittest.mock import patch
from corpus_test_support import add_heading_build, digest, remove_build_record, save_fixture, signed_create as create, write_fixture


class HeadingEvidenceTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.path = Path(temporary.name)
        self.row, self.report = write_fixture(self.path)

    def create(self):
        return json.loads(create('dannyota/trachuan', 'a' * 40, 1, 'vn', self.row['rel'], self.path,
                                 self.path / 'report.json'))

    def schema_nine(self, mutate=None):
        record = add_heading_build(self.path, self.row, self.report)
        record['schema_version'] = 9
        record['changes_projection'] = {'version': 1, 'sha256': hashlib.sha256(
            b'v1\tvn\t20990102_1\t20990101_1\t\t1\n').hexdigest()}
        record['changes_counts'] = dict.fromkeys(('document_added', 'document_removed', 'document_validity',
                                                 'provision_validity', 'provision_text', 'document_key'), 0)
        if mutate:
            mutate(record)
        data = json.dumps(record).encode()
        (self.path / 'build.json').write_bytes(data)
        manifest = (digest((self.path / 'corpus.dump').read_bytes()) + '  corpus.dump\n' +
                    digest(data) + '  build.json\n').encode()
        (self.path / 'MANIFEST.sha256').write_bytes(manifest)
        self.row['schema_version'] = 9
        self.report['build_artifact_sha256'] = self.row['run']['build_artifact_sha256'] = digest(manifest)
        save_fixture(self.path, self.row, self.report)
        return record

    def test_pre_eight_first_signing_requires_signed_manifest(self):
        from corpus_manifest import validated_inputs
        self.row['schema_version'] = 7
        remove_build_record(self.path, self.row, self.report)
        with patch('corpus_manifest.validate_authorized'), self.assertRaises(ValueError):
            validated_inputs('vn', self.row['rel'], self.path, self.path / 'report.json', signing=True)

    def test_pre_eight_resign_ignores_missing_named_build(self):
        from corpus_manifest import validated_inputs
        from corpus_test_support import set_schema_build
        set_schema_build(self.path, self.row, self.report, 7)
        (self.path / 'build.json').unlink()
        with patch('corpus_manifest.validate_authorized'):
            result = validated_inputs('vn', self.row['rel'], self.path, self.path / 'report.json',
                                      signing=True, signed_manifest={'schema_version': 7})
            self.assertEqual(result.row['schema_version'], 7)
        (self.path / 'build.json').write_bytes(b'not a build record')
        with patch('corpus_manifest.validate_authorized'):
            result = validated_inputs('vn', self.row['rel'], self.path, self.path / 'report.json',
                                      signing=True, signed_manifest={'schema_version': 7})
            self.assertEqual(result.row['schema_version'], 7)

    def test_pre_eight_resign_uses_signed_schema(self):
        from corpus_manifest import validated_inputs
        self.row['schema_version'] = 7
        remove_build_record(self.path, self.row, self.report)
        with patch('corpus_manifest.validate_authorized'):
            result = validated_inputs('vn', self.row['rel'], self.path, self.path / 'report.json',
                                      signing=True, signed_manifest={'schema_version': 7})
            self.assertEqual(result.row['schema_version'], 7)
            with self.assertRaises(ValueError):
                validated_inputs('vn', self.row['rel'], self.path, self.path / 'report.json',
                                 signing=True, signed_manifest={'schema_version': 6})

    def test_schema_nine_build_refuses_spoofed_release_versions(self):
        for version in (7, 8, 10):
            with self.subTest(version=version):
                self.schema_nine()
                self.row['schema_version'] = version
                (self.path / 'release.json').write_text(json.dumps(self.row))
                with self.assertRaises(ValueError):
                    self.create()
                with patch('corpus_manifest.validate_authorized'), self.assertRaises(ValueError):
                    validate_inputs('vn', self.row['rel'], self.path, self.path / 'report.json')

    def test_unsupported_build_schema_is_refused_even_when_release_matches(self):
        self.schema_nine(lambda record: record.update(schema_version=10))
        self.row['schema_version'] = 10
        (self.path / 'release.json').write_text(json.dumps(self.row))
        with self.assertRaises(ValueError):
            self.create()

    def test_schema_nine_projection_matches_verified_sidecar(self):
        record = self.schema_nine()
        obj = self.create()
        self.assertEqual(obj['changes_projection'], record['changes_projection'])
        self.assertEqual(validate_manifest(obj), obj)

    def test_schema_nine_upload_checks_projection(self):
        self.schema_nine()
        with patch('corpus_manifest.validate_authorized'):
            validate_inputs('vn', self.row['rel'], self.path, self.path / 'report.json')
        self.schema_nine(lambda r: r['changes_counts'].update(document_added=1))
        with patch('corpus_manifest.validate_authorized'), self.assertRaises(ValueError):
            validate_inputs('vn', self.row['rel'], self.path, self.path / 'report.json')

    def test_schema_nine_rejects_sidecar_changed_after_hash_check(self):
        from corpus_projection import project
        self.schema_nine()
        path = self.path / 'changes.jsonl'
        def changed(source, country):
            path.write_bytes(path.read_bytes().replace(b'"schema":1', b'"schema" : 1', 1))
            return project(source, country)
        with patch('corpus_evidence.project', side_effect=changed), self.assertRaises(ValueError):
            self.create()

    def test_schema_nine_without_sidecars_is_refused(self):
        from corpus_test_support import save_older_shape
        self.schema_nine()
        save_older_shape(self.path, self.row)
        with patch('corpus_manifest.older_shape_allowed', return_value=True), self.assertRaises(ValueError):
            self.create()

    def test_schema_nine_rejects_bad_projection_or_counts(self):
        faults = [lambda r: r.pop('changes_projection'), lambda r: r.pop('changes_counts'),
                  lambda r: r['changes_projection'].update(version=2),
                  lambda r: r['changes_projection'].update(version=True),
                  lambda r: r['changes_projection'].update(sha256='a' * 64),
                  lambda r: r['changes_projection'].update(extra=1),
                  lambda r: r['changes_counts'].update(document_added=1),
                  lambda r: r['changes_counts'].update(document_added=False),
                  lambda r: r['changes_counts'].pop('document_key'),
                  lambda r: r['changes_counts'].update(extra=0)]
        for fault in faults:
            with self.subTest(fault=fault):
                self.schema_nine(fault)
                with self.assertRaises(ValueError):
                    self.create()

    def test_manifest_projection_requires_schema_nine(self):
        add_heading_build(self.path, self.row, self.report)
        obj = self.create()
        obj['schema_version'] = 9
        with self.assertRaises(ValueError):
            validate_manifest(obj)
        for value in (None, {}, {'version': 2, 'sha256': 'a' * 64},
                      {'version': True, 'sha256': 'a' * 64},
                      {'version': 1, 'sha256': 'A' * 64},
                      {'version': 1, 'sha256': 'a' * 64, 'extra': 1}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                validate_manifest(obj | {'changes_projection': value})
        with self.assertRaises(ValueError):
            validate_manifest(obj | {'schema_version': 8, 'changes_projection': {'version': 1, 'sha256': 'a' * 64}})

    def test_schema_eight_copies_verified_build_values(self):
        record = add_heading_build(self.path, self.row, self.report)
        obj = self.create()
        self.assertEqual(obj.get('heading_evidence'), dict(projection=record['heading_projection'],
                         scan_sha256=record['heading_scan_sha256'],
                         exclusions_sha256=record['heading_exclusions_sha256']))
        self.assertEqual(validate_manifest(obj), obj)

    def test_schema_seven_does_not_require_or_read_build_record(self):
        self.row['schema_version'] = 7
        remove_build_record(self.path, self.row, self.report)
        self.assertNotIn('heading_evidence', self.create())
        (self.path / 'build.json').write_text('invalid JSON')
        self.assertNotIn('heading_evidence', self.create())

    def test_build_record_missing_or_tampered_is_refused(self):
        for fault in ('missing', 'tampered', 'duplicate line', 'wrong hash', 'missing line'):
            with self.subTest(fault=fault):
                add_heading_build(self.path, self.row, self.report)
                path = self.path / 'build.json'
                manifest = self.path / 'MANIFEST.sha256'
                if fault == 'missing':
                    path.unlink()
                elif fault == 'tampered':
                    path.write_bytes(path.read_bytes().replace(b'a', b'c'))
                else:
                    lines = manifest.read_text().splitlines(keepends=True)
                    if fault == 'duplicate line':
                        lines.append(lines[-1])
                    elif fault == 'wrong hash':
                        lines[-1] = '0' * 64 + '  build.json\n'
                    else:
                        lines.pop()
                    manifest.write_text(''.join(lines))
                    self.report['build_artifact_sha256'] = self.row['run']['build_artifact_sha256'] = digest(manifest.read_bytes())
                    save_fixture(self.path, self.row, self.report)
                with self.assertRaises((ValueError, OSError)):
                    self.create()

    def test_verified_build_record_requires_well_typed_evidence(self):
        for field, value in [('heading_projection', None), ('heading_projection', True),
                             ('heading_projection', 1.0), ('heading_scan_sha256', 'BAD'),
                             ('heading_exclusions_sha256', 'B' * 64)]:
            with self.subTest(field=field, value=value):
                record = add_heading_build(self.path, self.row, self.report)
                if value is None:
                    record.pop(field)
                else:
                    record[field] = value
                data = json.dumps(record).encode()
                (self.path / 'build.json').write_bytes(data)
                path = self.path / 'MANIFEST.sha256'
                lines = path.read_text().splitlines(keepends=True)
                lines[-1] = digest(data) + '  build.json\n'
                path.write_text(''.join(lines))
                self.report['build_artifact_sha256'] = self.row['run']['build_artifact_sha256'] = digest(path.read_bytes())
                save_fixture(self.path, self.row, self.report)
                with self.assertRaises(ValueError):
                    self.create()

    def test_manifest_evidence_schema(self):
        obj = self.create()
        obj['schema_version'] = 8
        evidence = dict(projection=1, scan_sha256='a' * 64, exclusions_sha256='b' * 64)
        obj['heading_evidence'] = evidence
        self.assertEqual(validate_manifest(obj), obj)
        cases = [None, [], {}, evidence | {'extra': 1}]
        for field in evidence:
            cases.append({key: value for key, value in evidence.items() if key != field})
        for field, value in [('projection', True), ('projection', '1'), ('projection', 1.0),
                             ('scan_sha256', 'A' * 64), ('scan_sha256', 'a' * 63),
                             ('exclusions_sha256', 'BAD'), ('exclusions_sha256', None)]:
            cases.append(evidence | {field: value})
        for value in cases:
            with self.subTest(evidence=value):
                candidate = copy.deepcopy(obj)
                candidate['heading_evidence'] = value
                with self.assertRaises(ValueError):
                    validate_manifest(candidate)

    def test_evidence_is_required_on_eight_and_forbidden_on_seven(self):
        obj = self.create()
        obj['schema_version'] = 8
        with self.assertRaises(ValueError):
            validate_manifest(obj)
        obj['schema_version'] = 7
        obj['heading_evidence'] = dict(projection=1, scan_sha256='a' * 64, exclusions_sha256='b' * 64)
        with self.assertRaises(ValueError):
            validate_manifest(obj)

    def test_review_is_copied_independently_of_evidence(self):
        add_heading_build(self.path, self.row, self.report)
        review = dict(release=self.row['rel'], projection=1, scan_sha256='c' * 64,
                      exclusions_sha256='d' * 64, reviewer='coordinator', date='2026-11-16')
        reviews = self.path / 'reviews.yaml'
        reviews.write_text(json.dumps([review]))
        obj = json.loads(create('dannyota/trachuan', 'a' * 40, 1, 'vn', self.row['rel'], self.path,
                               self.path / 'report.json', heading_reviews_path=reviews))
        self.assertEqual(obj['heading_review'], {key: value for key, value in review.items() if key != 'release'})
        self.assertEqual(obj.get('heading_evidence'), dict(projection=1, scan_sha256='a' * 64, exclusions_sha256='b' * 64))
