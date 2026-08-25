# Global pnpm virtual store

## Goal

Stop identical Hikyo worktrees from materializing a separate pnpm virtual store for every checkout.

## Implementation

- Pin all three JavaScript package roots to pnpm 11.24.0.
- Set `virtualStoreType: global` in each package root's `pnpm-workspace.yaml`.
- Treat every workspace configuration change as a full CI input.
- Keep setup documentation aligned with the executable package-manager pins.

pnpm stores dependency graphs under `~/Library/pnpm/store/v11/links` outside CI. Each worktree keeps only its package-root symlinks. pnpm disables the global virtual store automatically in CI.

## Validation

- Each package root reports `virtual-store-type=global`.
- Frozen installs reused every package and downloaded none.
- TypeScript SDK generation, typecheck, and 14 tests pass.
- Web typecheck, 388 tests, and production build pass.
- Docs check, build, policy checks, PWA checks, and offline browser test pass.
- Changed-path classifier fixtures pass.

The adoption also fixes an existing implicit event type in `Matrix.tsx` exposed by the current TypeScript toolchain.
