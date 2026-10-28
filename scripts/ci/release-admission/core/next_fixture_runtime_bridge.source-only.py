"""Source-only bridge. No import I/O, default authority, login or live entry.

Root must independently authenticate original tool captures and release the
snapshot through a separately reviewed authority. Python classes and hashes do
not authenticate Root. There is deliberately no register/verified/echo API.
"""
from abc import ABC, abstractmethod
import copy
import hashlib
import os
from pathlib import Path
import selectors
import signal
import subprocess
import time

MODEL_SHA256 = 'f40c07b2204c3305b434836015c99f30af4dfc893a0ce0e4f0318c17ea844a19'
EXECUTOR_SHA256 = '57c31fce3d373d770930e5e01f4b1a0fb27c4128983c78eb18b0c08a4f27a85c'
LEDGER_SHA256 = 'c93ebd62f4304b880d29eb24812ccbf4c567d803970db1b11f27ff834f55a297'
MAX_STDOUT, MAX_STDERR = 2_000_000, 32768
AWS_LAUNCHER = '/opt/homebrew/Cellar/awscli/2.38.1/libexec/bin/aws'
AWS_LAUNCHER_SHA256 = '4d6834117ccdcd45ca16fb5ab765baaf8557a89c856568e9e48e854045af582c'


def source_pin(module, expected):
    if hashlib.sha256(Path(module.__file__).read_bytes()).hexdigest() != expected:
        raise ValueError('runtime-bridge:reviewed-source-drift')


class RootCaptureAuthority(ABC):
    """External Root decision, not an authentication implementation.

    No live implementation is supplied. A release must originate only after
    Root observes/authenticates the original direct tool results, exact public
    projections, current context, installed executable and complete argv/env.
    This method receives no proposed bundle that could simply be echoed.
    Caller-written subclasses are not evidence of Root authority.
    """
    @abstractmethod
    def release_root_reviewed_snapshot(self):
        raise NotImplementedError('A real external Root capture decision is required')


