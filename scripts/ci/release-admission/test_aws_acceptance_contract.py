"""Run the producer/consumer contract from the trusted feedback entry point."""
from pathlib import Path
import unittest


def load_tests(_loader, _tests, _pattern):
    owner = Path(__file__).resolve().parent.parent / 'aws-acceptance'
    return unittest.TestLoader().discover(str(owner), pattern='test_*.py', top_level_dir=str(owner))
