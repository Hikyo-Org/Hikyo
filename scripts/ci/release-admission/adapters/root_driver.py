"""Configured parent entry for the real one-use paired-driver implementation.

The parent owns source adoption and cross-process configuration. This module
has no default authority or legacy model loader.
"""
from . import adapter_context
from . import paired_core
from . import source_gate


def run(*, model, **arguments):
    adapter_context.get(model)
    arguments['m'] = model
    return paired_core.Attempt().run(**arguments)