class OriginalCaptureRegistry:
    """Read-only snapshot; no append, register, refresh or verified-flag API."""
    def __init__(self, model, authority, explicit_clock):
        source_pin(model, MODEL_SHA256)
        model.require(isinstance(authority, RootCaptureAuthority) and callable(explicit_clock), 'external-root-authority-required')
        self.m, self.now = model, explicit_clock
        snapshot = copy.deepcopy(authority.release_root_reviewed_snapshot())
        model.exact(snapshot, {'schema', 'releasedAt', 'bridgeSHA256', 'captures', 'transportScopes'})
        model.require(snapshot['schema'] == 'hikyo-158/root-original-capture-release/v1'
            and snapshot['bridgeSHA256'] == hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'whole-reviewed-bridge-release')
        model.fresh(snapshot['releasedAt'], self.now())
        model.require(type(snapshot['captures']) is list and len(snapshot['captures']) <= 128
            and type(snapshot['transportScopes']) is list and len(snapshot['transportScopes']) <= 2
            and len(model.canonical(snapshot)) <= 2_000_000, 'bounded-root-snapshot')
        self._snapshot = snapshot
        seen = set()
        for capture in snapshot['captures']:
            model.exact(capture, {'captureId', 'capturedAt', 'context', 'bundle', 'originals'})
            model.require(type(capture['captureId']) is str and 8 <= len(capture['captureId']) <= 128
                and capture['captureId'] not in seen, 'distinct-original-capture')
            seen.add(capture['captureId']);self._originals(capture)
            model.require(model.instant(capture['capturedAt']) <= model.instant(snapshot['releasedAt']), 'capture-before-root-release')

    def _originals(self, capture):
        m = self.m;m.fresh(capture['capturedAt'], self.now())
        bundle = capture['bundle'];m.exact(bundle, {'schema', 'kind', 'observedAt', 'records', 'rawEvidence'})
        m.require(bundle['schema'] == 'hikyo-158/next-external-proof-bundle/v1'
            and type(capture['context']) is dict and type(bundle['rawEvidence']) is list
            and 1 <= len(bundle['rawEvidence']) <= 128 and type(capture['originals']) is list
            and len(capture['originals']) == len(bundle['rawEvidence']), 'complete-original-correspondence')
        for original, raw in zip(capture['originals'], bundle['rawEvidence']):
            m.exact(original, {'source', 'operation', 'selector', 'receivedAt', 'toolName', 'toolCallId', 'toolResult', 'responsePath'})
            m.exact(raw, {'source', 'operation', 'selector', 'response', 'responseSHA256', 'observedAt'})
            m.require(original['source'] in ('native-owner-console', 'github-api', 'official-cli', 'public-source')
                and original['source'] == raw['source'] and original['operation'] == raw['operation']
                and m.same(original['selector'], raw['selector']) and type(original['toolName']) is str
                and 1 <= len(original['toolName']) <= 256 and type(original['toolCallId']) is str
                and 8 <= len(original['toolCallId']) <= 256 and type(original['responsePath']) is list
                and len(original['responsePath']) <= 32 and type(original['toolResult']) in (dict, list), 'original-tool-selector')
            m.fresh(original['receivedAt'], self.now())
            m.require(m.instant(raw['observedAt']) <= m.instant(original['receivedAt']) <= m.instant(capture['capturedAt']), 'original-capture-chronology')
            projected = original['toolResult']
            for key in original['responsePath']:
                m.require((type(projected) is dict and type(key) is str and key in projected)
                    or (type(projected) is list and type(key) is int and 0 <= key < len(projected)), 'exact-original-response-path')
                projected = projected[key]
            m.require(m.same(projected, raw['response']) and raw['responseSHA256'] == m.digest(projected), 'whole-original-response')

    def authenticate(self, bundle, context, now):
        m = self.m;m.clock(now);m.fresh(self._snapshot['releasedAt'], now)
        matches = [row for row in self._snapshot['captures'] if m.same(row['bundle'], bundle) and m.same(row['context'], context)]
        m.require(len(matches) == 1, 'exact-root-reviewed-original-and-context-required')
        self._originals(matches[0])
        return copy.deepcopy(matches[0]['bundle'])

    def require_transport_scope(self, scope):
        self.m.fresh(self._snapshot['releasedAt'], self.now())
        self.m.require(sum(self.m.same(row, scope) for row in self._snapshot['transportScopes']) == 1, 'exact-root-reviewed-cli-scope')


def captured_proof_adapter(executor, registry):
    """Concrete implementation of the pinned executor's abstract adapter."""
    source_pin(executor, EXECUTOR_SHA256)
    if type(registry) is not OriginalCaptureRegistry:
        raise ValueError('runtime-bridge:original-registry-required')
    class CapturedRootProofAdapter(executor.RootProofAdapter):
        def authenticate(self, bundle, context, explicit_now):
            return registry.authenticate(bundle, context, explicit_now)
    return CapturedRootProofAdapter()


def cli_scope(executor, cli, phase):
    """Exact finite call envelope derived from the reviewed controller paths."""
    source_pin(executor, EXECUTOR_SHA256)
    m = cli.m
    m.require(type(cli) is executor.OfficialFixtureCLI and phase in ('provision', 'retire'), 'pinned-cli-and-phase')
    counts = {'Identity': 2 if phase == 'provision' else 1, 'User': 2 if phase == 'provision' else 1,
        'MFA': 2 if phase == 'provision' else 1, 'Roles': 2, 'Providers': 1,
        'Key': 1, 'KeyPolicy': 1, 'Rotation': 1}
    if phase == 'retire':counts['Provider'] = 1
    calls = []
    for operation, maximum in counts.items():
        calls.append({'argv': cli.arguments(operation), 'maximumCalls': maximum})
    for role in m.ROLES:
        for operation in ('Role', 'Inline', 'Attached'):
            calls.append({'argv': cli.arguments(operation, role), 'maximumCalls': 1 if phase == 'provision' else 2})
        if phase == 'retire':calls.append({'argv': cli.arguments('Policy', role), 'maximumCalls': 1})
    schedule = executor.provision_schedule(m) if phase == 'provision' else executor.retirement_schedule(m)
    for operation, role in schedule:calls.append({'argv': cli.arguments(operation, role), 'maximumCalls': 1})
    return {'schema': 'hikyo-158/root-cli-transport-scope/v1', 'phase': phase,
        'modelSHA256': MODEL_SHA256, 'executorSHA256': EXECUTOR_SHA256, 'ledgerSHA256': LEDGER_SHA256,
        'awsLauncher': {'path': '/opt/homebrew/bin/aws', 'resolvedPath': AWS_LAUNCHER, 'sha256': AWS_LAUNCHER_SHA256},
        'boundSHA256': m.digest(cli.bound), 'environment': copy.deepcopy(cli.environment), 'calls': calls}


