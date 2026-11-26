import json
import unittest

from updater import deploy_corpus_verify as verify
from updater.corpus_verify_test import manifest
from updater.deploy_io import canonical
from updater.law_restore_test import LawFixture
from updater.verification_test import FIXTURE_POLICY
from updater.world_test import LawCase


EVIDENCE = dict(projection=1, scan_sha256='a' * 64, exclusions_sha256='b' * 64)
KINDS = ('document_added', 'document_removed', 'document_validity', 'provision_validity', 'provision_text', 'document_key')


def schema9():
    return dict(manifest(), schema_version=9, heading_evidence=EVIDENCE,
                changes_projection=dict(version=1, sha256='c' * 64))


def report(obj):
    return dict(changes_projection=dict(version=1, sha256=obj['changes_projection']['sha256'],
                                        release=obj['rel'], previous=obj['previous'], baseline=False,
                                        counts={kind: 0 for kind in KINDS}))


class ProjectionManifestTest(unittest.TestCase):
    def test_schema_requires_exact_known_projection(self):
        obj = schema9()
        self.assertEqual(verify.manifest(canonical(obj), FIXTURE_POLICY), obj)
        invalid = [dict(obj, schema_version=8), dict(obj, schema_version=10), {k: v for k, v in obj.items() if k != 'changes_projection'}]
        for value in (None, {}, dict(version=True, sha256='c'*64), dict(version=2, sha256='c'*64),
                      dict(version=1, sha256='C'*64), dict(version=1, sha256='c'*64, extra=1)):
            invalid.append(dict(obj, changes_projection=value))
        for value in invalid:
            with self.subTest(value=value), self.assertRaises(ValueError):
                verify.manifest(canonical(value), FIXTURE_POLICY)

    def test_target_report_is_strict_and_bound_to_manifest(self):
        obj = schema9()
        good = report(obj)
        self.assertEqual(verify.projection(obj, good), good['changes_projection']['counts'])
        for bad in ({}, dict(good, changes_projection=None),
                    *[dict(good, changes_projection=dict(good['changes_projection'], **change)) for change in (
                        dict(version=2), dict(version=True), dict(sha256='d'*64), dict(release='20990101'),
                        dict(previous='20990103'), dict(baseline=0), dict(counts={}),
                        dict(counts=dict(good['changes_projection']['counts'], document_added=True)),
                        dict(counts=dict(good['changes_projection']['counts'], document_added=-1)),
                        dict(baseline=True, counts=dict(good['changes_projection']['counts'], document_added=1)),
                        dict(extra=1))]):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                verify.projection(obj, bad)
        old = dict(manifest(), schema_version=8, heading_evidence=EVIDENCE)
        self.assertIsNone(verify.projection(old, {}))
        with self.assertRaises(ValueError):
            verify.projection(old, good)


class ProjectionRecoveryTest(LawCase, unittest.TestCase):
    def prepare_swapping(self):
        self.m = schema9()
        self.write_manifest()
        self.db.describe.return_value = dict(self.description(self.m), schema_versions=[9])
        oid = self.db.add(self.name)
        self.open_operation('swapping', oid)

    def test_unswapped_recovery_waits_for_compatible_recipes(self):
        self.prepare_swapping()
        self.db.describe.return_value['dense_query_hash'] = 'f' * 64
        self.assertTrue(self.again())
        self.assertEqual(self.result()['status'], 'waiting')
        self.assertEqual(self.stored()['operation']['step'], 'swapping')
        self.assertEqual(self.db.pointer_value, self.active_name)
        self.assertEqual(self.db.swaps, [])
        self.assertNotIn('smoke', self.db.log)

    def test_unswapped_recovery_refuses_unsupported_schema_without_smoke(self):
        self.prepare_swapping()
        self.db.describe.return_value['schema_versions'] = [8]
        self.assertFalse(self.again())
        self.assertEqual(self.result()['status'], 'failed')
        self.assertEqual(self.stored()['operation']['step'], 'swapping')
        self.assertEqual(self.db.pointer_value, self.active_name)
        self.assertEqual(self.db.swaps, [])
        self.assertNotIn('smoke', self.db.log)

    def test_unswapped_recovery_refuses_failed_projection_check(self):
        self.prepare_swapping()
        def mismatched(row, image):
            self.db.log.append('smoke')
            value = report(row['manifest'])
            value['changes_projection']['sha256'] = 'd' * 64
            verify.projection(row['manifest'], value)
        self.db.smoke = mismatched
        self.assertFalse(self.again())
        self.assertEqual(self.result()['step'], 'recovery')
        self.assertEqual(self.db.pointer_value, self.active_name)
        self.assertEqual(self.db.swaps, [])
        self.assertIn('smoke', self.db.log)

    def test_already_swapped_recovery_does_not_repeat_projection_check(self):
        self.prepare_swapping()
        self.db.pointer_value = self.name
        self.assertTrue(self.again())
        self.assertNotIn('smoke', self.db.log)


class ProjectionSmokeTest(LawFixture, unittest.TestCase):
    def test_retained_target_passes_signed_values_and_records_counts(self):
        obj = schema9()
        self.row['manifest'] = obj
        self.responses['release-check'] = json.dumps(report(obj)).encode()
        self.db.host.paths.log.parent.mkdir(parents=True)
        self.db.smoke(dict(self.row, mode='retained'), 'server-image')
        args, _ = self.call('release-check')
        for key, value in (('--projection-version', '1'), ('--projection-sha256', 'c'*64),
                           ('--projection-release', obj['rel']), ('--projection-previous', obj['previous'])):
            self.assertEqual(args[args.index(key) + 1], value)
        self.assertIn('document_added', self.db.host.paths.log.read_text())

    def test_duplicate_projection_keys_in_release_check_are_refused(self):
        obj = schema9()
        good = json.dumps(report(obj))
        self.responses['release-check'] = ('{"changes_projection":{},' + good[1:]).encode()
        with self.assertRaises(ValueError):
            self.db.describe('server-image', self.name, manifest=obj)
        nested = json.dumps(report(obj)).replace('"version": 1', '"version": 1, "version": 1')
        self.responses['release-check'] = nested.encode()
        with self.assertRaises(ValueError):
            self.db.describe('server-image', self.name, manifest=obj)

    def test_empty_previous_remains_a_separate_argument(self):
        obj = dict(schema9(), previous='')
        self.db.describe('server-image', self.name, manifest=obj)
        args, _ = self.call('release-check')
        self.assertEqual(args[args.index('--projection-previous') + 1], '')

    def test_schema9_invalid_report_refuses(self):
        self.row['manifest'] = schema9()
        with self.assertRaises(ValueError):
            self.db.smoke(self.row, 'server-image')

    def test_schema8_sends_no_projection_arguments(self):
        self.row['manifest'] = dict(manifest(), schema_version=8, heading_evidence=EVIDENCE)
        self.db.smoke(self.row, 'server-image')
        args, _ = self.call('release-check')
        self.assertFalse(any(arg.startswith('--projection-') for arg in args))
