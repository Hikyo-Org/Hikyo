CREATE TABLE sealed_things (
    id TEXT PRIMARY KEY,
    payload_sealed BLOB NOT NULL,
    wrapped_key BLOB NOT NULL
);

CREATE TABLE new_secrets (
    id TEXT PRIMARY KEY,
    sneaky_ciphertext BLOB NOT NULL
);

CREATE TABLE both_sets (
    id TEXT PRIMARY KEY,
    ambiguous BLOB NOT NULL
);
