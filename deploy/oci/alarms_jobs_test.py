import pathlib
import re
import unittest


class ReleaseAlarmTest(unittest.TestCase):
    def test_release_alarms_notify_per_metric_stream(self):
        source = (pathlib.Path(__file__).parent / "alarms-jobs.tf").read_text()
        for name in ("corpus_release_failed", "corpus_release_waiting"):
            with self.subTest(alarm=name):
                match = re.search(
                    rf'resource "oci_monitoring_alarm" "{name}"\s*\{{([^}}]*)\}}',
                    source,
                )
                self.assertIsNotNone(match)
                self.assertRegex(
                    match.group(1),
                    r"(?m)^\s*is_notifications_per_metric_dimension_enabled\s*=\s*true\s*$",
                )
                body = re.search(r'(?m)^\s*body\s*=\s*"([^"]*)"', match.group(1))
                self.assertIsNotNone(body)
                self.assertIn('release.log', body.group(1))
                self.assertIn('release-frameworks.log', body.group(1))


if __name__ == "__main__":
    unittest.main()
