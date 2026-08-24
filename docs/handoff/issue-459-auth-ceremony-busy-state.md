# Handoff: #459 authentication ceremony busy state

Issue: https://github.com/Hikyo-Org/Hikyo/issues/459. Base:
`5543b70eae3f3851247ce34f842e976c60ad02cf`.

## Contract

- Password, passkey, and OIDC login share one page-level busy state.
- While any login mutation is pending, username and password inputs plus every
  authentication button are disabled.
- Each method keeps its existing method-specific pending label and error
  presentation.

## Coverage

- Component tests cover both passkey and OIDC pending states through the
  rendered login form and require all five available controls to be disabled.
- Browser coverage holds the OIDC start request pending, requires the same five
  controls to be disabled, and runs the serious/critical axe assertion against
  that pending state.
- Local web validation was deferred before the first push because host swap
  remained above 89% used. CI validates the initial exact PR head while local
  validation waits for memory pressure to subside.
- Generated outputs: none.
