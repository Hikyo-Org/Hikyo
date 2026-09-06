# Self-configuration variable inventory

Regenerate the report metadata from the complete parser inventory:

```sh
go run ./scripts/self-config-inventory > docs/reports/self-configuration/variable-inventory.json
```

The command emits metadata only. It never reads configured values, files, or
ambient environment variables. Activation classes describe required lifecycles;
they do not claim that the current build implements those Apply mechanisms.
Import classifications describe source boundaries. The active managed catalogue
must separately admit a key before importing or applying it.

Run `go test ./internal/config` to check completeness against every recognized
environment key and preserve the bootstrap, client-token, and root-key boundaries.

`secret` classifies the value (or the explicitly imported mail contents). For
external file references such as `HIKYO_ROOT_KEY_FILE`, a nonsecret path does not
make the referenced private-key bytes public: `referencedContentSecret` records
that separate boundary. Neither field authorizes reading a file.

This inventory covers recognized environment inputs. Server-only startup flags
such as `--config-rollout-enrollment` and `--config-rollout-signing-key` configure
installed deployment authority and have no environment-variable equivalent.
They remain outside this environment inventory and ordinary managed values.
