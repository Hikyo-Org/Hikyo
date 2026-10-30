"""The checked-in static closure must match every current executable byte."""
import hashlib
from pathlib import Path
import unittest

import load_successor
from release_records import implementation_checked
from repin_core import repin
from seal_sources import static_files


class RepositorySourceTests(unittest.TestCase):
    def test_complete_repository_source_and_consumer_pins_are_current(self):
        root = Path(__file__).resolve().parent
        mapping, manifest, _ = load_successor.verify(
            reviewed_map_sha256=hashlib.sha256((root / 'EXECUTION-MAP.json').read_bytes()).hexdigest(),
            reviewed_manifest_raw_sha256=hashlib.sha256((root / 'IMPLEMENTATION-MANIFEST.json').read_bytes()).hexdigest())
        # Locally computed pins establish integrity for this check only. Root
        # still supplies independently reviewed pins to the operational loader.
        self.assertEqual(mapping['sourceFiles'], static_files(root))
        self.assertEqual(implementation_checked(manifest), manifest)
        repin(check=True)


if __name__ == '__main__':
    unittest.main()
