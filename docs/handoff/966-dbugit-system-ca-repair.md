# #966: DBugIT system-CA consumer repair

Status: **prepared and locally validated, downstream application NOTRUN/HOLD**.
This packet changes no DBugIT repository, protected variable, credential or
operational runner. It accompanies the [TLS outcome handoff](966-cli-system-ca-trust.md).

## Exact source and defect

Repository: `DBugIT_V3/Rewrite` on `git.dbugit.io`.
Inspected fresh main: `68467d9463ebd53f522b00a7fb220d916680801b`.
File: `scripts/lib/hikyo.sh`, Git blob
`046e96a321d65da8a129d08397a8be92fe17de12`.
Original SHA-256: `5577439ce8ba3a4159458922ab6e273b375491f2663ae1dae4ba0202b684f57b`.
Repaired SHA-256: `1e81e0c32dc7ba544a487c15c9918a7b90de760e9eb054fda015729722d0fe16`.
Patch SHA-256: `e60cc81e8070215d787b1f849f8558f5f297105cd07202970eb225ffbca0bf55`.

The wrapper requires `spki_pin` before invoking Hikyo. An explicit system-CA
bundle has no pin and is rejected with `HIKYO_TRUST_BUNDLE is missing: spki_pin`.
The correction requires the wrapper's two routing fields and lets the verified
CLI validate the complete protected identity. It adds no fallback, bypass or
second TLS schema. The existing private-file provisioning and token handling
remain in force. Legacy SPKI bundles continue through the same import.

## Validation and limits

The original helper failed against a disposable real Hikyo authorization
fixture using the independently verified, byte-identical Linux/arm64 nightly
`0.0.1-nightly.20261029.99.gd0224221`. With only this patch changed, the helper's
`hikyo_machine_setup` and `hikyo_cli env list -o json` succeeded before and after
a valid leaf-key renewal. The server minted a fresh machine credential and
checked real grants. The fixture used no operational DBugIT credential.
After review, the consumer assertion additionally requires the exact fixture
environment and one successful environment-list response from the real API.
Forged output, including a matching ID after only a metadata request, is refused.

The repaired source snapshot passed `bash -n scripts/lib/hikyo.sh` and its
existing owner/consumer checks:

```bash
bash scripts/preview-deploy.test.sh
bash scripts/preview-teardown.test.sh
bash scripts/vu-release.test.sh
```

These owner checks use their existing stub tools and isolated temporary roots.
They do not deploy a preview or release. The separate real-authorization test
covers the released CLI and helper. No DBugIT application/TypeScript code,
database schema or live pipeline was changed, and no whole-application test
or hosted downstream CI pass is claimed.

## Apply to the authorized consumer source

Use an isolated DBugIT source checkout. `HIKYO_CONSUMER_CHECKOUT` identifies
that checkout; `HIKYO_CONSUMER_PATCH` names a new patch output file. Run the
extraction from this Hikyo repository root:

```bash
set -euo pipefail
: "${HIKYO_CONSUMER_CHECKOUT:?Set the isolated DBugIT source checkout}"
: "${HIKYO_CONSUMER_PATCH:?Set a new patch output file}"
python3 - "$HIKYO_CONSUMER_PATCH" <<'EXTRACT'
from pathlib import Path
import sys
source = Path("docs/handoff/966-dbugit-system-ca-repair.md").read_text()
patch = source.split("```diff\n", 1)[1].split("```", 1)[0]
with open(sys.argv[1], "x") as output:
    output.write(patch)
EXTRACT
git -C "$HIKYO_CONSUMER_CHECKOUT" apply --check "$HIKYO_CONSUMER_PATCH"
git -C "$HIKYO_CONSUMER_CHECKOUT" apply "$HIKYO_CONSUMER_PATCH"
```

Check the exact source/blob above and run the matching owner checks and the
released-consumer fixture before making a signed/DCO downstream PR. Preserve
normal downstream review and protections. Applying this source patch does not
update the operational CLI pin or protected bundle.

The selected isolated runner and target, paired non-production read credential,
protected `system-ca` bundle and rollback configuration must be approved before
the named downstream acceptance. The observed Forgejo bundle names the existing
operational `https://hikyo.dbugit.io` origin in legacy SPKI mode. Preview,
staging and release credential names exist; their values were not read and
none was reused. A credential's existence is not #966 isolated-use authority.

## Reviewed patch

```diff
--- a/scripts/lib/hikyo.sh
+++ b/scripts/lib/hikyo.sh
@@ -68,7 +68,8 @@
   local fields origin reference
   # Parse, do not grep: the bundle must be the JSON shape the CLI expects, and
   # both fields are needed here (the origin for --instance, the name as the
-  # reference every later call resolves).
+  # reference every later call resolves). The verified CLI owns mode, pin,
+  # socket, unknown-field and origin validation; do not impose SPKI-only trust.
   fields="$(DBUGIT_HIKYO_TRUST_FILE="${trust_file}" python3 -c '
 import json
 import os
@@ -82,13 +83,13 @@
     sys.exit(f"HIKYO_TRUST_BUNDLE is not valid JSON (line {error.lineno} column {error.colno})")
 if not isinstance(bundle, dict):
     sys.exit("HIKYO_TRUST_BUNDLE is not a JSON object")
-missing = [field for field in ("name", "origin", "spki_pin") if not bundle.get(field)]
+missing = [field for field in ("name", "origin") if not bundle.get(field)]
 if missing:
     sys.exit("HIKYO_TRUST_BUNDLE is missing: " + ", ".join(missing))
 print(bundle["name"])
 print(bundle["origin"])
 ')" || {
-    echo "ERROR: HIKYO_TRUST_BUNDLE is not a usable Hikyo trust bundle (expected JSON with name, origin, spki_pin)." >&2
+    echo "ERROR: HIKYO_TRUST_BUNDLE is not a usable Hikyo trust bundle (expected JSON with name and origin; the CLI validates the TLS identity)." >&2
     return 1
   }
   reference="$(printf '%s' "${fields}" | sed -n '1p')"
```
