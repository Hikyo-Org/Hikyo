"""Pure bounded representation matcher, never a shell executor/authenticator.

Accepted outer grammar is three literal words separated by ASCII space/tab.
Words concatenate safe unquoted ASCII, single quotes, double quotes and plain
backslash escapes. Double quotes exclude expansion and support only escaped
quote/backslash. Other double-quote escapes are deliberately unsupported because
stdlib shlex is not a complete shell decoder (notably for dollar/backtick).
Newlines may occur only as literal data inside quotes, for the public multiline
fixture. No original, context, inner command or source object is rewritten.
"""
import shlex
from typing import TypeGuard

# At most the existing whole-original byte bound, never an execution deadline.
MAX_BYTES = 1_999_999
_UNQUOTED = frozenset('abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+,-')
_SEPARATORS = frozenset(' \t')


def _text(value: object) -> TypeGuard[str]:
    if type(value) is not str or not 0 < len(value) <= MAX_BYTES:
        return False
    try:
        return len(value.encode('utf-8', errors='strict')) <= MAX_BYTES and not any(
            ord(c) < 32 and c not in '\n\t' or ord(c) == 127 for c in value)
    except UnicodeError:
        return False


def _literal_grammar(text: str) -> bool:
    """Validate a small literal-word language before any shlex decoding.

    A single linear scan, no recursion, regex backtracking, shell execution,
    substitution, environment resolution, filesystem access or normalization.
    """
    state = 'plain'
    index = 0
    while index < len(text):
        char = text[index]
        if state == 'single':
            if char == "'":
                state = 'plain'
        elif state == 'double':
            if char == '"':
                state = 'plain'
            elif char == '\\':
                index += 1
                # Deliberately reject \$, \`, \newline and \q in double quotes.
                # shlex and shell escape semantics must agree for accepted text.
                if index >= len(text) or text[index] not in '\\"':
                    return False
            elif char in '$`':
                return False
        elif char == "'":
            state = 'single'
        elif char == '"':
            state = 'double'
        elif char == '\\':
            index += 1
            if index >= len(text) or text[index] in '\n\r':
                return False
        elif char not in _UNQUOTED and char not in _SEPARATORS:
            return False
        index += 1
    return state == 'plain'


def matches_native_carrier(native_input: object, *, expected_inner_command: object,
                           expected_carrier_argv: object) -> bool:
    """Match ONLY literal `/bin/zsh -lc exactInnerCommand` representation.

    The caller must FIRST validate its whole reviewed source/command context
    and obtain the unchanged whole original via its existing genuine current-
    stream verifier. This function does not authenticate that original, inspect
    its output/timestamps or authorize the reviewed inner command. Operators and
    newlines INSIDE the safely quoted exact inner string are opaque caller-
    reviewed data, not additional outer-shell operations.
    """
    if not _text(native_input) or not _text(expected_inner_command):
        return False
    if type(expected_carrier_argv) is not list or len(expected_carrier_argv) != 3:
        return False
    if any(type(word) is not str for word in expected_carrier_argv):
        return False
    if expected_carrier_argv != ['/bin/zsh', '-lc', expected_inner_command]:
        return False
    if not _literal_grammar(native_input):
        return False
    try:
        decoded = shlex.split(native_input, comments=False, posix=True)
    except ValueError:
        return False
    # No rebuilding, canonicalizing, stripping or reparsing of the inner text.
    # Comparing UTF-8 bytes also forbids Unicode normalization equivalence.
    return decoded == expected_carrier_argv and decoded[2].encode('utf-8') == expected_inner_command.encode('utf-8')
