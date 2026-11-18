"""Opt-in real PostgreSQL backup acceptance, always in an owned temporary cluster.

PERSONA_TEST_POSTGRES_BIN=/path/to/postgres/bin python3 -B -m unittest -v scripts/test/test_web_development_postgres.py
No existing database or network listener is used. The binaries must already be installed.
"""
import ctypes
import ctypes.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import time
import unittest
from urllib.parse import quote, urlencode

ROOT = Path(__file__).resolve().parents[2]
BIN = os.environ.get("PERSONA_TEST_POSTGRES_BIN")


@unittest.skipUnless(BIN, "set PERSONA_TEST_POSTGRES_BIN for isolated real PostgreSQL acceptance")
class PostgreSQLBackup(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="persona-backup-postgres-")
        cls.addClassCleanup(cls.temp.cleanup)
        cls.root = Path(cls.temp.name)
        cls.socket = Path('/tmp/persona-backup-pg-' + str(os.getpid()))
        cls.socket.symlink_to(cls.root, target_is_directory=True)
        cls.addClassCleanup(cls.socket.unlink)
        cls.password = secrets.token_hex(16) + ":/@'\\"
        password_file = cls.root / "password"
        password_file.write_text(cls.password + "\n"); password_file.chmod(0o600)
        cls.env = {k: v for k, v in os.environ.items() if not k.startswith("PG")}
        cls.env.update(PATH=BIN + os.pathsep + cls.env["PATH"])
        def command(name, *args):
            return subprocess.run([str(Path(BIN) / name), *args], env=cls.env, capture_output=True, timeout=30)
        result = command('initdb', '-D', str(cls.root/'cluster'), '--username=fixture', '--auth-local=scram-sha-256', '--auth-host=reject', '--pwfile='+str(password_file))
        if result.returncode: raise RuntimeError("isolated initdb failed; no shared fallback allowed")
        result = command('pg_ctl', '-D', str(cls.root/'cluster'), '-l', str(cls.root/'server.log'), '-o',
                         f"-c listen_addresses='' -c unix_socket_directories='{cls.socket}' -p 54327 -c shared_buffers=16MB -c max_connections=8", '-w', 'start')
        if result.returncode: raise RuntimeError("isolated PostgreSQL start failed")
        def stop():
            result = command('pg_ctl', '-D', str(cls.root/'cluster'), '-m', 'immediate', '-w', 'stop')
            if result.returncode: raise RuntimeError("owned PostgreSQL did not stop")
        cls.addClassCleanup(stop)
        # Pin safe defaults as well: even a broken URI parser cannot reach another cluster.
        cls.env.update(PGHOST=str(cls.socket), PGPORT='54327', PGUSER='fixture', PGPASSWORD=cls.password, PGDATABASE='postgres')
        for database in ('source', 'restored_uri', 'restored_keywords'):
            result = command('psql', '-XAt', '-v', 'ON_ERROR_STOP=1', '-c', 'CREATE DATABASE '+database)
            if result.returncode: raise RuntimeError("isolated database creation failed")
        result = command('psql', '-XAt', '-v', 'ON_ERROR_STOP=1', '-d', 'source', '-c',
                         "CREATE TABLE history(id integer PRIMARY KEY, body text); INSERT INTO history VALUES (1,'synthetic saved history'), (2,'synthetic second row')")
        if result.returncode: raise RuntimeError("synthetic history setup failed")
        pq = ctypes.CDLL(ctypes.util.find_library('pq'))
        print('Real backup fixture:', subprocess.check_output([str(Path(BIN)/'pg_dump'), '--version'], text=True).strip(),
              'libpq', pq.PQlibVersion(), flush=True)

    def sql(self, query, database='source'):
        result = subprocess.run([str(Path(BIN)/'psql'), '-XAt', '-v', 'ON_ERROR_STOP=1', '-d', database, '-c', query],
                                env=self.env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, 'isolated SQL failed')
        return result.stdout.strip()

    def exercise(self, keywords=False):
        tag = 'keywords' if keywords else 'uri'
        app = 'persona_backup_fixture_' + tag
        if keywords:
            def escaped(value): return "'" + value.replace('\\', '\\\\').replace("'", "\\'") + "'"
            dsn = ' '.join(k+'='+escaped(v) for k,v in {'host':str(self.socket), 'port':'54327', 'user':'fixture',
                       'password':self.password, 'dbname':'source', 'sslmode':'disable', 'application_name':app,
                       'connect_timeout':'3', 'options':'-c statement_timeout=10000'}.items())
        else:
            dsn = 'postgresql://fixture:'+quote(self.password, safe='')+'@/source?'+urlencode({
                'host':str(self.socket), 'port':'54327', 'sslmode':'disable', 'application_name':app,
                'connect_timeout':'3', 'options':'-c statement_timeout=10000'}, quote_via=quote)
        runtime = self.root / tag
        (runtime/'data').mkdir(parents=True)
        (runtime/'data/history.txt').write_text('synthetic file history')
        (runtime/'hub.json').write_text(json.dumps({'database_url':dsn})); (runtime/'hub.json').chmod(0o600)
        program = runtime/'personad'; program.write_bytes(b'synthetic installed program')
        request = {'operation':'backup', 'runtime':str(runtime), 'hub_database':dsn,
                   'executables':{'personad':str(program)}, 'candidate':{'commit':'a'*40},
                   'result':str(runtime/'result.json'), 'config':{'backup':{
                       'quiesce':[sys.executable,'-c','pass'], 'resume':[sys.executable,'-c','pass']}}}
        # Hold a real table lock long enough to inspect the actual pg_dump command line.
        locker = subprocess.Popen([str(Path(BIN)/'psql'), '-qXAt', '-d', 'source'], env=dict(self.env, PGAPPNAME='persona_fixture_lock_'+tag),
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        def close_locker():
            self.sql("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name='persona_fixture_lock_"+tag+"'")
            if locker.poll() is None: locker.terminate()
            locker.wait(timeout=5)
            for stream in (locker.stdin, locker.stdout, locker.stderr): stream.close()
        self.addCleanup(close_locker)
        locker.stdin.write("BEGIN; LOCK TABLE history IN ACCESS EXCLUSIVE MODE; SELECT 'fixture-locked'; SELECT pg_sleep(30);\n")
        locker.stdin.flush()
        deadline = time.monotonic()+10
        import select
        locked = False
        while time.monotonic() < deadline:
            if select.select([locker.stdout], [], [], .2)[0] and locker.stdout.readline().strip() == 'fixture-locked':
                locked = True; break
        self.assertTrue(locked, 'synthetic table lock did not start')
        with subprocess.Popen([sys.executable,str(ROOT/'server/internal/update/webdev.py')], env=self.env,
                              stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True) as helper:
            helper.stdin.write(json.dumps(request)); helper.stdin.close(); helper.stdin = None
            try:
                deadline = time.monotonic()+15
                while time.monotonic() < deadline:
                    if helper.poll() is not None: break
                    if self.sql("SELECT count(*) FROM pg_stat_activity WHERE application_name='"+app+"' AND wait_event_type='Lock'") == '1':
                        break
                    time.sleep(.05)
                else: self.fail('real pg_dump did not reach the isolated table')
                if helper.poll() is not None:
                    stdout, stderr = helper.communicate(timeout=5)
                    self.assertEqual(helper.returncode, 0, 'real helper failed before reaching the isolated table lock (private output withheld)')
                children = Path(f'/proc/{helper.pid}/task/{helper.pid}/children').read_text().split()
                self.assertTrue(children, 'pg_dump did not remain alive for argv inspection')
                command_line = b' '.join(Path(f'/proc/{pid}/cmdline').read_bytes() for pid in children)
                self.assertIn(b'pg_dump', command_line)
                self.assertNotIn(self.password.encode(), command_line)
                self.assertNotIn(quote(self.password, safe='').encode(), command_line)
                self.assertNotIn(dsn.encode(), command_line)
                self.sql("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name='persona_fixture_lock_"+tag+"'")
                locker.wait(timeout=5)
                stdout, stderr = helper.communicate(timeout=30)
                self.assertEqual(helper.returncode, 0, 'real helper backup failed (private output withheld)')
                self.assertNotIn(self.password, stdout+stderr)
                self.assertNotIn(quote(self.password, safe=''), stdout+stderr)
            finally:
                if helper.poll() is None: helper.kill(); helper.wait(timeout=5)
        receipt = json.loads((runtime/'result.json').read_text())
        self.assertNotIn(self.password, json.dumps(receipt))
        archive = runtime/'update/development-backups'/receipt['id']/'database-0.dump'
        result = subprocess.run([str(Path(BIN)/'pg_restore'), '--exit-on-error', '--no-owner', '--no-acl',
                                 '--dbname=restored_'+tag, str(archive)], env=self.env, capture_output=True, timeout=20)
        self.assertEqual(result.returncode, 0, 'real custom dump could not be restored into the fresh synthetic database')
        expected='1|synthetic saved history\n2|synthetic second row'
        self.assertEqual(self.sql('SELECT * FROM history ORDER BY id', 'restored_'+tag), expected)
        self.assertEqual(self.sql('SELECT * FROM history ORDER BY id', 'source'), expected)
        server_log = (self.root/'server.log').read_text()
        self.assertNotIn(self.password, server_log)
        self.assertNotIn(quote(self.password, safe=''), server_log)

    def test_invalid_uri_is_rejected_before_writer_pause_without_secret_output(self):
        runtime = self.root / 'invalid'
        (runtime/'data').mkdir(parents=True)
        dsn = 'postgresql://fixture:'+quote(self.password, safe='')+'@/source?'+urlencode({
            'host':str(self.socket), 'port':'54327', 'unsupported_parameter':self.password}, quote_via=quote)
        (runtime/'hub.json').write_text(json.dumps({'database_url':dsn})); (runtime/'hub.json').chmod(0o600)
        paused = runtime/'paused'
        request = {'operation':'backup', 'runtime':str(runtime), 'hub_database':dsn,
                   'candidate':{'commit':'a'*40}, 'result':str(runtime/'result.json'),
                   'config':{'backup':{'quiesce':[sys.executable,'-c',"import pathlib; pathlib.Path("+repr(str(paused))+").touch()"],
                                       'resume':[sys.executable,'-c','pass']}}}
        result = subprocess.run([sys.executable,str(ROOT/'server/internal/update/webdev.py')],
                                env=self.env, input=json.dumps(request), capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(self.password, result.stdout+result.stderr)
        self.assertNotIn(quote(self.password, safe=''), result.stdout+result.stderr)
        self.assertFalse(paused.exists())
        self.assertFalse((runtime/'update/development-backups').exists())

    def test_uri_query_credentials_and_real_dump_restore(self): self.exercise()
    def test_keyword_parameters_and_real_dump_restore(self): self.exercise(keywords=True)

    def test_named_assistant_database_takes_precedence_over_saved_url(self):
        self.sql('CREATE DATABASE manager_source', 'postgres')
        self.sql('CREATE DATABASE restored_manager', 'postgres')
        self.sql("CREATE TABLE history(body text); INSERT INTO history VALUES ('synthetic manager history')", 'manager_source')
        dsn = 'postgresql://fixture:'+quote(self.password, safe='')+'@localhost/source?'+urlencode({
            'host':str(self.socket), 'port':'54327', 'sslmode':'disable'}, quote_via=quote)
        runtime = self.root / 'manager-inventory'
        manager = runtime / 'managers/assistant'
        manager.mkdir(parents=True)
        (runtime/'hub.json').write_text(json.dumps({'database_url':dsn}))
        # The built-in manager provisions database_name on the effective Hub
        # server before considering a previously saved database_url/container.
        (manager/'manager.json').write_text(json.dumps({
            'database_name':'manager_source', 'database_url':dsn,
            'moved_to_own_database':{'at':'2026-11-16T00:00:00Z'}}))
        program = runtime/'personad'; program.write_bytes(b'synthetic installed program')
        request = {'operation':'backup', 'runtime':str(runtime), 'hub_database':dsn,
                   'executables':{'personad':str(program)}, 'candidate':{'commit':'a'*40},
                   'result':str(runtime/'result.json'), 'config':{'backup':{
                       'quiesce':[sys.executable,'-c','pass'], 'resume':[sys.executable,'-c','pass']}}}
        result = subprocess.run([sys.executable,str(ROOT/'server/internal/update/webdev.py')],
                                env=self.env, input=json.dumps(request), capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, 'real manager backup failed (private output withheld)')
        self.assertNotIn(self.password, result.stdout+result.stderr)
        receipt = json.loads((runtime/'result.json').read_text())
        snapshot = runtime/'update/development-backups'/receipt['id']
        self.assertEqual(json.loads((snapshot/'manifest.json').read_text())['databases'], 2,
                         'backup omitted the live named manager database')
        restored = subprocess.run([str(Path(BIN)/'pg_restore'), '--exit-on-error', '--no-owner', '--no-acl',
                                   '--dbname=restored_manager', str(snapshot/'database-1.dump')],
                                  env=self.env, capture_output=True, timeout=20)
        self.assertEqual(restored.returncode, 0, 'manager dump could not be restored')
        self.assertEqual(self.sql('SELECT body FROM history', 'restored_manager'), 'synthetic manager history')
        self.assertEqual(self.sql('SELECT body FROM history', 'manager_source'), 'synthetic manager history')


if __name__ == '__main__': unittest.main()
