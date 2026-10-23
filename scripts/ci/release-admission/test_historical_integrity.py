"""Guard literal historical custody, charges, chronology and budget contracts."""
import ast
import hashlib
from pathlib import Path
import unittest

BASELINE_MODEL_SHA256 = '21a15eaf72e1507f13ad90241a33d463b97af40c5151f3a533e3f0aef6880b53'
# Exact declaration bytes from the hash-verified original above. ast.dump()
# changed its empty-field formatting between Python 3.12 and 3.13; its output
# is not a portable source fingerprint. Keep source bytes, including decorators,
# rather than regenerating historical pins for each interpreter.
EXPECTED_SOURCE_PINS = {
    'ACCOUNT': 'a0edf40dfcdc1fe6d27ccef3124e7ff1ebccd7375a88a1660d47f2fd957ea57b',
    'BANKED_DIRECTIVE': '899958cc7e111b1e678b3bc6da71db82dfc8cb59a8cdd86584526a5ee199c8ee',
    'BOOTSTRAP_1017': 'd11efba53f92329b7e3425287a7d071a350019038cc6f93bce729d9dc06757d6',
    'COMPLETED_RAW_PINS': '4c794b46e51a155c2b78f36127cf6a903313d49724da442a533fec861b7e186b',
    'CONSUMED_PROPOSAL_RAW_SHA': '35425f5e25cbe4c04c9a4df731bfd5de58194fce559d8c54571dec811905e3d0',
    'CONSUMED_SOURCE_PINS': '6bd9494194e4a578664e86547d9ad55423a0d4f6b00ba5c6a199143d1dcabd1e',
    'FROZEN': 'f5820bd0244d1ec966b6d44be806cb288a1ac12ec309978153a91fbed7e393c6',
    'HELD_ARCHIVE_REFERENCES': '5764f09e4e0bf074391c3c23015bfdebc9fa1ceda43a07a4a160be61aab6b40c',
    'HELD_CAS_RAW_PINS': '90f3aa1addad4de0ce1916dff89f6f39c9f7c924bfd4f4ac329fb1d5144ffd44',
    'HELD_RAW_PINS': '3bc9e0e4d97ccc2aef59128b81a310e9b78371dd16cd71296745df369956b592',
    'HISTORICAL_SOURCE_PINS': '785916301079c30da44ff08e143501e41467934ea775c5ac18042e81128bd987',
    'HORIZON': 'a0edf40dfcdc1fe6d27ccef3124e7ff1ebccd7375a88a1660d47f2fd957ea57b',
    'LIFETIME_RESERVATIONS': '90225576465ba31eab2693cbd2ff9c6a04d14d9555c0a6e27dcf82538e736f4e',
    'NEW_RESERVATIONS': '90225576465ba31eab2693cbd2ff9c6a04d14d9555c0a6e27dcf82538e736f4e',
    'REGION': 'a0edf40dfcdc1fe6d27ccef3124e7ff1ebccd7375a88a1660d47f2fd957ea57b',
    'RETAINED_RESERVATIONS': '90225576465ba31eab2693cbd2ff9c6a04d14d9555c0a6e27dcf82538e736f4e',
    'USER_ID': '8d183bf411542c012a3b90c9df3fcbc1c8d46239d71d4e796aa31cf9d8e55a2d',
    'affirmative_merge_checked': 'e0cf192bfd86046c94aa83b0c3a396989e53d8878c8d076f307a512b969da067',
    'banked_billing_checked': 'e25e81d62b9a5593b9ae221d8761947589255173304a9fedee7af40ff8a458a8',
    'banked_series_scope': '233023ac4ee047157522c2176d118a8b106ea043aaf1684cf74c46594f7385be',
    'budget_checked': '39e27ea7a3cef18e74db9ed4566129f3c24cb272fdcb4d32a758b30b1d4104ed',
    'completed_history_checked': '50cc339e86ffba8f51702e73b23de3ff630ef78bc834cc7644bf587e0c110a3a',
    'consumed_approval_checked': '6380d2632e042372e8b8e96ca38e821dad1a70e5d3fc7ffea22b784ec1a7f1f6',
    'episode_semantics_checked': 'dbe7c2736f8411c7e3f0b3bb5746c45b4e7dcc75c9ccf80d74799cf39e98f583',
    'future_approval_checked': '2c067b312811b33ba81c9379913bde51580d5ca0d4335f45d9e8c6feb32023a0',
    'held_history_checked': '33efd95a02b242d7be8705692a255bb934632b167fbfc739275cfcac5437688a',
    'scheduled_key_closure': '5b3282cf0e3c6b0c5f4763c80503cae81a40b267c967efbe48dc8c6dbba584ec',
}


def historical_source_pins(source):
    lines = source.splitlines(keepends=True)
    observed = {}
    for node in ast.parse(source).body:
        if isinstance(node, ast.FunctionDef):
            names = [node.name]
        elif isinstance(node, ast.Assign):
            names = [target.id for target in node.targets if isinstance(target, ast.Name)]
        else:
            names = []
        for name in names:
            if name in EXPECTED_SOURCE_PINS:
                if name in observed:
                    raise ValueError('duplicate-historical-declaration:' + name)
                first = min([node.lineno] + [decorator.lineno for decorator in
                            getattr(node, 'decorator_list', [])])
                raw = ''.join(lines[first - 1:node.end_lineno]).encode('utf-8')
                observed[name] = hashlib.sha256(raw).hexdigest()
    return observed


class HistoricalIntegrityTests(unittest.TestCase):
    @staticmethod
    def model_source():
        path = Path(__file__).with_name('core') / 'next_fixture_model.source-only.py'
        return path.read_bytes().decode('utf-8')

    def test_exact_frozen_historical_contracts_are_not_relabelled(self):
        self.assertEqual(historical_source_pins(self.model_source()), EXPECTED_SOURCE_PINS)

    def test_changed_or_missing_historical_declarations_are_rejected(self):
        source = self.model_source()
        tree = ast.parse(source)
        budget = next(node for node in tree.body
                      if isinstance(node, ast.FunctionDef) and node.name == 'budget_checked')
        lines = source.splitlines(keepends=True)
        for name, changed in {
            'changed literal': source.replace("HORIZON='2026-10-26T12:22:56Z'",
                                              "HORIZON='2026-10-27T12:22:56Z'", 1),
            'changed validator': ''.join(lines[:budget.end_lineno - 1])
                + '    return None\n' + ''.join(lines[budget.end_lineno:]),
            'missing validator': ''.join(lines[:budget.lineno - 1] + lines[budget.end_lineno:]),
            'decorated validator': ''.join(lines[:budget.lineno - 1])
                + '@unreviewed\n' + ''.join(lines[budget.lineno - 1:]),
        }.items():
            with self.subTest(name=name):
                self.assertTrue(changed != source, 'mutation must change the real declaration')
                self.assertNotEqual(historical_source_pins(changed), EXPECTED_SOURCE_PINS)

    def test_duplicate_historical_declaration_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'duplicate-historical-declaration:HORIZON'):
            historical_source_pins(self.model_source() + "\nHORIZON = 'replacement'\n")


if __name__ == '__main__':
    unittest.main()