class FiniteSubprocessEngine:
    """Actual subprocess implementation. Tests never call this engine.

    Uses only the passed environment, stdin EOF, absolute argv, no shell or
    retry. Raw stderr stays bounded in private process memory, never logged.
    """
    def __init__(self, executor, cli, scope, registry):
        cli.m.require(type(registry) is OriginalCaptureRegistry, 'engine-root-scope-required')
        cli.m.require(cli.m.same(scope, cli_scope(executor, cli, scope['phase'])), 'engine-closed-controller-scope')
        registry.require_transport_scope(scope)
        self.cli, self.scope, self.registry = cli, copy.deepcopy(scope), registry
        self.sequence = 0

    def __call__(self, argv, *, env, timeout):
        cli, m = self.cli, self.cli.m
        self.registry.require_transport_scope(self.scope)
        m.require(cli._active == self.scope['phase'] and m.same(env, self.scope['environment'])
            and type(argv) is list and argv and argv[0] == '/opt/homebrew/bin/aws'
            and sum(m.same(row['argv'], argv) for row in self.scope['calls']) == 1
            and type(timeout) in (int,float) and 0 < timeout <= 45, 'exact-engine-request')
        sequence = self.sequence + 1;stem = f"runtime-{self.scope['phase']}-{sequence:03d}"
        intent = cli.ledger.read_once(stem + '-intent.once.json')
        m.exact(intent, {'argvSHA256','boundSHA256','sentAt','maximumCalls'})
        m.require(intent['argvSHA256'] == m.digest(argv) and intent['boundSHA256'] == self.scope['boundSHA256']
            and type(intent['maximumCalls']) is int and intent['maximumCalls'] == 1, 'adjacent-durable-process-intent')
        m.fresh(intent['sentAt'], cli.now())
        allowed = {m.digest(row['argv']): row['maximumCalls'] for row in self.scope['calls']}
        burned = list(cli.ledger.folder.glob(f"runtime-{self.scope['phase']}-*-process-forward.once.json"))
        m.require(len(burned) <= sum(allowed.values()), 'finite-durable-process-count')
        counts = {}
        for path in burned:
            marker = cli.ledger.read_once(path.name)
            previous_intent = cli.ledger.read_once(path.name.replace('-process-forward.', '-intent.'))
            m.exact(marker, {'intentSHA256','maximumProcesses'})
            m.exact(previous_intent, {'argvSHA256','boundSHA256','sentAt','maximumCalls'})
            previous_key = previous_intent['argvSHA256']
            m.require(marker['intentSHA256'] == m.digest(previous_intent) and type(marker['maximumProcesses']) is int
                and marker['maximumProcesses'] == 1 and type(previous_intent['maximumCalls']) is int
                and previous_intent['maximumCalls'] == 1 and previous_intent['boundSHA256'] == self.scope['boundSHA256']
                and previous_key in allowed, 'whole-prior-process-custody')
            counts[previous_key] = counts.get(previous_key, 0) + 1
            m.require(counts[previous_key] <= allowed[previous_key], 'existing-process-budget-corruption')
        m.require(counts.get(m.digest(argv), 0) < allowed[m.digest(argv)], 'durable-per-argv-process-limit')
        cutoff = cli.bound['run']['provisioningCutoff' if self.scope['phase'] == 'provision' else 'cleanupUntil']
        remaining = (m.instant(cutoff) - cli.now()).total_seconds();m.require(remaining > 0, 'engine-deadline-before-burn')
        launcher = Path('/opt/homebrew/bin/aws').resolve(strict=True)
        m.require(str(launcher) == AWS_LAUNCHER and launcher.stat().st_size <= 65536
            and hashlib.sha256(launcher.read_bytes()).hexdigest() == AWS_LAUNCHER_SHA256, 'reviewed-installed-launcher-unchanged')
        cli.ledger.once(stem + '-process-forward.once.json', {'intentSHA256': m.digest(intent), 'maximumProcesses': 1})
        self.sequence = sequence
        # Recheck immediately after durable fsync, before the only process start.
        if self.scope['phase']=='provision':
            m.admit_current_release(cli.bound['preparation'],cli.now())
        remaining = (m.instant(cutoff) - cli.now()).total_seconds();m.require(remaining > 0, 'engine-deadline-adjacent-to-process')
        timeout = min(timeout, remaining)
        try:
            process = subprocess.Popen(argv, env=env, cwd='/', stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, shell=False, close_fds=True, start_new_session=True)
        except Exception:
            raise RuntimeError('runtime-bridge:private-process-start-failed') from None
        buffers = {'stdout': bytearray(), 'stderr': bytearray()}
        limits = {'stdout': MAX_STDOUT, 'stderr': MAX_STDERR}
        deadline = time.monotonic() + timeout;timed_out = False;overflow = False
        try:
            with selectors.DefaultSelector() as selector:
                for name in buffers:
                    stream = getattr(process, name);os.set_blocking(stream.fileno(), False)
                    selector.register(stream, selectors.EVENT_READ, name)
                while selector.get_map():
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:timed_out = True;break
                    for key, _ in selector.select(min(remaining, .25)):
                        chunk = os.read(key.fileobj.fileno(), 65536)
                        if not chunk:selector.unregister(key.fileobj);continue
                        buffer = buffers[key.data];limit = limits[key.data]
                        buffer.extend(chunk[:max(0, limit + 1 - len(buffer))])
                        if len(buffer) > limit:overflow = True;break
                    if overflow:break
            if timed_out or overflow:
                try:os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:pass
            process.wait(timeout=max(.001, deadline - time.monotonic()) if not (timed_out or overflow) else 5)
            return {'returncode': process.returncode, 'stdout': bytes(buffers['stdout']), 'stderr': bytes(buffers['stderr']),
                'timedOut': timed_out, 'overflow': overflow}
        finally:
            if process.poll() is None:
                try:os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:pass
                process.wait(timeout=5)
            process.stdout.close();process.stderr.close()


