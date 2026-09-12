# #723 environment parameters

Implemented bounded public fetch inputs for shared-preview config. The engine in
`internal/parameters` performs one-pass `${NAME}` replacement, with no expressions,
defaults, recursion, cross-key lookup or secret substitution.

Migration 00052 adds `environments.parameters_json` and
`snapshots.parameter_contract` on both engines. The latter is written with the
snapshot insert and never updated. It captures declarations plus validation
schemas for templated config keys, bounded to 256 KiB per snapshot and included in
project payload quota and instance storage metrics. New environment declarations affect only the
next ordinary publication, preserving publish approvals and protected-environment
confirmation. Declaration edits require project `definitions-edit`, a project
lock, database-managed definitions and a schema-revision budget charge.

`env param add|list|delete`, export `--param` and the declaration API are wired.
`HikyoSecret.spec.parameters` feeds the delivery query and local cursor binding.
The operator's keyed stamp derivation also binds canonical parameters, so input
changes move the stamp even when mapped data is unchanged. Empty parameter maps
keep the existing token and stamp encodings.

Delivery/export resolve and validate atomically before commit or returned values.
Read-only render accounting excludes hidden secret bytes. Historical export and
pin delivery use the captured historical contract. Parameter audit maps are
bounded and credential-redacted; config exports now emit per-value disclosure
events under the read proof as well as secret exports under reveal authority.
Operator HTTP 400 handling reads only bounded typed `bad_request` details, naming
invalid parameters/config keys while retaining the previous Secret.

Explicit boundaries: declarations are not transported in definitions bundles;
git-managed edits are refused. Outbound adapters and managed instance config
refuse parameterized snapshots. Compose and run lack input parameters and refuse
their fetch on missing required inputs. Different parameter sets share the same
stored secrets; use independent environments for per-preview secrets.

Regression coverage includes full-match regex and bounds, single-pass behavior,
undeclared publish refusal, secret literal preservation, parameter audit,
conditional cursor changes, historical pattern/schema immutability, final-schema
atomic refusal, exact-1MiB config unaffected by hidden-secret size, declaration
wire authority, duplicate CLI flags, operator stamp and safe validation details.
UTF-8 contract metadata is charged by bytes in both project quota and instance
metrics. Contract reads have a dedicated read-only store operation; managed
self-configuration keeps its bound snapshot check when reading that metadata.
Parent owns combined verification, generated compatibility metadata, PostgreSQL
suite, final review and delivery lifecycle.

Clone-at-creation of an environment with live parameter declarations is refused
atomically because the new destination has no declarations. Use a concrete clone source, or
create the destination, declare parameters and copy explicitly.

## PR #731 review corrections

- Template syntax activates only with declarations. Existing zero-declaration
  values retain `${` literally across upgrade and subsequent publication.
- `$${` escapes a literal opening when templating is active. First opt-in requires
  escaping legacy literal openings; refused publications preserve old delivery.
  Any dollar immediately before `${` escapes it; `$$${NAME}` delivers literal
  `$${NAME}`, and `$$` elsewhere stays unchanged. Adjacent literal-dollar plus
  resolved-reference syntax is unsupported.
- Caller-owned draft advisories expose `validation_deferred`; escaped-only values
  receive normal resolved schema validation before publication.
- Stored contracts have semantic version 1, accept additive same-version metadata,
  and reject unsupported versions. Nonempty declarations or schemas require
  version 1; pre-feature empty `{}` snapshots remain literal. The never-shipped
  version-zero template parser and synthetic historical fixtures were removed.
- Compiled whole-value RE2 patterns use a synchronized 128-entry FIFO cache.
- Parameter deletion validates names directly and rejects `--pattern`, including
  an explicitly empty flag; service deletion also refuses a nonempty pattern.
- Copy reads committed live cells and current environment declarations. When
  declarations exist, config `${` syntax, including escaped openings, requires
  destination declarations before any secret is opened. Clone refuses any live
  source declarations atomically. A previous snapshot cannot override these
  checks; private pending drafts are not copy sources. Removing live declarations
  restores literal copy semantics even while an older snapshot remains templated.

Operator version compatibility: nonempty parameter inputs require a positive
selected snapshot revision in every successful server response, including
conditional current responses. Legacy API responses that ignore the parameter
query are refused before managed data or cursor updates. Fetches without
parameters retain compatibility with older response shapes.
