# Security policy

Do not report vulnerabilities in public issues. Use GitHub Private Vulnerability
Reporting or, when GitHub is unavailable or inaccessible, the independently
hosted fallback address below. The repository's New Issue page links straight to
private reporting, so this is the default path.

If a vulnerability is nonetheless filed as a public issue, the maintainer locks
the issue and redacts the exploitation details, then opens a private advisory to
coordinate the fix. Because the report is already public, the disclosure is
treated as public: mitigation guidance, the fix, and the advisory are
fast-tracked ahead of any milestone, exactly as for active exploitation. The
reporter is still credited unless they opt out.

## Reporting a vulnerability

1. Report privately through [GitHub Private Vulnerability Reporting](https://github.com/Hikyo-Org/Hikyo/security/advisories/new).
2. If GitHub is unavailable or you cannot access it, email
   [security@hikyo.app](mailto:security@hikyo.app). This address is
   fallback-only so reports stay consolidated in the primary channel.
3. Include affected versions, impact, reproduction steps, and any known
   mitigations. Do not include secrets that are not needed to reproduce the
   issue.

Both channels are notification-tested quarterly. Accepting a report creates a
temporary private fix fork and grants the reporter access so remediation never
leaks through the public pull-request flow.

## Response targets

Reports are acknowledged within 7 days. Fix-release targets start when a report
is confirmed:

- critical: 14 days;
- high: 30 days; and
- medium or low: the next scheduled release.

These are targets for a solo maintainer, not guarantees. The disclosure clock
below is the reporter's backstop. Reporters are credited in the advisory unless
they opt out.

## Coordinated-disclosure embargo

The default embargo is 90 days from the report itself. The clock never waits on
acknowledgement. If acknowledgement is missed, reporters are expressly invited
to escalate through `security@hikyo.app`; disclosure at the 90-day deadline
is legitimate regardless of maintainer response.

The embargo may be shortened by mutual agreement. Active exploitation
accelerates disclosure and remediation; it never extends the embargo. Extension
beyond 90 days requires mutual agreement for exceptional coordination on a
non-exploited issue and a revised hard deadline. If a reporter cannot be reached
at that deadline, the advisory is published on schedule.

The patched release is published before advisory details. Immediate mitigation
guidance, the fix, and the advisory are fast-tracked ahead of any milestone when
exploitation is active.

## Advisories and CVEs

A security advisory is the record of a vulnerability: what was affected, the
impact, and the fixed version. It starts as a private draft used to coordinate
the fix and becomes the public record once the patched release ships. GitHub
assigns each advisory a GHSA (GitHub Security Advisory) identifier; a CVE (Common
Vulnerabilities and Exposures) is the cross-industry identifier for the same
issue.

A CVE is requested through the private GitHub advisory before publication. CVE
assignment is never release-blocking: an urgent fix may ship under its GHSA,
with the CVE added later. Duplicates use the existing CVE; a rejected request
leaves the GHSA as the canonical reference.

## Supported versions

| Version | Supported |
| --- | --- |
| Latest patch release of the latest minor | Yes |
| All older stable releases | No |
| Prereleases | No |

The table is the concise form of the complete [support policy](./SUPPORT.md).
There are no backports or overlapping support windows.