PUBLIC_FIELDS = {
 'get-caller-identity': {'Account','Arn','UserId'}, 'get-user': {'User'},
 'list-mfa-devices': {'MFADevices','IsTruncated','Marker'}, 'list-roles': {'Roles','IsTruncated','Marker'},
 'list-open-id-connect-providers': {'OpenIDConnectProviderList'},
 'get-open-id-connect-provider': {'Url','ClientIDList','ThumbprintList','CreateDate','Tags'},
 'describe-key': {'KeyMetadata'}, 'get-key-policy': {'Policy','PolicyName'},
 'get-key-rotation-status': {'KeyId','KeyRotationEnabled','RotationPeriodInDays','NextRotationDate','OnDemandRotationStartDate','PendingRotationPeriodInDays'},
 'get-role': {'Role'}, 'get-role-policy': {'RoleName','PolicyName','PolicyDocument'},
 'list-role-policies': {'PolicyNames','IsTruncated','Marker'}, 'list-attached-role-policies': {'AttachedPolicies','IsTruncated','Marker'},
 'create-open-id-connect-provider': {'OpenIDConnectProviderArn','Tags'}, 'create-role': {'Role'},
 'put-role-policy': set(), 'delete-role-policy': set(), 'delete-role': set(), 'delete-open-id-connect-provider': set(),
 'schedule-key-deletion': {'KeyId','KeyState','PendingWindowInDays','DeletionDate'}}


