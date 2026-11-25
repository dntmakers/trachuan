from pathlib import Path
import unittest

HOST = Path(__file__).resolve().parent


class PublicSampleConfigTest(unittest.TestCase):
    def test_server_public_sample_is_enabled(self):
        env = (HOST / 'server.env').read_text().splitlines()
        self.assertIn('SERVER_PUBLIC_SAMPLE=true', env)
        self.assertEqual(1, sum(line.startswith('SERVER_PUBLIC_SAMPLE=') for line in env))


if __name__ == '__main__':
    unittest.main()
