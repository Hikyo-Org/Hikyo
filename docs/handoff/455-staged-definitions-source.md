# Issue #455 — staged definitions-source governance

Issue: https://github.com/Hikyo-Org/Hikyo/issues/455

## Contract

- Changing the Definitions source selector stages a local choice and performs no write.
- `Apply definitions source` is enabled only when the staged choice differs from saved settings
  and no mutation is pending.
- Applying sends the existing definitions-settings mutation once. Query invalidation resynchronises
  the staged choice from the saved server response.
- The Git-mode notice continues to describe saved policy, not an unapplied staged choice.

## Coverage

- The project-settings component test pins no mutation on selection and one exact Git payload on
  Apply.
- The browser settings flow now applies the staged choice before checking persisted Git policy.
- API contracts and generated outputs are unchanged.
