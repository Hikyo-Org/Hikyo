"""N/D runtime wiring from an explicitly configured parent bundle.

No fixed model loader, database discovery, provider access or native originals
are supplied. The parent retains the same configured model and readonly store.
"""
from datetime import timedelta
from pathlib import Path
import sqlite3
import time

from . import adapter_context
from . import preflight
from . import root_preflight
from . import runner


def prepare(*, model, modules, controller, values, review, plan, registry,
            status_context, status_raw, ready_raw, index_ref, clock, monotonic):
    """Return real preflight/pre-submit closures for paired_core.Attempt.run."""
    context = adapter_context.checked(model, plan, review, values['lockContext'], status_context)
    context.prepared(model, values['prepared'])
    model.require(controller.m is model and controller.now is clock
                  and model.same(controller.prepared, values['prepared']),
                  'same-configured-parent-controller-model-preparation-clock')
    ledger = controller.ledger
    root_preflight.physical_exclusion(ledger.parent)
    directory, endpoints = runner.endpoints(model, review, plan['holderContextSHA256'])
    records = root_preflight.readonly_records(model, modules['ledger'], ledger.folder, ledger.parent, directory)
    window = model.strict_json(ready_raw[:-1])
    def check():
        now = clock()
        model.clock(now)
        model.admit_current_release(values['prepared'], now)
        after = clock()
        model.clock(after)
        model.require(after >= now, 'monotonic-forward-admission-clock')
        # Same original physical lock and exact retained journal under both
        # adjacent submission checks, without renewing any existing window.
        root_preflight.physical_exclusion(ledger.parent)
        runner.window_checked(model, window, review, monotonic)
        model.require(model.instant(window['readyAt']) <= after
                      <= model.instant(window['lockAcquiredAt']) + timedelta(milliseconds=window['maximumHoldMS']),
                      'original-physical-forward-window-after-admission')
        shared = values['nativeManifestBundle']['observerManifest']
        model.require(shared['notBeforeMS'] <= modules['keyNormalizer'].ms(after)
                      <= shared['expiresAtMS'] - 60000, 'original-shared-forward-window-after-admission')
        current = root_preflight.readonly_records(model, modules['ledger'], ledger.folder, ledger.parent, directory)
        model.require(model.same(current, records), 'unchanged-current-holder-records')
        return after
    def descriptor():
        now = check()
        connection = sqlite3.connect('file:' + str(context.native_store) + '?mode=ro', uri=True)
        connection.execute('PRAGMA query_only=ON')
        try:
            return preflight.descriptor(m=model, kn=modules['keyNormalizer'], deadline=modules['guardDeadline'],
                values=values, review=review, index_ref=index_ref, ready_raw=ready_raw, **records,
                stage_registry=registry, status_context=status_context, status_raw=status_raw, connection=connection,
                source_pins=context.stage_pins(), phase=plan['phase'], explicit_now=now,
                final_clock=clock, monotonic_now=monotonic)
        finally:
            connection.close()
    return {'preflight': descriptor, 'pre_submit': check}
