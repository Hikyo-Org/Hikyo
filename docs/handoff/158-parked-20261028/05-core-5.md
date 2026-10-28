# Parked #158 handoff: 5. Independent cleanup and cloud state to preserve


Latest full independent cleanup snapshot is c11 at `2026-10-27T23:53:43.584594Z`, not a fresh28October authenticated account inspection. All27acknowledged complete ARNs and2unacknowledged reserved names returned strict NotFound. Complete regional Secrets Manager inventory, including planned deletion, was empty after335.922 seconds terminal quiescence and in the final readback. All8exact roles, owned issuer, boundary, alias and9protected variables were absent. The protected environment/OIDC settings and unrelated service-linked roles were preserved.

Setup user ARN`arn:aws:iam::099256073061:user/hikyo-158-setup`, UserId`AIDAROHA7PNS4ZCR3U3FR`. Its managed policy ARN`arn:aws:iam::099256073061:policy/hikyo-158-setup-policy`, PolicyId`ANPAROHA7PNSTPQUQEUTR`. Original expiredv5 restored. Preserve v2-v5, their bytes/dates and the sole attachment, identity and MFA. No setup access keys, groups or inline policies were authorized. Last actual v11 is retained nondefault; C12 may archive/delete only exact owned nondefaultv11 after fresh readbacks, then one bounded new version using the actual acknowledged ID. Do not assume the next version number.

| Setup version | Original creationUTC |
|---|---|
|v2|2026-10-19T14:13:52Z|
|v3|2026-10-19T23:33:57Z|
|v4|2026-10-20T20:51:04Z|
|v5|2026-10-20T21:45:02Z|
|v11(nondefault)|2026-10-27T23:03:49Z|

Ownedv11 canonical documentSHA256`1ca190778d617cd90ec21e757b2b286d8f6c3f9110d7a6f44637b29cdd86f2f3`. Recordedv5documentSHA256`5d0cb52fdd94cb0bc68db4e2ffc5c5570a62c9a459da7c5399fda68e05e7afd7`. Verify representation and actual document before destructive version operations. Never deletev2-v5 or recreate identity/MFA as a shortcut.

### KMS schedules: preserve exactly, do not cancel

| KeyID | Exact retained deletionUTC | Last recorded interpretation |
|---|---|---|
|990c479f-d673-4bc9-8bca-7b9b86f4b205|2026-10-27T21:42:12.444Z|Naturally absent after scheduled time in complete c11 reconciliation|
|bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1|2026-10-28T23:31:42.338Z|PendingDeletion at last authenticated snapshot|
|64224f0e-697e-4090-9978-ea153625f6d9(c05)|2026-11-02T13:48:51.595Z|PendingDeletion|
|4005b859-369d-4fa5-a96c-7b8e865ee712(c06)|2026-11-02T17:19:46.043Z|PendingDeletion|
|b8806a19-57c8-4d77-9d1d-39c611a933a1(c07)|2026-11-02T20:45:47.974Z|PendingDeletion|
|b71f3c57-626a-4a89-9fcf-f7901a421e49(c08)|2026-11-03T10:51:15.444Z|PendingDeletion|
|88e19f70-fee7-4f9b-9b35-847bba607a55(c09)|2026-11-03T14:19:45.807Z|PendingDeletion|
|03dcf56b-9687-4173-97d5-6f4925d08797(c10)|2026-11-03T21:32:50.570Z|PendingDeletion|
|81294276-467e-45b2-8279-1abb6fea8e34(c11)|2026-11-03T23:50:52.008Z|PendingDeletion|

Older key selectors10908b8d-fe54-40e3-9210-a3eefea709b6 andfa374267-a456-4e79-b5b1-6cdf356b7d0e were strictNotFound in last complete reconciliation. The former had original schedule2026-10-26T09:54:54.536Z. Allc05-c11aliases above were strict absent. C12alias has never been created.

A strict NotFound after the pinned scheduled time can prove natural deletion only with a complete exact selector set and correctly bounded read start/finish. AccessDenied, transport failure, missing captures, incomplete list pagination, or a different ARN is not absence. PendingDeletion must be disabled with the exact existing deletion timestamp. Do not reuse a pending key or cancel its schedule.

Cleanup receiptSHA256: c07`dbb506f841f18dee20d0e62db3d450489714db6b3f223524ab9b64a546ed39f8`; c08`794c73583f5923109f57a703bf2c54d669bba9df427c220a1d50467b164f3e45`; c09`4db0330e33816d30041f9904c2dfad0c70acb0a00040ea22944d5151b0fee434`; c10`8ff3d9331d328ad6c03804f67f92322f94ea6249616cc23f880691e352667c77`; c11`6c82131e5d37cac25d45584125c63c0da233dfbef366fd7ec449311ee103d419`. C11 canonical closure-label correction is in section4.

Separate private owner/setup browser profiles are retained for recovery under the owner's earlier background-browser instruction. Preserve their private storage; do not copy it into Git/ticket or inspect cookies/cache/tokens/DPoP keys. No owned test container or test worker remained at the last verification. Shared PostgreSQL/dev services and active worktrees are protected. Required failure evidence, custody records and recovery backups are intentionally retained. Parking does not authorize global Docker/cache/worktree pruning.
