import glob
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from updater import deploy_corpus_sql as law
from updater import deploy_frameworks_sql as frameworks
from updater.frameworks_sql_test import frameworks_row, law_row


MIGRATIONS = Path(__file__).resolve().parents[3] / 'apps' / 'server' / 'migrations'

def find(name):
    found = shutil.which(name)
    if found:
        return found
    roots = sorted(glob.glob('/usr/lib/postgresql/*/bin') + glob.glob('/opt/homebrew/opt/postgresql@*/bin'),
                   reverse=True)
    return next((str(Path(root) / name) for root in roots if (Path(root) / name).exists()), None)


BINARIES = [find(name) for name in ('initdb', 'pg_ctl', 'postgres', 'psql')]
PREVIOUS = {'law': 'corpus_vn_20990101_1', 'frameworks': 'frameworks_20990101'}
DIGEST = 'b' * 64
SNAPSHOT = {
    'pointer': {'law': "SELECT row_to_json(t) FROM corpus_release t",
                'frameworks': "SELECT row_to_json(t) FROM frameworks_release t"},
    'retired': "SELECT coalesce(json_agg(t ORDER BY db_name), '[]') FROM retired_release t",
    'journal': "SELECT coalesce(json_agg(t ORDER BY id), '[]') FROM release_journal t",
}


def up(path):
    return path.read_text().split('-- +goose Down')[0]


