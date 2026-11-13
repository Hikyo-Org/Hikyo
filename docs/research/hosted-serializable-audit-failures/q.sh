#!/bin/bash
# psql against the benchmark server; PGBIN selects the PostgreSQL 18 client.
PGB=${PGBIN:-$(dirname "$(command -v psql)")}
exec "$PGB/psql" -h "${PGHOST:-127.0.0.1}" -p "${PGPORT:-55432}" -U "${PGUSER:-postgres}" -X -v ON_ERROR_STOP=1 "$@"