def no_credential_fields(model, value):
    """Additional fail-closed filter, not a substitute for public-source review."""
    forbidden = {'AccessKeyId','SecretAccessKey','SessionToken','SecretString','SecretBinary',
        'Password','password','access_token','refresh_token','id_token','client_secret','Authorization'}
    if type(value) is dict:
        model.require(not (set(value) & forbidden), 'private-field-in-metadata-response')
        for child in value.values():no_credential_fields(model, child)
    elif type(value) is list:
        for child in value:no_credential_fields(model, child)


class MetadataCLITransport:
    def __init__(self, executor, cli, phase, registry, engine=None):
        source_pin(executor, EXECUTOR_SHA256)
        cli.m.require(type(registry) is OriginalCaptureRegistry, 'reviewed-original-registry-required')
        self.cli, self.phase, self.registry = cli, phase, registry
        self.scope = cli_scope(executor, cli, phase);registry.require_transport_scope(self.scope)
        self.engine = FiniteSubprocessEngine(executor, cli, self.scope, registry) if engine is None else engine
        cli.m.require(callable(self.engine), 'explicit-finite-process-engine')
        self.counts, self.sequence = {}, 0

    def __call__(self, argv, *, env, timeout):
        cli, m = self.cli, self.cli.m
        self.registry.require_transport_scope(self.scope)
        m.require(cli._active == self.phase and m.same(env, self.scope['environment'])
            and type(argv) is list and argv and argv[0] == '/opt/homebrew/bin/aws'
            and type(timeout) in (int,float) and 0 < timeout <= 45, 'active-exact-cli-request')
        allowed = [row for row in self.scope['calls'] if m.same(row['argv'], argv)]
        m.require(len(allowed) == 1, 'closed-reviewed-cli-argv')
        key = m.digest(argv);used = self.counts.get(key, 0)
        m.require(used < allowed[0]['maximumCalls'], 'finite-cli-call-count')
        now = cli.now();m.clock(now)
        cutoff = cli.bound['run']['provisioningCutoff' if self.phase == 'provision' else 'cleanupUntil']
        remaining = (m.instant(cutoff) - now).total_seconds();m.require(remaining > 0, 'adjacent-process-deadline')
        self.sequence += 1;stem = f'runtime-{self.phase}-{self.sequence:03d}'
        cli.ledger.once(stem + '-intent.once.json', {'argvSHA256': key, 'boundSHA256': self.scope['boundSHA256'], 'sentAt': m.stamp(now), 'maximumCalls': 1})
        self.counts[key] = used + 1
        try:result = self.engine(copy.deepcopy(argv), env=copy.deepcopy(env), timeout=min(timeout, remaining))
        except Exception:
            cli.ledger.once(stem + '-outcome.once.json', {'argvSHA256': key, 'status': 'ambiguous-process-exception', 'automaticRetry': False})
            raise RuntimeError('runtime-bridge:private-process-failure-no-retry') from None
        m.exact(result, {'returncode','stdout','stderr','timedOut','overflow'})
        m.require(type(result['returncode']) is int and type(result['stdout']) is bytes and type(result['stderr']) is bytes
            and type(result['timedOut']) is bool and type(result['overflow']) is bool
            and len(result['stdout']) <= MAX_STDOUT + 1 and len(result['stderr']) <= MAX_STDERR + 1, 'bounded-process-result')
        parsed = None
        safe = not result['timedOut'] and not result['overflow']
        try:
            if result['returncode'] == 0 and safe:
                parsed = m.strict_json(result['stdout']) if result['stdout'].strip() else {}
                m.require(type(parsed) is dict and set(parsed) <= PUBLIC_FIELDS[argv[2]], 'allowlisted-public-metadata-only')
                if argv[2] == 'get-key-policy' and 'PolicyName' in parsed:
                    m.require(parsed['PolicyName'] == 'default', 'exact-default-key-policy-name')
                no_credential_fields(m, parsed)
            elif result['stdout'].strip():safe = False
        except (ValueError, KeyError):safe = False;parsed = None
        cli.ledger.once(stem + '-outcome.once.json', {'argvSHA256': key, 'receivedAt': m.stamp(cli.now()),
            'returncode': result['returncode'], 'stdoutSHA256': hashlib.sha256(result['stdout']).hexdigest(),
            'stderrSHA256': hashlib.sha256(result['stderr']).hexdigest(), 'stderrEmpty': result['stderr'] == b'',
            'timedOut': result['timedOut'], 'overflow': result['overflow'], 'publicResponse': parsed,
            'completeWireCapture': not result['overflow'], 'automaticRetry': False})
        m.require(safe, 'process-output-held-no-retry')
        # Exact stderr bytes go only to the pinned executor, which hashes them.
        # Never replace them with a marker that would falsify original custody.
        return {k: result[k] for k in ('returncode','stdout','stderr')}


