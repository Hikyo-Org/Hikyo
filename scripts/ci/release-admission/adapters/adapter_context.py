"""One immutable configured binding for the actual portable outer consumers."""
import copy
from pathlib import Path

ACTIVE = None
FIELD = 'attemptTrustBindingSHA256'


class Context:
    def __init__(self, model, admission, binding, manifest, *, source_check,
                 runtime_root, native_store, node_path, transport_root):
        self.model, self.admission = model, admission
        self._binding, self._manifest = copy.deepcopy(binding), copy.deepcopy(manifest)
        self.binding_sha256 = model.digest(self._binding)
        self.source_check = source_check
        self.runtime_root, self.native_store = Path(runtime_root), Path(native_store)
        self.node_path, self.transport_root = Path(node_path), Path(transport_root)
        for path in (self.runtime_root, self.native_store, self.node_path, self.transport_root):
            model.require(path.is_absolute() and '..' not in path.parts
                          and not any(p.is_symlink() for p in (path, *path.parents)),
                          'explicit-plain-adapter-runtime-path')
        self.checked(model)

    @property
    def binding(self):
        return copy.deepcopy(self._binding)

    @property
    def manifest(self):
        return copy.deepcopy(self._manifest)

    def checked(self, model, *packets):
        model.require(model is self.model, 'same-configured-successor-model-instance')
        self.source_check()
        model.require(model.RELEASE_ADMISSION is self.admission
                      and model.same(model.attempt_binding_checked(self._manifest), self._binding),
                      'same-configured-model-release-and-attempt')
        self.admission.attempt_binding_checked(self._binding, self._manifest)
        model.require(self.binding_sha256 == model.digest(self._binding), 'immutable-attempt-binding')
        model.require(model.digest(self.admission.configuration) == self._binding['releaseConfigurationSHA256'],
                      'same-configured-release-admission')
        for packet in packets:
            model.require(type(packet) is dict and packet.get(FIELD) == self.binding_sha256,
                          'same-attempt-trust-binding-on-real-consumer')
        return self

    def prepared(self, model, prepared):
        self.checked(model, prepared)
        model.preparation_checked(prepared)
        model.require(model.same(prepared['proposal']['sourceManifest'], self._manifest),
                      'same-original-attempt-source-manifest')
        return self

    def forward(self, model, now, *packets):
        self.checked(model, *packets)
        model.clock(now)
        return self.admission.admit_now(self._binding, self._manifest, now,
                                       reserved_binding=self._binding)

    def cleanup(self, model, action_at, now, *packets):
        self.checked(model, *packets)
        model.clock(action_at)
        model.clock(now)
        return self.admission.reconstruct_original_for_cleanup(
            self._binding, self._manifest, action_at, now)

    def stage_pins(self):
        return self.source_check()['stagePins']

    def source_sha(self, relative):
        return self.source_check()['files'][relative]['rawSHA256']


def get(model=None):
    if ACTIVE is None:
        raise ValueError('adapter:explicit-configured-context-required')
    if model is not None:
        ACTIVE.checked(model)
    return ACTIVE


def checked(model, *packets):
    return get(model).checked(model, *packets)
