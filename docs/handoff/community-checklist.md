# Community checklist handoff

Date: 2026-10-28

## Request and baseline

Address the missing items on the repository's
[community checklist](https://github.com/Hikyo-Org/Hikyo/community), including
the additional accessibility file. GitHub's community-profile API reported
75% health on main at `24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
Code of conduct, issue templates, and pull request template were absent.
The supplied screenshot also showed accessibility as missing. Description,
README, contributing, license, security policy, and content reporting were
already present.

## Changes

- [Code of conduct](../../CODE_OF_CONDUCT.md): expected behavior, enforcement,
  repository-admin reporting, and an independent GitHub Support escalation
  when the sole maintainer is involved.
- [Issue templates](../../.github/ISSUE_TEMPLATE/): bug reports, feature
  requests, and accessibility barriers, with sanitized-evidence guidance.
  The existing private security contact link and blank-issue option remain
  available.
- [Pull request template](../../.github/pull_request_template.md): change
  summary, evidence, risk, signed commits and DCO, relevant checks, and visual
  evidence only when applicable.
- [Accessibility statement](../../ACCESSIBILITY.md): accessibility goals,
  intended environments, actual automated coverage, verification gaps,
  alternative interfaces, and reporting instructions. It makes no blanket
  conformance or screen-reader compatibility claim.

## Validation and completion boundary

Local feedback planning selected no application, frontend, workflow, shell, or
published-site changes. `scripts/ci/fast-feedback.sh --working-tree` passed,
including whitespace checks on the new files. The three issue templates parsed
as YAML front matter with distinct names, descriptions, existing repository
labels, and nonempty Markdown bodies. Relative document links were checked
against repository files and headings. The existing issue configuration retains
its private vulnerability-reporting destination.

This is a community-documentation change. No runtime behavior changed, and no
UI screenshots or application test run are claimed. Local checks, hosted CI,
review, and default-branch detection are separate evidence.

The files become active after maintainer approval and merge to the default
branch. After merge, refresh the community checklist and run
`gh api repos/Hikyo-Org/Hikyo/community/profile` to confirm GitHub detects the
code of conduct, issue template, and pull request template. Check the
accessibility entry on the community page separately because the profile API
does not expose that field. Do not report the live checklist as complete before
that readback.
