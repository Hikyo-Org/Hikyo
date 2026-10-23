"""Guard literal historical custody, charges, chronology and budget contracts."""
import ast
import hashlib
from pathlib import Path
import unittest

BASELINE_MODEL_SHA256 = '21a15eaf72e1507f13ad90241a33d463b97af40c5151f3a533e3f0aef6880b53'
EXPECTED = {'ACCOUNT': 'd55c9498ac3c507c57e1e469118571a2da54147e35f7c319c4fd6f85b0f38325', 'BANKED_DIRECTIVE': 'adca05cce5956835f9ceb2b3aa48c855a07640d35e251e983fc383a0ad6bc269', 'BOOTSTRAP_1017': '83ef48ac75c34cf9bcf09f350d8d54722e596d592fd8fd9c5a5185f4f4adbd83', 'COMPLETED_RAW_PINS': 'b6ccf4e5ed0ab7337de3fbdf06b8551bfc4b9eade9304619c0ad0381204d0aca', 'CONSUMED_PROPOSAL_RAW_SHA': '96ff79fc55761a7f6ebcf426906f68ba9da554da63fb81fd4996f00b33a4d033', 'CONSUMED_SOURCE_PINS': '0fc75a66a9752a8c9fd45d8cf7d0c02b0fa26114d78972e06bcf117e62f62c57', 'FROZEN': '3d60ee508fe3a501e39e9642630f88fd62d6997b4582d6a4c02760f20751251a', 'HELD_ARCHIVE_REFERENCES': '33862871f640a5dbe132eead54548f0311521b324bfdadd70c9689b3d7973bd9', 'HELD_CAS_RAW_PINS': '52a5b321268d46b5fc7b8a5e735bc7ba9ca3fb466cf799cdebbaba8677351b5f', 'HELD_RAW_PINS': '07dd33ca6b9afe70871e6b1ecabc15b2740b984eb24edeb8850dc565997b944a', 'HISTORICAL_SOURCE_PINS': '1ac8146e9dbfeaaa0a152d35fec80f803e58ca36594255f5d76ad6b62702ff9b', 'HORIZON': '66a711c55aca5fcf12c6b5e4ef01f28ba0c45f991c0949aed578659c644d287f', 'LIFETIME_RESERVATIONS': 'e25f274f0ee231c364ca13a68156b794cd2a60eab6ad483670251d3fa5f09221', 'NEW_RESERVATIONS': 'f454ba958bff9ef7abc7f694ca0968402b31414795c91be3f73ea532f662611f', 'REGION': 'e6557e00673231072bc2c9c866e4ff9046656ee7be19a24876b831d83e04dfa1', 'RETAINED_RESERVATIONS': '4a01d0db072262baa4e5a3b14ea90bbbd0ab49ab8dc0cc1487f7f8951d86143f', 'USER_ID': '9f9cc78f4a23c5d2416cafec87607b7ccc4f99b82bf089d6c9aa52d2c217c1ef', 'affirmative_merge_checked': '44f2f4822de1c94f09605ad0eb75d0a5c85b8681d2555ea71c8fea2ee6479726', 'banked_billing_checked': 'f5205fd3ae90784092f7deaaefbea17768ce056ea1796aadc84334a2c4fd7175', 'banked_series_scope': '02c2eee89497b57f54480b5b1a92f3103388926b5c4b28881df0f949f2ce80ed', 'budget_checked': '790e55f5aabfa6cf3272a9f40ae95a28f8b0ba306dec28098c889272d229bdec', 'completed_history_checked': '388f7b07df522309e3c619963da1bd826755f1a4941ec7508c3d51f728241d2e', 'consumed_approval_checked': '6c8e60c6ac3d862fc492b16bc8ba3c3f7b4f1a7646ac1772284f57cc84efb1a4', 'episode_semantics_checked': '468c64b8a308b904beb25a2a17642363b395a491d43e72baf2b7889be5930c50', 'future_approval_checked': 'f292ad93ec69c34f072c725d5d298cfd1cfd6a6450172fb9bcab86510bab4b0b', 'held_history_checked': '14ca70f2c2c11bf48819f460adae5882d32db5892435876918a81b847f2a37b3', 'scheduled_key_closure': '9aa6b46e47be1f91e06e9630fc73b229280062d68b10690ec538abc73809326a'}


class HistoricalIntegrityTests(unittest.TestCase):
    def test_exact_frozen_historical_contracts_are_not_relabelled(self):
        path=Path(__file__).with_name('core')/'next_fixture_model.source-only.py'
        observed={}
        for node in ast.parse(path.read_bytes()).body:
            names=[node.name] if isinstance(node,ast.FunctionDef) else [t.id for t in node.targets if isinstance(t,ast.Name)] if isinstance(node,ast.Assign) else []
            for name in names:
                if name in EXPECTED:
                    observed[name]=hashlib.sha256(ast.dump(node,include_attributes=False).encode()).hexdigest()
        self.assertEqual(observed,EXPECTED)


if __name__=='__main__':
    unittest.main()