@unittest.skipUnless(all(BINARIES) or os.environ.get('CI'), 'PostgreSQL client and server programs are not installed')
class FinishRepeatTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not all(BINARIES):
            raise AssertionError('PostgreSQL programs are required in CI')
        cls.work = tempfile.mkdtemp(prefix='finish-sql-', dir='/tmp')
        cls.addClassCleanup(shutil.rmtree, cls.work, True)
        initdb, pg_ctl, postgres, cls.psql = BINARIES
        cls.bin = str(Path(postgres).parent)
        data = Path(cls.work) / 'data'
        subprocess.run([initdb, '-D', str(data), '-A', 'trust', '-U', 'postgres', '-E', 'UTF8'],
                       check=True, capture_output=True)
        cls.pg_ctl, cls.data = pg_ctl, str(data)
        cls.addClassCleanup(cls.stop)
        env = dict(os.environ, PATH=cls.bin + os.pathsep + os.environ['PATH'])
        subprocess.run([pg_ctl, '-D', cls.data, '-o', f"-c listen_addresses='' -k {cls.work}", '-w', 'start',
                        '-l', str(Path(cls.work) / 'log')], check=True, capture_output=True, env=env)
        cls.run_sql('postgres', 'CREATE DATABASE auth;')
        for path in sorted(MIGRATIONS.glob('*.sql')):
            cls.run_sql('auth', up(path))

    @classmethod
    def stop(cls):
        subprocess.run([cls.pg_ctl, '-D', cls.data, '-m', 'immediate', '-w', 'stop'], capture_output=True)

    @classmethod
    def run_sql(cls, database, text):
        done = subprocess.run([cls.psql, '-XqAt', '-v', 'ON_ERROR_STOP=1', '-h', cls.work, '-U', 'postgres',
                               '-d', database], input=text, capture_output=True, text=True)
        if done.returncode:
            raise subprocess.CalledProcessError(done.returncode, 'psql', done.stdout, done.stderr)
        return done.stdout.strip()

    def setUp(self):
        self.run_sql('auth', 'TRUNCATE corpus_release, frameworks_release, retired_release, release_journal;')

    def snapshot(self, key):
        pointer = self.run_sql('auth', SNAPSHOT['pointer'][key])
        return (pointer, self.run_sql('auth', SNAPSHOT['retired']), self.run_sql('auth', SNAPSHOT['journal']))

    def pointer_table(self, key):
        return 'corpus_release' if key == 'law' else 'frameworks_release'

    def seed(self, key, active):
        self.run_sql('auth', f"INSERT INTO {self.pointer_table(key)} (country,active_db,activated_at,activated_by) "
                             f"VALUES ('vn','{active}','2020-01-01','activator');")

    def statement(self, key, row):
        retire = (PREVIOUS[key], DIGEST)
        return law.activate(row, 1, retire) if key == 'law' else frameworks.activate(row, retire)

    def prepared(self, key, make, mode='new'):
        row = make(mode)
        row['expected_active_db'] = PREVIOUS[key]
        self.seed(key, PREVIOUS[key])
        return row

    def forge(self, key, row, **changes):
        fields, value = law.journal_values(row, 'activated', key, key if key != 'law' else 'corpus')
        value = dict(value, **changes)
        value['manifest'] = json.dumps(value['manifest']) if isinstance(value['manifest'], dict) else value['manifest']
        columns = ','.join(fields)
        values = ','.join(law.literal(value[f]) for f in fields)
        self.run_sql('auth', f'INSERT INTO release_journal ({columns}) VALUES ({values});')

    def cases(self):
        yield 'law', law_row
        yield 'frameworks', frameworks_row

    def test_a_second_finish_after_a_committed_one_changes_nothing_and_returns_the_same_row(self):
        for key, make in self.cases():
            for mode in ('new', 'retained'):
                with self.subTest(kind=key, mode=mode):
                    self.setUp()
                    row = self.prepared(key, make, mode)
                    kind = 'law' if key == 'law' else 'frameworks'
                    self.run_sql('auth', "INSERT INTO retired_release (country,db_name,retired_at,retain_until,"
                                         f"corpus_kind) VALUES ('vn','{row['database_name']}',now(),now(),'{kind}');")
                    first = self.run_sql('auth', self.statement(key, row))
                    after_first = self.snapshot(key)
                    retired = [r['db_name'] for r in json.loads(after_first[1])]
                    wanted = [PREVIOUS[key]] if mode == 'retained' else sorted([PREVIOUS[key], row['database_name']])
                    self.assertEqual(sorted(retired), wanted)
                    second = self.run_sql('auth', self.statement(key, row))
                    self.assertEqual(self.snapshot(key), after_first)
                    self.assertEqual(json.loads(second), json.loads(first))
                    self.assertEqual([r['step'] for r in json.loads(after_first[2])], ['activated'])
                    self.assertEqual(json.loads(after_first[0])['active_db'], row['database_name'])

    def test_a_retained_repeat_does_not_delete_a_retirement_written_after_the_first_finish(self):
        for key, make in self.cases():
            with self.subTest(kind=key):
                self.setUp()
                row = self.prepared(key, make, 'retained')
                self.run_sql('auth', self.statement(key, row))
                kind = 'law' if key == 'law' else 'frameworks'
                self.run_sql('auth', "INSERT INTO retired_release (country,db_name,retired_at,retain_until,"
                                     f"corpus_kind) VALUES ('vn','{row['database_name']}',now(),now(),'{kind}');")
                before = self.snapshot(key)
                self.run_sql('auth', self.statement(key, row))
                self.assertEqual(self.snapshot(key), before)

    def test_a_first_finish_for_a_pointer_already_at_the_target_still_writes_everything(self):
        for key, make in self.cases():
            with self.subTest(kind=key):
                self.setUp()
                row = make('retained')
                row['expected_active_db'] = PREVIOUS[key]
                self.seed(key, row['database_name'])
                self.run_sql('auth', self.statement(key, row))
                _, retired, journal = self.snapshot(key)
                self.assertEqual([r['step'] for r in json.loads(journal)], ['activated'])
                self.assertEqual([r['db_name'] for r in json.loads(retired)], [PREVIOUS[key]])

    def test_an_activated_row_with_the_pointer_elsewhere_does_not_settle_the_operation(self):
        for key, make in self.cases():
            with self.subTest(kind=key):
                self.setUp()
                row = self.prepared(key, make)
                self.run_sql('auth', self.statement(key, row))
                self.run_sql('auth', f"UPDATE {self.pointer_table(key)} SET active_db='{PREVIOUS[key]}';")
                self.run_sql('auth', self.statement(key, row))
                pointer, _, journal = self.snapshot(key)
                self.assertEqual(json.loads(pointer)['active_db'], row['database_name'])
                self.assertEqual([r['step'] for r in json.loads(journal)], ['activated', 'activated'])

    def settles_nothing(self, key, row, **changes):
        self.forge(key, row, **changes)
        self.run_sql('auth', f"UPDATE {self.pointer_table(key)} SET active_db='{row['database_name']}';")
        before = len(json.loads(self.snapshot(key)[2]))
        returned = json.loads(self.run_sql('auth', self.statement(key, row)))
        journal = json.loads(self.snapshot(key)[2])
        self.assertEqual(len(journal), before + 1)
        self.assertEqual(returned['id'], journal[-1]['id'])
        for field in ('database_name', 'corpus_kind', 'operation_id', 'step', 'message'):
            self.assertEqual(returned[field], law.journal_values(row, 'activated', key,
                                                                  key if key != 'law' else 'corpus')[1][field])

    def test_a_forged_row_that_would_be_refused_never_settles(self):
        other = {'law': 'corpus_vn_20990505_1', 'frameworks': 'frameworks_20990505'}
        for key, make in self.cases():
            for name, changes in (('database', dict(database_name=other[key])),
                                  ('kind', dict(corpus_kind='frameworks' if key == 'law' else 'law')),
                                  ('digest', dict(dump_sha256='c' * 64)),
                                  ('expected', dict(expected_active_db=other[key])),
                                  ('message', dict(message='forged'))):
                with self.subTest(kind=key, forged=name):
                    self.setUp()
                    row = self.prepared(key, make)
                    if name == 'kind':
                        foreign = PREVIOUS['frameworks' if key == 'law' else 'law']
                        changes = dict(changes, database_name=foreign, expected_active_db=foreign)
                    self.settles_nothing(key, row, **changes)

    def test_a_newer_junk_row_after_the_real_one_does_not_replace_the_returned_row(self):
        for key, make in self.cases():
            with self.subTest(kind=key):
                self.setUp()
                row = self.prepared(key, make)
                first = json.loads(self.run_sql('auth', self.statement(key, row)))
                self.forge(key, row, message='forged')
                before = self.snapshot(key)
                second = json.loads(self.run_sql('auth', self.statement(key, row)))
                self.assertEqual(second, first)
                self.assertEqual(self.snapshot(key), before)

    def test_an_activated_row_of_another_operation_for_the_same_database_does_not_settle(self):
        for key, make in self.cases():
            with self.subTest(kind=key):
                self.setUp()
                row = self.prepared(key, make, 'retained')
                self.forge(key, dict(row, operation_id='f' * 32))
                self.run_sql('auth', f"UPDATE {self.pointer_table(key)} SET active_db='{row['database_name']}';")
                self.run_sql('auth', self.statement(key, row))
                journal = json.loads(self.snapshot(key)[2])
                self.assertEqual([r['operation_id'] for r in journal], ['f' * 32, row['operation_id']])
                self.assertEqual([r['db_name'] for r in json.loads(self.snapshot(key)[1])], [PREVIOUS[key]])
