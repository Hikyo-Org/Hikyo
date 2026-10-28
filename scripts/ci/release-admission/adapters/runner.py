"""Concrete local FIFO owner. No browser, command launcher or phase executor."""

from . import adapter_context
import copy
import json
import os
from pathlib import Path
import time

from . import create_holder
from .create_custody import Custody
from . import local_files as files
from . import pipe_io

INPUTS = {'prepared', 'fullIntent', 'forwardContract', 'nativeManifestBundle',
          'guardStatus', 'observerStatus', 'lockContext', 'sourceBinding'}
READY = 'ready.fifo'
RELEASE = 'release.fifo'
ORIGINAL = 'native-original.raw.json'


def state_only_controller(executor, m, ledger, prepared, sources, clock):
    class RefuseProof(executor.RootProofAdapter):
        def authenticate(self, *args, **kwargs):
            raise RuntimeError('holder:proof-and-phase-execution-refused')
    # The exact reviewed constructor validates model/preparation/source links.
    # This private state-only facade exposes no controller phase methods.
    concrete = executor.FixtureController(m, ledger, prepared, sources, RefuseProof(), clock)
    class StateOnly:
        pass
    result = StateOnly()
    for name in ('m', 'ledger', 'prepared', 'sources', 'now'):
        setattr(result, name, getattr(concrete, name))
    return result


def review_context(m, values, index_ref, *, runtime_dir, sequence_min, sequence_max,
                   source_manifest_sha, python_ref):
    context = adapter_context.checked(m, values['lockContext'], values['sourceBinding'], values['fullIntent'], values['forwardContract'], values['forwardContract']['ticket'])
    context.prepared(m, values['prepared'])
    m.exact(values, INPUTS)
    files.plain_path(runtime_dir)
    m.require(type(sequence_min) is int and type(sequence_max) is int
              and 0 <= sequence_min <= sequence_max <= sequence_min + 50000,
              'bounded-fixed-current-thread-sequence-window')
    custody = Custody(values['sourceBinding'])
    custody.command_context_checked(m, values['lockContext']['chromeCommand'])
    m.require(m.sha(source_manifest_sha), 'reviewed-extension-source-pin')
    return {adapter_context.FIELD: context.binding_sha256, 'schema': 'hikyo-158/persistent-create-holder-review/v1',
            'sourceManifestRawSHA256': source_manifest_sha, 'python': copy.deepcopy(python_ref),
            'inputIndex': copy.deepcopy(index_ref), 'inputDigests': {k: m.digest(v) for k, v in values.items()},
            'lockContext': copy.deepcopy(values['lockContext']),
            'sourceBinding': copy.deepcopy(values['sourceBinding']),
            'runtimeDirectory': str(runtime_dir), 'nativeOriginalPath': str(Path(runtime_dir) / ORIGINAL),
            'readyPath': str(Path(runtime_dir) / READY), 'releasePath': str(Path(runtime_dir) / RELEASE),
            'sequenceMin': sequence_min, 'sequenceMax': sequence_max, 'maximumHoldMS': 55000,
            'nativeStore': str(context.native_store),
            'externalRootAuthenticationRequiredBeforeRelease': True,
            'externalRootExclusiveControlRequired': True, 'runtimeAdoptionEstablished': False}


def initialize(m, review, expected_context_sha):
    adapter_context.checked(m, review, review['lockContext'])
    m.require(m.digest(review) == expected_context_sha, 'explicit-reviewed-complete-context-pin')
    directory = files.plain_path(review['runtimeDirectory'])
    # One fresh directory, never reuse or reset a prior owner run.
    directory.mkdir(mode=0o700, exist_ok=False)
    files.directory_checked(directory)
    files.exclusive(directory / 'start.once.json', m.canonical({'contextSHA256': expected_context_sha}))
    opened = []
    try:
        identities = {}
        for leaf in (READY, RELEASE):
            os.mkfifo(directory / leaf, 0o600)
            os.chmod(directory / leaf, 0o600)
            fd, identity = files.open_fifo(directory / leaf)
            opened.append(fd); identities[leaf] = identity
        descriptor = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema': 'hikyo-158/persistent-create-holder-endpoints/v1',
                      'contextSHA256': expected_context_sha, 'review': review, 'fifos': identities,
                      'directory': files.directory_info(directory)}
        files.exclusive(directory / 'descriptor.json', m.canonical(descriptor))
        return directory, descriptor, opened[0], opened[1]
    except BaseException:
        for fd in opened: os.close(fd)
        raise


