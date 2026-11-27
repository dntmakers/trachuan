import json
from pathlib import Path
import re
import unittest


OCI = Path(__file__).parents[1] / 'oci'
BUCKET = 'trachuan-frameworks-releases'


def statements():
    found = []
    for line in (OCI / 'iam.tf').read_text().splitlines():
        if line.strip().startswith('statements '):
            found.extend(json.loads(line.split('=', 1)[1]))
    return found


def bucket(label):
    source = (OCI / 'storage.tf').read_text()
    return re.search(r'resource "oci_objectstorage_bucket" "' + label + r'" \{\n(.*?)\n\}', source, re.S).group(1)


class OCIPolicyTest(unittest.TestCase):
    def test_host_reads_every_policy_secret_from_vault(self):
        policy = json.loads(Path(__file__).with_name('deploy-policy.json').read_bytes())
        reads = [s for s in statements() if s.startswith('Allow dynamic-group trachuan-host to read secret-bundles ')]
        self.assertEqual(len(reads), 1)
        self.assertLessEqual(set(policy['secrets']), set(re.findall(r"target\.secret\.name = '([^']+)'", reads[0])))

    def test_frameworks_bucket_has_law_bucket_settings(self):
        self.assertEqual(bucket('frameworks_releases'),
                         bucket('releases').replace('"trachuan-releases"', '"' + BUCKET + '"'))

    def test_release_buckets_abort_stale_multipart_uploads(self):
        source = (OCI / 'storage.tf').read_text()
        policies = re.findall(r'resource "oci_objectstorage_object_lifecycle_policy" "[^"]+" \{\n(.*?)\n\}', source, re.S)
        self.assertEqual(len(policies), 1)
        policy = policies[0]
        mapping = re.search(r'for_each\s*=\s*\{([^{}]+)\}', policy)
        self.assertIsNotNone(mapping)
        entries = re.findall(r'(\w+)\s*=\s*oci_objectstorage_bucket\.(\w+)', mapping.group(1))
        self.assertEqual(dict(entries), {'corpus': 'releases', 'frameworks': 'frameworks_releases'})
        self.assertEqual(len(entries), 2)
        remainder = re.sub(r'\w+\s*=\s*oci_objectstorage_bucket\.\w+', '', mapping.group(1))
        self.assertEqual(remainder.strip(' ,\t\n'), '')
        self.assertRegex(policy, r'(?m)^\s*bucket\s*=\s*each\.value\.name\s*$')
        self.assertRegex(policy, r'(?m)^\s*namespace\s*=\s*each\.value\.namespace\s*$')
        rules = re.findall(r'(?ms)^\s*rules\s*\{\s*\n(.*?)^\s*\}', policy)
        self.assertEqual(len(rules), 1)
        self.assertEqual(len([line for line in rules[0].splitlines() if line.strip()]), 6)
        assignments = re.findall(r'(?m)^\s*(\w+)\s*=\s*(.*?)\s*$', rules[0])
        self.assertEqual(len(assignments), 6)
        fields = dict(assignments)
        self.assertEqual(fields, {'action': '"ABORT"', 'is_enabled': 'true',
                                  'name': '"abort-stale-multipart-uploads"', 'target': '"multipart-uploads"',
                                  'time_amount': '2', 'time_unit': '"DAYS"'})

    def test_only_expected_principals_reach_frameworks_bucket(self):
        where = "in compartment trachuan where target.bucket.name = '" + BUCKET + "'"
        self.assertEqual([s for s in statements() if BUCKET in s], [
            "Allow service objectstorage-${var.region} to manage object-family in compartment trachuan where all "
            "{any {target.bucket.name = 'trachuan-releases', target.bucket.name = 'trachuan-frameworks-releases'}, "
            "any {request.permission = 'BUCKET_INSPECT', request.permission = 'BUCKET_READ', "
            "request.permission = 'OBJECT_INSPECT', request.permission = 'OBJECT_DELETE'}}",
            'Allow group trachuan-release-upload to read buckets ' + where,
            "Allow group trachuan-release-upload to manage objects in compartment trachuan where all "
            "{target.bucket.name = '" + BUCKET + "', any {request.permission = 'OBJECT_CREATE', "
            "request.permission = 'OBJECT_OVERWRITE', request.permission = 'OBJECT_INSPECT', "
            "request.permission = 'OBJECT_READ'}}",
            'Allow group trachuan-frameworks-verify to read buckets ' + where,
            'Allow group trachuan-frameworks-verify to read objects ' + where,
            'Allow dynamic-group trachuan-host to read buckets ' + where,
            'Allow dynamic-group trachuan-host to read objects ' + where,
        ])
        self.assertEqual([s for s in statements() if 'trachuan-frameworks-verify' in s],
                         ['Allow group trachuan-frameworks-verify to read buckets ' + where,
                          'Allow group trachuan-frameworks-verify to read objects ' + where])
