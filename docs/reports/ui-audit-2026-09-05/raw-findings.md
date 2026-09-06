P0 Members.tsx:507 grant/invite/reset controls ungated by capability (verify: does server refuse? UI should hide + honest note)
P0 ProjectSettings.tsx:200 Policy/Danger always render (prototype gates canPolicy)
P1 app.css:189 .avatar bg-raise -> bg-panel
P1 ProjectSettings.tsx:459 definitions source detail hardcoded -> conditional on git
P1 ChromeIdentityControls.tsx:59.. dead disabled controls outside prototype -> remove
P1 Sections.tsx:85 Done replaces undo (prototype undo on grant/revoke) -> ratify or restore
P1 ProjectSettings.tsx:150 Description disabled readOnly "not available in the API" -> delete
P2 app.css:72,79 .btn bg-raise -> bg-panel/hover; :3016,3803; :460-505 toast; :1163 menu hover; :1181,2626,2655,2661,4665,4592
P2 app.css:1301 header identity hidden on mobile -> keep name
P2 Projects.tsx:14 / Placeholder.tsx:11 h1 inside card -> page anatomy
P2 OrgSettings.tsx:326 silent return on invalid retention -> Alert + reset
P2 ProjectSettings.tsx:1012 over-cap client refusal missing
P2 Shell.tsx:187 activeOrgRole constant "Organisation member" -> derive or drop
P2 InstanceAdmin.tsx:102 9 jump chips vs scroll-margin 176px
P2 app.css:1191 mobile touch floor missing for switcher rows, settings-row links
P2 InstanceAdmin.tsx:213 "not exposed by this API" availability claim
P2 sidebar-model.ts:32 "not available ... yet"; ChromeIdentityControls:168 "not available yet"
P2 OrgSettings.tsx:198 delete cascade copy vs prototype (check ADR)
P3 app.css:669 mobile drawer 46px section gap -> 30
P3 app.css:862 context-sidebar h2 tx-dim -> tx-faint
P3 app.css:654,780 50% -> radius-pill; :1060,5693 dots; :2017 3px literal; :653 badge ring
P3 13 more bg-raise in chrome: 1456,1735,1758,1974,2190,2243,3438,5122,5344,5568,5658,5767,5790
P3 Audit/Adapters/ChangeApprovals/MachineAccess no JumpIndex
P3 em-dash conflict with GIT_DEFINITIONS_NOTICE verbatim (ui-spec:32) -> carve out
P3 OrgSettings:174 prototype-only fake slug rename
P3 ProjectSettings:107 jump list drift
P1 MatrixRowEditor.tsx:424 secret copy-to hard gated off (openapi supports reveal(src)+reveal(dst)+publish(dst))
P1 MatrixKeyCreate.tsx:432 / MatrixRowEditor.tsx:249 secret input type=password flattens newlines
P1 MatrixRowEditor.tsx:238 schema JSON dump -> human summary
P1 MatrixKeyCreate.tsx:130 no near-miss warning
P2 MatrixKeyCreate.tsx:166 silent trim; use normalizeMatrixDraftValue + notice
P2 app.css:1957/1790 row density 47px desktop, 54 mobile -> token on td, cell padding-block 0
P2 app.css:1927/2561 .matrix__key 19.5px tall; mobile width < 44
P2 app.css:1735/1456 --bg-raise on popovers -> --bg-panel
P2 app.css:1974 cell hover --bg-raise -> --bg-hover
P2 app.css:1768 .matrix__scroll fixed 70vh -> flex fill
P2 MatrixPublishSheet.tsx:93 inline section, no close
P2 matrix-state.ts:91 raw environment id in problem message
P2 Matrix.tsx:1230 lock aria-hidden, aria-label lacks "secret"
P2 Matrix.tsx:1250 degraded cell '—' aria-hidden -> '· unreadable' + label
P2 app.css:1575 problem-count pill -> badge radius
P2 Matrix.tsx:1591 draft dot colour-only (prototype vs DESIGN.md) -> decision
P2 Matrix.tsx:1035 empty-state says import via CLI while Import button exists
P2 MatrixRowEditor.tsx:626 canReveal fails open
P2 Matrix.tsx:1174 group count inherits uppercase/bold
P2 Matrix.tsx:404 group collapse no animation (DESIGN.md says grid-template-rows)
P2 MatrixRowEditor.tsx:290/292/375 "explicit set/absent", "unset pending" wording
P3 textareas fixed rows -> field-sizing content
P3 Matrix.tsx:896 aria-controls dangling
P3 Matrix.tsx:984 vs 1046 case drift lowercase vs sentence case
P3 em-dashes Matrix.tsx:802,806,982,1010,1253,1595,1640,1646; MatrixKeyCreate.tsx:227; MatrixPublishSheet.tsx:96,136,219
P3 Shell.tsx:791 sidebar count replaced by problem count colour-only -> prefix !
P3 MatrixRowEditor.tsx:238 no "Edit declaration" link in modal
P3 ScanWarnDialog.tsx:122 locator dropped
P3 Matrix.tsx:1236 linked-keys tooltip claim vs publish sheet not grouping
P3 tokens.css:58 ease quint vs DESIGN quart
P3 Matrix.tsx:1154 group th no scope
Mine: productionPROTECTED no gap; matrix__linked-keys block line doubles row height (#665); key truncation "STRIPE_SECR…" with space available
A HISTORY
P1 HistoryDrawer.tsx:1484 release confirm hedges; state sole-keeper consequence
P1 HistoryDrawer.tsx:1074 pin row lacks "sole keeper" badge / past-retention clause
P1 app.css:3552 Δ schema drift painted --danger -> --changed
P1 HistoryDrawer.tsx:110 secret-safe diff (rev↔rev) absent - scope gap needing API (track)
P2 app.css:3350 .history__current pill -> badge; :3580 chips .count pill with prose
P2 HistoryDrawer.tsx:558 settings pointer inert span -> Link
P2 HistoryDrawer.tsx:1210 two instructions (matrix vs in-sheet publish)
P2 HistoryDrawer.tsx:1132 impact preview flatMap drops env; key collision; label reads environments[0]
P2 HistoryDrawer.tsx:546 "pending by other people" -> name actors if available
P2 app.css:3455 collected tag --danger -> muted
P3 :1303 quota n/100; history-state.ts:178 renew vs move divergence; :1411 comparison behind button; :1204 '(absent)' -> · absent; :177 mobile drill-in vs prototype (keep, ours is better); :199 ?rev deep link mobile lands on list; :503.. glyphs not aria-hidden; em-dashes 27+19
Mine: Published by shows raw principal id (usr_…) -> needs display name
B MACHINE
P1 MachineAccess.tsx:325 kubernetes count hardcoded '0' -> unknown
P1 identities.ts:549 step 5 "Grant reveal" claim but only read grants exist
P1 identities.ts:403 scopeOf drops origins -> origin chips
P2 :669,818 machine__row-actions no CSS
P2 identities.ts:566 expiry severity invisible -> badge tiers + words
P2 :321.. tab counts '—' -> unknown; :712,844 '—' actions cell; :811 '—' expiry -> · absent
P2 :1430 journey rail inert (no actions)
P2 :664 'none' vs Adapters 'credential absent'
P3 :594 K8s CR conditions no API (scope gap); :1501 restore reconciliation exempt by ADR; :1779 lowercase title; :1832 expiry not shown at mint (needs pick widen); :431 tabs no roving tabindex; :799 N alerts in td; :752 machine__hint no CSS; :2143 preset colour-only; em-dashes 93+36+4
LOGIN (social sign-in family, post-1.0 per parity handoff; deferred, document): staged entry, sign-up door, "Sign-up is paused.", signup routes. Fixable now: Login.tsx:105 SAML providers missing from list; :115 shared pending flag across provider buttons; :105 no loading state; :29 blanket disable; :130 two full-width secondary btns.
ADAPTERS: :1058 visibility lacks `selected` option; no widening ceremony; no GHES base URL; no auto-create consent naming Administration:write; :271 possible_capture / owned-missing not surfaced; :1023.. disabled controls no reason; :1044 "Organization" spelling; :1088 silent uppercase.
KEY DECL: KeyDeclarationDetail.tsx:1305 any_of read-only; app.css:2199 no autogrow; MatrixKeyCreate:166 silent trim; :114/119 silent normalisation; near-miss absent; :262 deprecation no occurrence warning; :753 tightening advisory absent; owner-only invalid marker absent; MatrixKeyCreate lacks authoring statement + git banner; em-dashes 8
REMOTES: :233 raw enum in badge (remoteStateText exists); data-state no CSS; :467 duplicate identity not detected; :462 self-connection not refused at add; remotes.ts:341 stale line decoupled; app.css:3178 card list; :3243 nested cards; :172 'unavailable' kind; :263 absence 3 ways; 14 em-dashes
CHANGE APPROVALS: :181 nested <main>; no JumpIndex/Panel; no loading state; :379 empty no action; :343 env select blank state; :32 recovery phrasing
AUDIT: :86 no JumpIndex/Panel; :200 no loading; :206 empty no action control; :222 rows as .btn; :224 raw event.type; app.css:5365 outcomes same look
SCIM: :550 no origin chip; :1139 deprovision flag no glyph/CSS; :1155 group(s); 9 em-dashes
DEFINITIONS BUNDLE: :222 hand-written git banner vs GIT_DEFINITIONS_NOTICE; :227 only ref shown; :224 commands not mono
VALUES: :540 "absent" missing middot; :424 well-title id reuse, no anatomy; :597 empty no action; no S1 scan path
PROJECTS: :13 card no anatomy; :35 empty below form; :25 no-org wall
IMPORT WIZARD: :788 git notice concatenated; no step indicator; :231 refusal no recovery; :98 negative copy; 6 em-dashes
WORKSPACE/OIDC: OIDCDone.tsx:45 failure never rendered (navigates immediately); :59 "Reauthentication completed" for link/login; WorkspaceScope:262 reconnect anatomy + "session expired" wording; :68 polling degrade unstated; WorkspaceApprove:204 "Authorization unavailable"; CLIReauth:89 loading layout jump; WorkspaceCallback:40 dead setFailure
PLACEHOLDER: :10 hard-coded well-title id; :18 Overview no link; card h1
CSS: :4098/4126 hard-coded oklch on-accent ink -> token; :4128 identity-hue 30px no mobile; :4096 user preview radius; :4808 #ffffff -> --qr-paper
SYSTEMIC: two page anatomies; data-state unstyled; raw enums; absence x4; loading omissions; silent normalisation; em-dashes (contract conflict: GIT_DEFINITIONS_NOTICE + ui-spec copy contain em-dash); organisation spelling; recovery phrasing
MINE (real server): header shows principal id (fixed server-side); breadcrumb casing inconsistent (Overview/Projects vs members/settings/account); ops banners: Warning banner styled like Error (red) and third warning neutral; 7776000s raw seconds in instance policy row
(see agent output stored in session; key items)
P1 MatrixRowEditor.tsx:191 dialog unnamed -> aria-labelledby h2
P1 ScanWarnDialog.tsx:100 dialog unnamed
P1 clipboard.ts:21 wipes clipboard unconditionally -> compare readText first
P1 MatrixRowEditor.tsx:405 Copy offered without canReveal for secrets
P1 MatrixRowEditor.tsx:400 no sentence when reveal absent
P1 MatrixRowEditor.tsx:355 aria-label on plaintext p hides value; no aria-live countdown
P1 Values.tsx:542 same aria-label hiding plaintext
P2 app.css:2247 editor grid 2 cols -> auto-fit minmax(320px)
P2 MatrixRowEditor.tsx:238 schema raw JSON pre -> human summary below actions
P2 MatrixRowEditor.tsx:195 padding click closes dialog
P2 MatrixRowEditor.tsx:360 per-row Clear in editAll (Values says no per-row clear)
P2 Ceremony.tsx:215 "confirms a disclosure" for publish purpose
P2 Ceremony.tsx:291 window sentence missing
P2 MatrixRowEditor.tsx:424 secret copy gated off; protectedGuard dead
P2 MatrixRowEditor.tsx:459 protected copy asks twice (checkbox + ceremony)
P2 MatrixRowEditor.tsx:304 textarea never grows -> field-sizing: content
P2 MatrixRowEditor.tsx:646 any_of shows first alt error only
P2 MatrixRowEditor.tsx:395 Edit all one-way -> toggle
P2 ScanWarnDialog.tsx:160 third button "Dismiss for now" contradicts doc + spec
P2 app.css:2127,2175,2217,2227,2455,2815 containers use control radius
P2 em-dashes: Values.tsx:277, ScanWarnDialog.tsx:105, Ceremony.tsx:220, clipboard.ts:25, MatrixRowEditor.tsx:348 ('—' placeholder)
P2 MatrixRowEditor.tsx:406 / Values.tsx:571 copy label not saying audited disclosure
P2 Values.tsx:186 / useProtectedPublishCeremony.ts:74 one modal per env vs one enumerated set
P3 Ceremony.tsx:230 no lock glyph on key list
P3 Ceremony.tsx:281 Cancel placement / shared busy
P3 MatrixRowEditor.tsx:336 validation message no glyph
P3 MatrixRowEditor.tsx:296 role=alert at mount for existing problems
P3 MatrixRowEditor.tsx:290 "explicit absent" wording