def start(*, m, controller, deadline, key_normalizer, values, review, expected_context_sha,
          connection, clock, monotonic=time.monotonic):
    directory, descriptor, ready_fd, release_fd = initialize(m, review, expected_context_sha)
    try:
        result = create_holder.hold_create_key_window(controller=controller, model=m, deadline=deadline,
            key_normalizer=key_normalizer, context=values['lockContext'], full_intent=values['fullIntent'],
            forward_contract=values['forwardContract'], bundle=values['nativeManifestBundle'],
            guard_status=values['guardStatus'], observer_status=values['observerStatus'],
            release_fd=release_fd, ready_fd=ready_fd, explicit_clock=clock, monotonic_clock=monotonic,
            maximum_hold_ms=review['maximumHoldMS'], native_connection=connection,
            sequence_min=review['sequenceMin'], sequence_max=review['sequenceMax'],
            custody=Custody(values['sourceBinding']))
        output = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema': 'hikyo-158/persistent-create-holder-final/v1',
                  'contextSHA256': expected_context_sha, 'outcome': 'validated-release', 'result': result,
                  'processExitVerifiedByRecord': False}
        files.exclusive(directory / 'final.json', m.canonical(output))
        return output
    except BaseException:
        # No synthetic response, ledger phase, held.once or auth decision is made.
        # A missing final file after hard process death is also reconciliation only.
        failure = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema': 'hikyo-158/persistent-create-holder-final/v1',
                   'contextSHA256': expected_context_sha, 'outcome': 'held-failure-reconcile-only',
                   'processExitVerifiedByRecord': False, 'automaticRetry': False}
        if not (directory / 'final.json').exists():
            files.exclusive(directory / 'final.json', m.canonical(failure))
        raise
    finally:
        os.close(ready_fd); os.close(release_fd)


def endpoints(m, review, expected_context_sha):
    adapter_context.checked(m, review, review['lockContext'])
    m.require(m.digest(review) == expected_context_sha, 'same-reviewed-context-for-finite-client')
    directory = files.directory_checked(review['runtimeDirectory'])
    descriptor_path = directory / 'descriptor.json'
    # Descriptor has no authority: compare the complete reviewed context again.
    descriptor = m.strict_json(files.read_plain(descriptor_path, limit=65536, owned=True))
    adapter_context.checked(m, descriptor)
    m.exact(descriptor, {adapter_context.FIELD, 'schema', 'contextSHA256', 'review', 'fifos', 'directory'})
    m.exact(descriptor['fifos'], {READY, RELEASE})
    m.require(descriptor['schema'] == 'hikyo-158/persistent-create-holder-endpoints/v1'
              and descriptor['contextSHA256'] == expected_context_sha and m.same(descriptor['review'], review),
              'whole-original-endpoint-context')
    m.require(m.same(descriptor['directory'], files.directory_info(directory)), 'same-owned-runtime-directory-inode')
    m.require(not (directory / 'final.json').exists(), 'holder-already-finished-reconcile-only')
    return directory, descriptor


