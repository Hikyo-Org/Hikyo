"""Pure future operator sequencing. No loader, constructor, I/O or authority.

Root supplies an already configured, independently authenticated Operator and
modules. Delegated Operator calls retain their original registry/CAS gates.
This wrapper neither installs native code nor performs a provider request.
"""
import copy


BINDING_KEYS = {'key', 'job', 'budget', 'code', 'owner', 'historicalAbsence',
                'firstMutationEvidence'}


class FutureDriver:
    def __init__(self, operator, model, deadline, key_normalizer, prepared,
                 explicit_clock):
        self.op, self.m, self.deadline = operator, model, deadline
        self.kn, self.prepared, self.now = key_normalizer, copy.deepcopy(prepared), explicit_clock
        self._configured()

    def _configured(self):
        m = self.m
        m.configuration_checked()  # Production None must refuse, never a default grant.
        m.require(callable(self.now) and self.op.m is m and self.op.now is self.now
                  and type(self.op.modules) is dict and self.op.modules.get('model') is m
                  and m.same(self.op.prepared, self.prepared),
                  'same-configured-operator-model-preparation-explicit-clock')

    def _call(self, authority, proof):
        self._configured()
        self.m.require(authority is not None and type(authority) is not bool
                       and type(proof) is dict, 'explicit-Root-authority-and-whole-proof')
        now = self.now()
        self.m.clock(now)
        return now

    def _preclaim(self, original_window, now):
        m = self.m
        m.require(type(original_window) is dict and 'observedAt' in original_window
                  and 'minimumRemainingMS' in original_window, 'whole-original-preclaim-window')
        reconstructed = self.deadline.preclaim_window(
            m, self.prepared, explicit_now=m.instant(original_window['observedAt']),
            minimum_remaining_ms=original_window['minimumRemainingMS'])
        m.require(m.same(reconstructed, original_window)
                  and m.instant(original_window['observedAt']) <= now,
                  'original-preclaim-window-not-renewed')

    def reserve(self, authority, proof):
        now = self._call(authority, proof)
        window = self.deadline.preclaim_window(
            self.m, self.prepared, explicit_now=now, minimum_remaining_ms=120000)
        reservation = self.op.reserve(authority, proof)
        return {'preclaimWindow': window, 'reservation': reservation}

    def arm_key(self, authority, exact_request, proof, *, preclaim_window):
        now = self._call(authority, proof)
        self._preclaim(preclaim_window, now)
        self.m.first_key_forward_window(
            self.prepared, explicit_now=now, minimum_remaining_ms=60000)
        return self.op.arm_key(authority, exact_request, proof)

    def owner_contract(self, operation, authority, proof, *, preclaim_window=None):
        now = self._call(authority, proof)
        if operation == 'CreateKey':
            self._preclaim(preclaim_window, now)
            self.m.first_key_forward_window(
                self.prepared, explicit_now=now, minimum_remaining_ms=60000)
        return self.op.owner_contract(operation, authority, proof)

    def accept_owner(self, operation, authority, proof):
        self._call(authority, proof)
        # A later receipt must not pretend the original baseline was observed now.
        return self.op.accept_owner(operation, authority, proof)

    def arm_dispatch(self, authority, proof):
        self._call(authority, proof)
        # The core validates independent CURRENT dispatch records and once state.
        return self.op.arm_dispatch(authority, proof)

    def dispatch_contract(self, authority, proof):
        self._call(authority, proof)
        return self.op.dispatch_contract(authority, proof)

    def binding_records(self, records):
        """Build before Root captures/authenticates the whole seven-record proof."""
        self._configured()
        self.m.exact(records, BINDING_KEYS - {'firstMutationEvidence'})
        result = copy.deepcopy(records)
        result['firstMutationEvidence'] = self.m.first_mutation_evidence(
            self.prepared, result['key'])
        return result

    def bind(self, authority, proof):
        self._call(authority, proof)
        # Actual carriers can expose only stdoutEvidenceIndex until the core
        # registry authenticates and decodes whole original stdout. The core
        # then enforces exact seven records/reference; never inspect, parse or
        # append fields to the still-opaque carrier here.
        return self.op.bind(authority, proof)

    def create_manifests(self, full_intent, forward_contract, approved_body,
                         preclaim_window, *, redirect_limitation_reviewed):
        self._configured()
        now = self.now()
        self.m.clock(now)
        return self.deadline.create_manifests(
            self.m, self.kn, self.deadline.PINS, self.prepared, full_intent,
            forward_contract, approved_body, preclaim_window, explicit_now=now,
            minimum_remaining_ms=60000,
            redirect_limitation_reviewed=redirect_limitation_reviewed)

    def native_recheck(self, full_intent, forward_contract, bundle, guard_status,
                       observer_status, *, phase):
        self._configured()
        now = self.now()
        self.m.clock(now)
        return self.deadline.recheck(
            self.m, self.kn, self.prepared, full_intent, forward_contract,
            bundle, guard_status, observer_status, phase=phase, explicit_now=now,
            minimum_remaining_ms=60000)
