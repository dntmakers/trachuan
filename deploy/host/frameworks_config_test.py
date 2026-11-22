from pathlib import Path
import unittest

HOST = Path(__file__).resolve().parent


class FrameworksConfigTest(unittest.TestCase):
    def test_server_mounts_frameworks_key(self):
        server = (HOST / 'quadlet' / 'trachuan-server.container').read_text().splitlines()
        self.assertIn('Secret=server_frameworks_key,type=mount,target=/run/secrets/server_frameworks_key,uid=65532,gid=65532,mode=0400', server)

    def test_server_frameworks_default_is_disabled(self):
        env = (HOST / 'server.env').read_text().splitlines()
        self.assertIn('SERVER_FRAMEWORKS=false', env)

    def test_server_frameworks_key_path(self):
        env = (HOST / 'server.env').read_text().splitlines()
        self.assertIn('SERVER_FRAMEWORKS_KEY_FILE=/run/secrets/server_frameworks_key', env)


if __name__ == '__main__':
    unittest.main()
