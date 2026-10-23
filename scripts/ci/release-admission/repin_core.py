"""Topologically repin current source dependencies, preserving historical ASTs.

No import, model facade, fixed point, historical rehash or source hash spoofing.
Only the named current-generation assignment literals below may change.
"""
import ast
import hashlib
from pathlib import Path

HERE = Path(__file__).resolve().parent
MODEL = 'core/next_fixture_model.source-only.py'
TEST = 'core/test_next_fixture_model.py'
EXECUTOR = 'core/next_fixture_execution.source-only.py'
LEDGER = 'core/next_fixture_ledger.source-only.py'
RESULT = 'core/next_hosted_result.source-only.py'
FINALIZER = 'core/next_owner_finalization.source-only.py'
RUNTIME = 'core/next_fixture_runtime_bridge.source-only.py'
EXECUTOR_TEST = 'test_two_release_pipeline.py'


def pin(root, relative):
    return hashlib.sha256((root / relative).read_bytes()).hexdigest()


def rewrite(root, relative, replacements, *, check=False):
    path = root / relative
    source = path.read_text()
    tree = ast.parse(source)
    edits = []
    for name, value in replacements.items():
        matches = [node for node in tree.body if isinstance(node, ast.Assign)
                   and any(isinstance(target, ast.Name) and target.id == name for target in node.targets)]
        if len(matches) != 1:
            raise ValueError('repin:exact-one-current-assignment:' + relative + ':' + name)
        node = matches[0].value
        lines = source.splitlines(keepends=True)
        start = sum(map(len, lines[:node.lineno - 1])) + node.col_offset
        end = sum(map(len, lines[:node.end_lineno - 1])) + node.end_col_offset
        edits.append((start, end, repr(value)))
    for start, end, value in sorted(edits, reverse=True):
        source = source[:start] + value + source[end:]
    # Every other top-level AST is invariant, including literal historical pins
    # and the original #1017/c03/c04 validators.
    after = ast.parse(source)
    names = set(replacements)
    def retained(nodes):
        return [ast.dump(node, include_attributes=False) for node in nodes
                if not (isinstance(node, ast.Assign) and any(isinstance(t, ast.Name)
                        and t.id in names for t in node.targets))]
    if retained(tree.body) != retained(after.body):
        raise ValueError('repin:unlisted-AST-change')
    if source != path.read_text():
        if check:
            raise ValueError('repin:stale-current-source-pins:' + relative)
        path.write_text(source)


def repin(root=HERE, *, check=False):
    """Order follows real byte dependencies. No upstream file is edited later."""
    current = {'MODEL_SHA256': MODEL, 'MODEL_TEST_SHA256': TEST, 'LEDGER_SHA256': LEDGER}
    def values(mapping):
        return {name: pin(root, path) for name, path in mapping.items()}
    for relative in ('core/normalize_next_inventory.source-only.py',
                     'core/normalize_next_key_custody.source-only.py',
                     'core/normalize_key_origin_cleanup_only.source-only.py'):
        rewrite(root, relative, values({'MODEL_SHA': MODEL, 'TEST_SHA': TEST}), check=check)
    rewrite(root, EXECUTOR, values({'MODEL_SHA256': MODEL, 'MODEL_TEST_SHA256': TEST}), check=check)
    rewrite(root, 'core/project_pending_job.source-only.py', values({'MODEL_SHA256': MODEL}), check=check)
    rewrite(root, RESULT, values({**current, 'EXECUTOR_SHA256': EXECUTOR,
                                'EXECUTOR_TEST_SHA256': EXECUTOR_TEST}), check=check)
    guards = {'next_fixture_iam_operations.source-only.mjs': 'core/next_fixture_iam_operations.source-only.mjs',
              'native_next_fixture_iam_guard.source-only.js': 'core/native/native_next_fixture_iam_guard.source-only.js',
              'setup_version_operations.source-only.mjs': 'core/setup_version_operations.source-only.mjs',
              'native_setup_version_operations_guard.source-only.js': 'core/native/native_setup_version_operations_guard.source-only.js'}
    rewrite(root, FINALIZER, {**values({'MODEL_SHA256': MODEL, 'LEDGER_SHA256': LEDGER,
        'EXECUTOR_SHA256': EXECUTOR, 'RESULT_SHA256': RESULT, 'EXECUTOR_TEST_SHA256': EXECUTOR_TEST}),
        'NATIVE_GUARD_PINS': values(guards)}, check=check)
    rewrite(root, RUNTIME, values({'MODEL_SHA256': MODEL, 'LEDGER_SHA256': LEDGER,
                                  'EXECUTOR_SHA256': EXECUTOR}), check=check)
    operator = {'model': MODEL, 'executor': EXECUTOR, 'ledger': LEDGER, 'runtime': RUNTIME,
        'result': RESULT, 'finalizer': FINALIZER}
    rewrite(root, 'core/next_operator_orchestration.source-only.py', {'PINS': values(operator)}, check=check)
    stdout = 'core/next_operator_orchestration_stdout.source-only.py'
    rewrite(root, stdout, {'PINS': values(operator)}, check=check)
    rewrite(root, 'core/future_key_guard_deadline.source-only.py', {'PINS': values({
        'modelSHA256': MODEL, 'modelTestSHA256': TEST,
        'keyNormalizerSHA256': 'core/normalize_next_key_custody.source-only.py',
        'guardSHA256': 'core/native/native_owner_kms_xhr_guard.source-only.js',
        'observerSHA256': 'core/native/native_next_key_custody_prearm_observer.source-only.js'})}, check=check)
    partial = {**operator, 'operator': 'core/next_operator_orchestration.source-only.py', 'metadata': 'core/partial_metadata_bridge.source-only.py',
        'keyFacts': 'core/normalize_key_origin_cleanup_only.source-only.py', 'stdout': stdout}
    partial.pop('result')
    rewrite(root, 'core/partial_setup_cleanup_inventory.source-only.py', {'PINS': values(partial)}, check=check)
    assembly = {'model': MODEL, 'inventory': 'core/normalize_next_inventory.source-only.py',
        'setup': 'core/normalize_setup_whole_reads.source-only.py',
        'inventoryObserver': 'core/native/native_next_inventory_prearm_observer.source-only.js',
        'setupObserver': 'core/native/native_setup_whole_read_prearm_observer.source-only.js',
        'historicalObserver': 'core/native/native_historical_describe_observer_per_preparation.source-only.js',
        'historicalGate': 'core/native/native_historical_describe_transport_gate_per_preparation.source-only.js'}
    rewrite(root, 'core/admission_assembly.source-only.py',
        {'PINS': {name: (path, pin(root, path)) for name, path in assembly.items()}}, check=check)
    return values({'model': MODEL, 'test': TEST, 'executor': EXECUTOR, 'result': RESULT,
                   'finalizer': FINALIZER, 'runtime': RUNTIME})


if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    repin(check=args.check)
