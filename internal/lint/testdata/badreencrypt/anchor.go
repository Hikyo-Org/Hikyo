package badreencrypt // want "reencrypt-coverage: new_secrets.sneaky_ciphertext is an unclassified BLOB column" "reencrypt-coverage: both_sets.ambiguous is in BOTH the covered and exempt sets" "reencrypt-coverage: covered column gone_table.gone_column no longer exists in the schema"

// The anchor the reencryptcoverage analyzer runs on; the migrations sit under
// the fixture root and the flags name the covered and exempt sets the schema is held to.
