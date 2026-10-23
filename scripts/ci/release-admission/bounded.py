"""G-compatible bounded JSON and explicit UTC helpers. No implicit clock or I/O."""
from datetime import datetime, timedelta
import hashlib
import json
import math
import re


def require(condition, label):
    if not condition:
        raise ValueError('release-admission:' + label)


def exact(value, fields):
    require(type(value) is dict and set(value) == set(fields), 'closed-fields')


def canonical(value):
    active = set()

    def walk(item, depth=0):
        require(depth <= 40, 'json-depth')
        if type(item) in (dict, list):
            identity = id(item)
            require(identity not in active, 'json-cycle')
            active.add(identity)
            if type(item) is dict:
                require(all(type(key) is str for key in item), 'json-key')
                children = item.values()
            else:
                require(len(item) <= 2048, 'json-list-size')
                children = item
            for child in children:
                walk(child, depth + 1)
            active.remove(identity)
        elif type(item) is float:
            require(math.isfinite(item), 'nonfinite-json')
        else:
            require(item is None or type(item) in (str, int, bool), 'json-value')
    walk(value)
    raw = json.dumps(value, sort_keys=True, separators=(',', ':'),
                     ensure_ascii=True, allow_nan=False).encode()
    require(len(raw) <= 2_000_000, 'json-size')
    return raw


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def strict_json(raw):
    require(type(raw) is bytes and len(raw) <= 2_000_000, 'raw-json-size')

    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate-key')
            result[key] = value
        return result

    def constant(_):
        raise ValueError('release-admission:nonfinite-json')

    try:
        value = json.loads(raw.decode('utf-8'), object_pairs_hook=pairs,
                           parse_constant=constant)
        canonical(value)
    except (UnicodeError, RecursionError, OverflowError) as error:
        raise ValueError('release-admission:invalid-json') from error
    return value


def sha(value, length=64):
    return type(value) is str and re.fullmatch('[0-9a-f]{' + str(length) + '}', value) is not None


def instant(value):
    require(type(value) is str and re.fullmatch(
        r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?Z', value
    ) is not None, 'explicit-utc-time')
    return datetime.fromisoformat(value[:-1] + '+00:00')


def clock(now):
    require(type(now) is datetime and now.tzinfo is not None
            and now.utcoffset() == timedelta(0), 'explicit-utc-clock')


def stamp(now):
    clock(now)
    return now.isoformat(timespec='microseconds' if now.microsecond else 'seconds').replace('+00:00', 'Z')


def fresh(value, now):
    clock(now)
    require(timedelta(0) <= now - instant(value) <= timedelta(minutes=5), 'freshness')
