import base64
import io
import json
import pathlib
import unittest

import corpus_projection as projection


ROOT = pathlib.Path(__file__).resolve().parents[2]
GOLDEN = ROOT / 'internal/releasechanges/testdata'


class EncodingTest(unittest.TestCase):
    def test_Encoding_Golden(self):
        payload = projection.canonical_payload('{"z":null,"s":"\\\"\\\\\\u0001Việt","n":-2,"zero":0}'.encode())
        output = io.BytesIO()
        writer = projection.Hasher(1, 'vn', 'r', output=output)
        writer.add(1, 'document_added', 'vn:key', None, payload)
        self.assertEqual(output.getvalue(), (GOLDEN / 'projection.txt').read_bytes())
        self.assertEqual((writer.digest + '\n').encode(), (GOLDEN / 'projection.sha256').read_bytes())
        self.assertEqual(writer.counts['document_added'], 1)
        self.assertEqual(writer.counts['document_removed'], 0)

    def test_CanonicalPayload_Values(self):
        self.assertEqual(projection.canonical_payload(b'{"z":[-0,true,{"\xce\xb2":"<>&","a":"\\n"}],"a":1}'), '{"a":1,"z":[0,true,{"a":"\\u000a","β":"<>&"}]}')
        self.assertEqual(projection.canonical_payload(b'{"a":"\\ud83d\\ude00"}'), '{"a":"😀"}')
        for raw in (b'{"a":1,"a":2}', b'{"a":1.5}', b'{"a":1e2}', b'[]', b'null', b'{"a":"\\ud800"}', b'{"a":"\\udc00"}', b'{"a":"\\ud800A"}', b'{"a":"\xff"}'):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                projection.canonical_payload(raw)

    def test_Hasher_InvalidRows(self):
        cases = [(True, 1, 'document_added', 'key', '{}'), (False, 2, 'document_added', 'key', '{}'), (False, 1, 'unknown', 'key', '{}'), (False, 1, 'document_added', 'key', 'null'), (False, 1, 'document_added', '', '{}'), (False, 1, 'document_added', 'a\tb', '{}'), (False, 1, 'document_added', 'a\nb', '{}'), (False, 1, 'document_added', 'a\x00b', '{}'), (False, 1, 'document_added', '\ud800', '{}')]
        for baseline, ordinal, kind, key, payload in cases:
            with self.subTest(key=key, kind=kind, ordinal=ordinal), self.assertRaises(ValueError):
                projection.Hasher(1, 'vn', 'r', baseline=baseline).add(ordinal, kind, key, None, payload)
        writer = projection.Hasher(1, 'vn', 'r')
        writer.add(1, 'document_added', 'key', None, '{}')
        with self.assertRaises(ValueError):
            writer.add(1, 'document_added', 'key', None, '{}')

    def test_Header_Invalid(self):
        for args in [(2, 'vn', 'r', None, None), (1, '', 'r', None, None), (1, 'vn', '', None, None), (1, 'vn', 'r', '', None), (1, 'vn', 'r', None, 'A' * 64), (1, 'vn', 'r', None, 'a' * 63), (1, 'v\nn', 'r', None, None)]:
            with self.subTest(args=args), self.assertRaises(ValueError):
                projection.Hasher(*args)

    def test_Hasher_RejectsNonBooleanBaseline(self):
        for baseline in ('false', 0, 1, None, 0.0):
            with self.subTest(baseline=baseline), self.assertRaises(ValueError):
                projection.Hasher(1, 'vn', 'r', baseline=baseline)

    def test_Hasher_RejectsNonIntegerVersionAndOrdinal(self):
        for value in (True, 1.0):
            with self.assertRaises(ValueError):
                projection.Hasher(value, 'vn', 'r')
            with self.assertRaises(ValueError):
                projection.Hasher(1, 'vn', 'r').add(value, 'document_added', 'a', None, '{}')

    def test_Writer_RejectsNoncanonical(self):
        with self.assertRaises(ValueError):
            projection.Hasher(1, 'vn', 'r').add(1, 'document_added', 'a', None, '{"z":-0, "a":1}')


FIXTURE = ROOT / "apps/corpus/internal/stage/releasechange/testdata"
HEADER = {"schema": 1, "bindings": {"rel": "r", "dump_sha256": "d", "build_artifact_sha256": "b", "report_sha256": "p", "predecessor_rel": "old", "predecessor_identity_sha256": "sha256:" + "a" * 64}, "baseline": False}
DOCUMENT = {"type": "document", "key": "a", "change": "added", "cause": "membership"}


def input_bytes(header=HEADER, records=()):
    return b"\n".join(json.dumps(row, ensure_ascii=True).encode() for row in (header, *records)) + b"\n"