def window_checked(m, window, review, clock):
    adapter_context.checked(m, window, review, review['lockContext'])
    m.exact(window, {adapter_context.FIELD, 'schema', *create_holder.FALSE_FLAGS, 'lockAcquiredAt', 'readyAt', 'maximumHoldMS',
        'sendDeadlineMS', 'monotonicDeadline', 'monotonicAcquired', 'contextSHA256', 'preflightSHA256',
        'claimSHA256', 'stateSHA256', 'intentSHA256', 'ticketSHA256', 'manifestBundleSHA256', 'nativeSources'})
    context = review['lockContext']
    m.require(window['schema'] == 'hikyo-158/chrome-create-lock-window/v1'
              and all(window[k] is False for k in create_holder.FALSE_FLAGS)
              and window['contextSHA256'] == m.digest(context)
              and all(window[k] == context[k] for k in ('claimSHA256', 'stateSHA256', 'intentSHA256',
                  'ticketSHA256', 'manifestBundleSHA256'))
              and m.same(window['nativeSources'], context['nativeSources'])
              and m.sha(window['preflightSHA256']) and type(window['maximumHoldMS']) is int
              and window['maximumHoldMS'] == review['maximumHoldMS']
              and type(window['sendDeadlineMS']) is int
              and m.instant(window['lockAcquiredAt']) <= m.instant(window['readyAt'])
              and type(window['monotonicDeadline']) in (int, float)
              and type(window['monotonicAcquired']) in (int, float)
              and window['monotonicAcquired'] >= 0
              and window['monotonicDeadline'] == window['monotonicAcquired'] + review['maximumHoldMS'] / 1000,
              'same-complete-finite-physical-window')
    pipe_io.remaining(window['monotonicDeadline'], clock)


def ready(m, review, expected_context_sha, *, clock=time.monotonic, wait_ms=5000):
    m.require(type(wait_ms) is int and 1 <= wait_ms <= 5000, 'finite-ready-wait')
    directory, descriptor = endpoints(m, review, expected_context_sha)
    files.exclusive(directory / 'ready.once.json', m.canonical({'contextSHA256': expected_context_sha}))
    fd, _ = files.open_fifo(directory / READY, descriptor['fifos'][READY])
    try:
        raw = pipe_io.read_frame(fd, limit=4096, stop=clock() + wait_ms / 1000, clock=clock)
        window = m.strict_json(raw[:-1])
        window_checked(m, window, review, clock)
        files.exclusive(directory / 'ready.raw.json', raw)
        return raw
    finally:
        os.close(fd)


def release(*, m, key_normalizer, values, review, expected_context_sha, original_raw_sha, connection,
            wall_clock, monotonic=time.monotonic):
    directory, descriptor = endpoints(m, review, expected_context_sha)
    # This permanent O_EXCL burn precedes every read/validation/send attempt.
    # Concurrent, partial and failed attempts can never submit a second frame.
    files.exclusive(directory / 'release.once.json', m.canonical(
        {'contextSHA256': expected_context_sha, 'originalRawSHA256': original_raw_sha}))
    ready_path = directory / 'ready.raw.json'
    raw_ready = files.read_plain(ready_path, limit=4096, owned=True)
    window = m.strict_json(raw_ready[:-1])
    m.require(raw_ready.endswith(b'\n') and raw_ready.count(b'\n') == 1
              and window['contextSHA256'] == m.digest(values['lockContext']), 'retained-whole-ready-required')
    window_checked(m, window, review, monotonic)
    stop = window['monotonicDeadline']; pipe_io.remaining(stop, monotonic)
    original_raw = files.read_pinned(directory / ORIGINAL, original_raw_sha, limit=1_999_999)
    m.require(b'\n' not in original_raw and b'\r' not in original_raw, 'original-must-fit-single-raw-frame-no-reformatting')
    # Repeat ALL response/row/guard/chronology checks before delivery. The holder
    # independently repeats them (including fixed SQL) under the physical lock.
    checked = create_holder._settled_original_checked(m, key_normalizer,
        values['prepared'], values['fullIntent'], values['nativeManifestBundle'], values['lockContext'],
        window, original_raw, wall_clock(), native_connection=connection,
        sequence_min=review['sequenceMin'], sequence_max=review['sequenceMax'], custody=Custody(values['sourceBinding']))
    pipe_io.remaining(stop, monotonic)
    fd, _ = files.open_fifo(directory / RELEASE, descriptor['fifos'][RELEASE])
    try:
        pipe_io.write_all(fd, original_raw + b'\n', stop=stop, clock=monotonic)
        submitted = {adapter_context.FIELD: review[adapter_context.FIELD], 'schema': 'hikyo-158/persistent-create-holder-submitted/v1',
                     'contextSHA256': expected_context_sha, **checked,
                     'holderResultRequiredSeparately': True, 'processExitVerifiedByRecord': False}
        files.exclusive(directory / 'submitted.json', m.canonical(submitted))
        return submitted
    finally:
        os.close(fd)
