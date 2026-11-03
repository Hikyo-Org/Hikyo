# JSON Schema builder handoff

Branch: `dunky13/json-schema-builder-simplified`, targeting `dev`.

## Result

A `json` key's optional schema no longer needs JSON Schema knowledge. The create
form and the declaration detail editor both show a field-by-field builder: a value
shape, then object fields (name, type, required) or an array's item shape, nested as
deep as needed, with length and pattern rules on text and a tick to reject unlisted
fields on an object. The raw document stays under "Advanced: edit as JSON Schema
(2020-12)" and shows what the builder writes. The create form can also infer a
schema from the first value on an explicit click.

There is no API, server or stored-format change. The form still holds and submits
one schema string, and the server's profile stays the only authority on it.

## Decisions

- The schema text is the source of truth. The builder is a view over it and never
  writes on mount, so a presence-only save leaves a stored schema untouched.
- The builder shows `type`, `properties`, `required` and `items`, plus string
  `minLength`, `maxLength` and `pattern` and object `additionalProperties: false`.
  Any other keyword, such as `format` or `$ref`, opens as raw JSON and is never
  rewritten; a root `$schema` naming the pinned dialect is the one exception, and it
  is kept. Strictness is what prevents a silent loss, so widening the builder means
  adding the control and the keyword together.
- A rule is accepted only where the builder can write it back exactly: lengths as
  non-negative safe integers on `string`, a non-empty `pattern` on `string`, and
  only the literal `false` for `additionalProperties` on `object`. Anything else,
  such as `additionalProperties: true`, `minLength: 1.5` or `pattern: ""`, or a
  minimum above a maximum, stays raw JSON. Lengths are kept as typed text, so a
  half-typed number is never coerced, and a refusal rides with the last valid text.
- Inference never sets these rules: one sample cannot establish a limit, and a rule
  must not be derived from a value.
- The pattern hint says "RE2" and "matches anywhere" because that is how the server
  evaluates it, and it differs from Hikyo's own string rule. The browser does not
  check the pattern: the server refuses an invalid one at save, with its own message.
- Inference copies names and shapes, never values. It marks every sampled field
  required, and an array's elements merge: `required` only where every element has
  the field, integer and number widen to number, and other conflicts become
  unconstrained.
- Inference is off for secret keys. Schemas are exported to Git, and a secret value's
  field names can themselves be credentials (`docs/adr/schema-model.md`, "Error
  disclosure"). Marking a key secret rebuilds the builder from its text, dropping rows
  kept for type flips, and removes a schema that may still hold a sampled name, or
  that it cannot read. A hand-written replacement that holds none is kept. This
  guards inference, not deliberate typing: a person can still enter any field name.
- On the create form the schema sits below the first value, because the value is what
  constrains it (`DESIGN.md`, "Ask in dependency order").

## Known limits

- The server refuses a secret key's schema when a field is named `enum`, `const` or
  `examples`: `valueLiteralKeyword` in `internal/schema/classification.go` also walks
  `properties` names. The refusal is loud and predates the builder. Narrowing it
  loosens a security check, so it was left for a separate decision.
- The detail editor has no inference, because no value is at hand there.
- A sampled field named like a keyword, such as `type`, makes the secret tick remove
  a hand-written schema. That errs towards removal and the note says why.
- "Replace schema from this value" overwrites the current schema with no undo; its
  label says so.

## Next steps

None required.