class SidecarTest(unittest.TestCase):
    def test_BuildBudgetDifferential(self):
        case = json.loads((FIXTURE / 'projection_budget_case.json').read_bytes())
        class Budget:
            used = 0
            def write(self, data):
                if self.used + len(data) > 128 * 1024 * 1024:
                    raise ValueError('projection exceeds byte limit')
                self.used += len(data)
        budget = Budget()
        writer = projection.Hasher(1, 'vn', 'r', output=budget)
        payload = '{"text":"' + 'x' * 3800 + '"}'
        for ordinal in range(1, case['rows'] + 1):
            writer.add(ordinal, 'document_added', f'k{ordinal:06d}', None, payload)
        with self.assertRaises(ValueError):
            writer.add(case['rows'] + 1, 'document_added', f"k{case['rows'] + 1:06d}", None, payload)
        self.assertEqual(budget.used, case['bytes'])
        self.assertEqual(writer.digest, case['sha256'])
        self.assertEqual(writer.counts['document_added'], case['rows'])

    def test_Projection_DifferentialCases(self):
        cases = json.loads((FIXTURE / 'projection_cases.json').read_text())
        for case in cases:
            with self.subTest(name=case['name']):
                source = io.BytesIO(base64.b64decode(case['input']))
                if case['refused']:
                    with self.assertRaises(ValueError):
                        projection.project(source, 'vn')
                else:
                    result = projection.project(source, 'vn')
                    self.assertEqual(result.digest, case['sha256'])
                    self.assertEqual(result.counts, case['counts'])

    def test_ProjectChanges_FixtureDigest(self):
        with (FIXTURE / "changes.jsonl").open("rb") as source:
            got = projection.project(source, "vn")
        expected = json.loads((FIXTURE / "changes.projection.json").read_text())
        self.assertEqual(got.digest, expected["sha256"])
        self.assertEqual(got.counts, expected["counts"])

    def test_ProjectStream_RejectsInvalidBindingCombinations(self):
        for baseline, predecessor, identity in ((False, 'none', 'none'), (True, 'old', 'sha256:' + 'a' * 64),
                                                 (False, 'old', 'a' * 64), (False, 'none', 'sha256:' + 'a' * 64)):
            header = dict(HEADER, baseline=baseline,
                          bindings=dict(HEADER['bindings'], predecessor_rel=predecessor,
                                        predecessor_identity_sha256=identity))
            with self.subTest(header=header), self.assertRaises(ValueError):
                projection.project(io.BytesIO(input_bytes(header)), 'vn')

    def test_Projection_ItemBudgetEscapedAstral(self):
        raw = (b'{"type":"provision","key":"a","anchor":"' + b'\\ud83d\\ude00' * 350 +
               b'","change":"added","text_sha256":{"before":null,"after":null}}')
        header = dict(HEADER, bindings=dict(HEADER['bindings'], predecessor_rel='old',
                      predecessor_identity_sha256='sha256:' + 'a' * 64))
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes(header) + raw + b'\n'), 'vn')

    def test_ProjectStream_Validation(self):
        cases = [b"{}\n", input_bytes() + b'{bad}\n',
                 input_bytes(records=[dict(DOCUMENT, type="unknown")]),
                 input_bytes(records=[{"type":"document", "key":"a"}]),
                 input_bytes(records=[dict(DOCUMENT, key="b"), DOCUMENT]),
                 input_bytes(records=[DOCUMENT, DOCUMENT]),
                 input_bytes(records=[dict(DOCUMENT, key='a' * 4096)]),
                 input_bytes(records=[{'type':'document_legal', 'key':'a',
                                       'eff_from':{'after':'2099-01-01'}}])]
        for data in cases:
            with self.subTest(data=data), self.assertRaises(ValueError):
                projection.project(io.BytesIO(data), "vn")

    def test_Projection_ClosedSchema(self):
        cases = [dict(DOCUMENT, extra=True), dict(DOCUMENT, replaced_by="b"), {"type":"document_legal", "key":"a", "eff_from":{"before":None,"after":"x","extra":True}}, {"type":"provision_validity","key":"a","anchor":"p","added":[{"status_class":"x","eff_from":None,"eff_to":None}],"removed":[]}]
        for row in cases:
            with self.subTest(row=row), self.assertRaises(ValueError):
                projection.project(io.BytesIO(input_bytes(records=[row])), "vn")
        duplicates = input_bytes().replace(b'"rel": "r"', b'"rel": "r", "rel": "s"')
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(duplicates), 'vn')
        invalid_baseline = input_bytes(dict(HEADER, baseline='false'))
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(invalid_baseline), 'vn')
        end = {'key': None, 'number': '', 'in_release': False, 'anchor': None}
        relation = {'type': 'relation', 'kind': 'x', 'method': 'm', 'source': end,
                    'target': end, 'change': 'added', 'count': 0}
        invalid_rows = [relation, dict(relation, source={}, target={}),
                        {'type': 'provision_validity', 'key': 'a', 'anchor': 'p',
                         'added': [{'status_class': 'x', 'eff_from': None, 'eff_to': None,
                                    'method': 'text', 'extra': 1}], 'removed': []}]
        for row in invalid_rows:
            with self.subTest(row=row), self.assertRaises(ValueError):
                projection.project(io.BytesIO(input_bytes(records=[row])), 'vn')
        for raw in (b'{"type":"document","key":"a","key":"b","change":"added","cause":"membership"}',
                    b'{"type":"document_legal","key":"a","eff_from":{"before":null,"before":"x","after":"d"}}'):
            with self.assertRaises(ValueError):
                projection.project(io.BytesIO(input_bytes() + raw + b'\n'), 'vn')

    def test_ProjectStream_UnsafeUnicodeAndKeys(self):
        for key in ("a\t", "a\n", "a\x00", "\ud800"):
            with self.subTest(key=key), self.assertRaises(ValueError):
                projection.project(io.BytesIO(input_bytes(records=[dict(DOCUMENT, key=key)])), "vn")
        bad_utf8 = input_bytes(records=[DOCUMENT]).replace(b'"key": "a"', b'"key": "\xff"')
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(bad_utf8), 'vn')
        bad_header = dict(HEADER, bindings=dict(HEADER['bindings'], rel='r\n'))
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes(bad_header)), 'vn')

    def test_CompactContextValue_StoredBytes(self):
        key_record = b'{"type":"document_key","key":"a","before":"' + b'\\u0061' * 512 + b'"}'
        provenance = (b'{"type":"document_provenance","key":"a","text_source":'
                      b'{"before":"' + b'\\u0061' * 300 + b'","after":"' +
                      b'\\u0061' * 300 + b'"}}')
        data = input_bytes() + key_record + b'\n' + provenance + b'\n'
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(data), 'vn')

    def test_Projection_RemovedValidityHandleBudget(self):
        row = {'type': 'provision_validity', 'key': 'a', 'anchor': 'x' * 500,
               'added': [], 'removed': [{'status_class': 's' * 3100,
               'eff_from': None, 'eff_to': None, 'method': 'text'}]}
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes(records=[row])), 'vn')

    def test_Projection_LongHandleOmitted(self):
        row = {'type': 'provision_validity', 'key': 'k' * 300, 'anchor': 'a' * 300,
               'added': [], 'removed': [{'status_class': 'expired',
               'eff_from': None, 'eff_to': None, 'method': 'text'}]}
        got = projection.project(io.BytesIO(input_bytes(records=[row])), 'vn')
        self.assertEqual(len(got.rows), 1)

    def test_ProjectStream_LineLimit(self):
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes() + b" " * (1024 * 1024 + 1) + b"\n"), "vn")

    def test_Projection_ItemBudget(self):
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes(records=[dict(DOCUMENT, key="𐀀" * 512)])), "vn")

    def test_BuildWriterAndDatabaseReaderBudgetIncludesHeader(self):
        baseline = dict(HEADER, baseline=True, bindings=dict(HEADER['bindings'], predecessor_identity_sha256='none'))
        size = len(b'v1\tvn\tr\told\t\t1\n')
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes(baseline)), 'vn', row_limit=size - 1)
        self.assertEqual(projection.project(io.BytesIO(input_bytes(baseline)), 'vn', row_limit=size).rows, [])

    def test_Projection_RowBudget(self):
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes(records=[DOCUMENT])), "vn", row_limit=1)

    def test_ProjectStream_ChangingInput(self):
        class Changing(io.BytesIO):
            def __init__(self):
                super().__init__(input_bytes(records=[DOCUMENT]))
                self.rewinds = 0
            def seek(self, offset, whence=0):
                if offset == 0 and whence == 0:
                    self.rewinds += 1
                    if self.rewinds == 2:
                        self.truncate(0)
                        super().seek(0)
                        self.write(input_bytes(records=[dict(DOCUMENT, key="b")]))
                return super().seek(offset, whence)
        with self.assertRaises(ValueError):
            projection.project(Changing(), "vn")

    def test_ProjectChanges_ContextAndTuples(self):
        rows = [{"type":"document_key","key":"new","before":"old"}, {"type":"document_legal","key":"new","eff_from":{"before":None,"after":"2099-01-01"}}, {"type":"document_provenance","key":"new","baseline":{"before":True,"after":False}}, {"type":"provision_validity","key":"new","anchor":"a","added":[{"status_class":"active","eff_from":None,"eff_to":None,"method":"text"}],"removed":[]}]
        got = projection.project(io.BytesIO(input_bytes(records=rows)), "vn")
        self.assertEqual(len(got.rows), 3)
        self.assertEqual(got.rows[1].key2, "old")
        self.assertEqual(json.loads(got.rows[2].payload)["validity"]["eff_from"], None)
        baseline = dict(HEADER, baseline=True, bindings=dict(HEADER['bindings'], predecessor_identity_sha256='none'))
        self.assertEqual(projection.project(io.BytesIO(input_bytes(baseline)), "vn").rows, [])
        with self.assertRaises(ValueError):
            projection.project(io.BytesIO(input_bytes(baseline, [DOCUMENT])), "vn")