def external_forward_contract(model, executor, ledger, operation, explicit_now):
    """Pure/custody contract only; never forwards or approves deployments."""
    source_pin(executor, EXECUTOR_SHA256)
    source_pin(model, MODEL_SHA256);model.clock(explicit_now)
    _, state = ledger.snapshot()
    model.require(state is not None and not state['held'], 'unheld-current-forward-state')
    if operation == 'GitHubDispatch':
        intent = ledger.read_once('dispatch-intent.once.json');ticket = ledger.read_once('dispatch-transmit.once.json')
        model.require(model.same(state['dispatch'], intent) and state['boundSHA256'] is None and state['pending'] is None, 'current-unbound-dispatch')
        model.require(intent['schema'] == 'hikyo-158/next-dispatch-intent/v1' and intent['path'] == '.github/workflows/aws-acceptance.yml'
            and intent['ref'] == 'main' and intent['inputs'] == {'reviewed_sha': intent['candidateSHA']}
            and type(intent['maximumAttempts']) is int and intent['maximumAttempts'] == 1, 'exact-manual-dispatch-contract')
        request = {'ref': intent['ref'], 'inputs': intent['inputs']}
        endpoint = 'https://api.github.com/repos/Hikyo-Org/Hikyo/actions/workflows/aws-acceptance.yml/dispatches'
    elif operation in executor.OWNER_OPERATIONS:
        intent = ledger.read_once('owner-' + operation + '-intent.once.json')
        ticket = ledger.read_once('owner-' + operation + '-transmit.once.json');request = intent['request']
        model.require(model.same(state['pending'], intent), 'current-pending-owner-forward')
        model.require(intent['schema'] == 'hikyo-158/next-owner-intent/v1' and intent['operation'] == operation, 'exact-owner-operation-contract')
        endpoint = 'https://kms.eu-central-1.amazonaws.com/' if operation == 'CreateKey' else 'https://iam.amazonaws.com/'
    else:raise ValueError('runtime-bridge:closed-external-operation')
    model.fresh(intent['armedAt'], explicit_now);model.fresh(ticket['issuedAt'], explicit_now)
    model.require(intent['attemptTrustBindingSHA256']==ticket['attemptTrustBindingSHA256']==state['attemptTrustBindingSHA256']
        ==model.digest(model.ATTEMPT_TRUST_BINDING),'same-forward-trust-binding')
    model.require(type(intent['maximumRequests']) is int and intent['maximumRequests'] == 1
        and model.instant(intent['armedAt']) <= model.instant(ticket['issuedAt']), 'one-ordered-forward-contract')
    if ticket['intentSHA256'] != model.digest(intent) or ticket['maximumRequests'] != 1 or type(ticket['maximumRequests']) is not int:
        raise ValueError('runtime-bridge:exact-burned-ticket-required')
    return {'operation': operation, 'endpoint': endpoint, 'request': copy.deepcopy(request), 'intentSHA256': model.digest(intent),
        'ticket': copy.deepcopy(ticket), 'attemptTrustBindingSHA256':intent['attemptTrustBindingSHA256'], 'maximumForwards': 1, 'automaticRetry': False,
        'runtimeReady': False, 'executionAuthorityGranted': False,
        'requiredExternalSteps': ['Root authenticates exact current operation and original source',
            'Compatible global lock and guard burn immediately adjacent to one forward',
            'Preserve complete original response custody before controller receipt/CAS',
            'Human deployment approval remains separate']}


if __name__ == '__main__':
    raise SystemExit('SOURCE ONLY: external Root capture authority and live bridges are absent')
