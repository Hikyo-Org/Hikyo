package securitymaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

var fixtureTime = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func fixtureIdentity(version string, sequence uint64) releaseidentity.Identity {
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: strings.Repeat("a", 40),
		CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility-" + version)), ManifestSHA256: releaseidentity.Hash([]byte("manifest-" + version))}
}

func fixtureCatalog() Catalog {
	return Catalog{Format: CatalogFormat, Sequence: 1, IssuedAt: fixtureTime.Add(-time.Hour), ExpiresAt: fixtureTime.Add(time.Hour),
		Advisories: []Advisory{{ID: "HK-SA-EXAMPLE", Summary: "Test-only security remediation", Severity: High, Components: []string{"hikyo"},
			Affected: []Affected{{Profile: releaseidentity.StableV1, Introduced: "1.0.0", Fixed: "1.3.1", Platforms: []string{"linux/amd64"}}}}},
		Targets: []Target{{Identity: fixtureIdentity("1.3.1", 8), Platforms: []string{"linux/amd64"}, Fixes: []string{"HK-SA-EXAMPLE"}, CoverageThrough: "2026-11-01",
			Impact: Impact{Restart: true, Features: true, Schema: true, Maintenance: true}}}}
}

func fixtureObservation() Observation {
	return Observation{Engine: releaseidentity.SQLite, Identity: fixtureIdentity("1.2.0", 7), Instance: "installation-example", Generation: 4, RestoreEpoch: 0,
		Incarnation: strings.Repeat("b", 64), Phase: "healthy", Ready: true}
}

func TestSecurityPolicySeparatesCoverageFromFeatureVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    func(*Policy)
		blocked string
	}{
		{"compatible route", func(*Policy) {}, ""},
		{"patch only", func(p *Policy) { p.UpgradeClass = "patch" }, "feature_upgrade_required"},
		{"pin", func(p *Policy) { p.Pin = "1.2.0" }, "application_pin"},
		{"manual", func(p *Policy) { p.Mode = Manual }, "manual_mode"},
		{"maintenance", func(p *Policy) { p.AllowMaintenance = false }, "maintenance_required"},
		{"window", func(p *Policy) { p.WindowStartMinute = 60; p.WindowDurationMinutes = 30 }, "maintenance_window"},
		{"channel", func(p *Policy) { p.Channel = updatecheck.ChannelNightly }, "channel_change"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultPolicy()
			tc.edit(&p)
			decision, err := Select(fixtureCatalog(), fixtureObservation().Identity, "linux/amd64", p, fixtureTime)
			if err != nil || decision.Blocked != tc.blocked || len(decision.Applicable) != 1 {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
			if (decision.Target != nil) != (tc.blocked == "") {
				t.Fatalf("unexpected target: %+v", decision)
			}
		})
	}
}

func TestSecurityPolicyWindowWrapAndCompleteCoverage(t *testing.T) {
	c := fixtureCatalog()
	c.IssuedAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c.ExpiresAt = c.IssuedAt.Add(24 * time.Hour)
	p := DefaultPolicy()
	p.WindowStartMinute = 23 * 60
	p.WindowDurationMinutes = 120
	if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", p, time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC)); err != nil || d.Target == nil {
		t.Fatalf("midnight window: %+v %v", d, err)
	}
	a := c.Advisories[0]
	a.ID = "HK-SA-SECOND"
	c.Advisories = append(c.Advisories, a)
	p = DefaultPolicy()
	if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", p, fixtureTime); err != nil || d.Target != nil || len(d.Applicable) != 2 || d.Blocked != "no_complete_security_target" {
		t.Fatalf("partial remediation accepted: %+v %v", d, err)
	}
}

func TestSecurityCatalogRejectsUnsafeCoverageAndAmbiguousInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Catalog)
	}{
		{"expired", func(c *Catalog) { c.ExpiresAt = fixtureTime }},
		{"future", func(c *Catalog) { c.IssuedAt = fixtureTime.Add(time.Hour); c.ExpiresAt = c.IssuedAt.Add(time.Hour) }},
		{"unbounded lifetime", func(c *Catalog) { c.ExpiresAt = c.IssuedAt.Add(8 * 24 * time.Hour) }},
		{"still affected", func(c *Catalog) { c.Targets[0].Identity = fixtureIdentity("1.2.1", 8) }},
		{"unknown fix", func(c *Catalog) { c.Targets[0].Fixes = []string{"unknown"} }},
		{"duplicate advisory", func(c *Catalog) { c.Advisories = append(c.Advisories, c.Advisories[0]) }},
		{"duplicate target", func(c *Catalog) { c.Targets = append(c.Targets, c.Targets[0]) }},
		{"mutable image", func(c *Catalog) { c.Targets[0].Image = "ghcr.io/example/hikyo:latest" }},
		{"unknown delivery", func(c *Catalog) { c.Advisories[0].Affected[0].Delivery = "remote-host" }},
		{"false date", func(c *Catalog) { c.Targets[0].CoverageThrough = "2099-01-01" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureCatalog()
			tc.edit(&c)
			raw, _ := json.Marshal(c)
			if _, err := DecodeCatalog(raw, fixtureTime); err == nil {
				t.Fatal("unsafe catalog accepted")
			}
		})
	}
	valid, _ := json.Marshal(fixtureCatalog())
	for _, raw := range [][]byte{append(valid, []byte(" {}")...), []byte(strings.Replace(string(valid), `"sequence":1`, `"sequence":1,"sequence":2`, 1)), []byte(strings.Replace(string(valid), `"sequence":1`, `"sequence":1,"unreviewed":true`, 1)), []byte("null")} {
		if _, err := DecodeCatalog(raw, fixtureTime); err == nil {
			t.Fatal("ambiguous closed artifact accepted")
		}
	}
}

func TestSecurityCoverageAndApplicabilityStayWithinArtifactDelivery(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "image"}[image], func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			binary := f.catalog.Targets[0]
			binary.CoverageThrough = "2026-10-31"
			f.catalog.Advisories[0].Affected[0].Delivery = "binary"
			imageAdvisory := f.catalog.Advisories[0]
			imageAdvisory.ID, imageAdvisory.Components = "HK-SA-IMAGE", []string{"image-base"}
			imageAdvisory.Affected = slices.Clone(imageAdvisory.Affected)
			imageAdvisory.Affected[0].Delivery = "image"
			f.catalog.Advisories = append(f.catalog.Advisories, imageAdvisory)
			oci := binary
			oci.Image, oci.Fixes, oci.CoverageThrough = "ghcr.io/example/hikyo@sha256:"+strings.Repeat("a", 64), []string{imageAdvisory.ID}, "2026-11-01"
			// Put OCI first so catalog order cannot give native its coverage.
			f.catalog.Targets = []Target{oci, binary}
			e.ImageDelivery = image
			decision, err := selectForDelivery(f.catalog, d.observed.Identity, e.Platform, e.Policy, fixtureTime, image)
			want := binary
			if image {
				want = oci
			}
			if err != nil || decision.Target == nil || decision.Target.Image != want.Image || !slices.Equal(decision.Applicable, want.Fixes) {
				t.Fatalf("artifact applicability escaped its scope: %+v %v", decision, err)
			}
			d.observed.Identity, d.observed.Image = want.Identity, want.Image
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), false)
			if err != nil || state.Phase != "current" || !slices.Equal(state.Coverage, want.Fixes) || state.CoverageThrough != want.CoverageThrough {
				t.Fatalf("current coverage came from another artifact: %+v %v", state, err)
			}
			if image {
				d.observed.Image = "ghcr.io/example/hikyo@sha256:" + strings.Repeat("d", 64)
				state, err = e.RunOnce(t.Context(), false)
				if err != nil || len(state.Coverage) != 0 || state.CoverageThrough != "" {
					t.Fatal("unlisted image borrowed another artifact's coverage", err)
				}
			}
		})
	}
}

func TestSecurityCoverageRefreshesWhenBlockedAfterCorrectionOrManualUpgrade(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrected bulletin", true: "manual artifact change"}[manual], func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			target := f.catalog.Targets[0]
			other := f.catalog.Advisories[0]
			other.ID, other.Components = "HK-SA-STILL-COVERED", []string{"other-component"}
			other.Affected = slices.Clone(other.Affected)
			f.catalog.Advisories = append(f.catalog.Advisories, other)
			f.catalog.Targets[0].Fixes = append(slices.Clone(target.Fixes), other.ID)
			d.observed.Identity = target.Identity
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), false)
			if err != nil || state.Phase != "current" || len(state.Coverage) != 2 {
				t.Fatal("initial verified coverage missing", err)
			}
			f.catalog.Sequence++
			if manual {
				d.observed.Identity = fixtureIdentity("1.3.2", 9)
				d.observed.Generation++
			}
			// Fresh authenticated metadata retracts the previous fix claim.
			// The still-running artifact is affected with no eligible target.
			f.catalog.Advisories[0].Affected[0].Fixed = "1.4.0"
			f.catalog.Targets[0].Fixes = []string{other.ID}
			f.catalog.Targets[0].CoverageThrough = "2026-10-31"
			state, err = e.RunOnce(t.Context(), false)
			wantIDs, wantComponents, wantDate := []string{other.ID}, other.Components, "2026-10-31"
			if manual {
				wantIDs, wantComponents, wantDate = nil, nil, ""
			}
			if err != nil || state.Phase != "blocked" || len(state.Applicable) != 1 || !slices.Equal(state.Coverage, wantIDs) || !slices.Equal(state.CoveredComponents, wantComponents) || state.CoverageThrough != wantDate || state.Running.Identity != d.observed.Identity {
				t.Fatalf("blocked status retained another catalog/artifact's coverage: %+v %v", state, err)
			}
		})
	}
}

func TestSecurityCompletionCannotRestoreRetractedCoverage(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.falseSuccess = true
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil {
		t.Fatal("fixture did not retain ambiguous replacement", err)
	}
	d.observed.Identity = state.Operation.Target.Identity
	d.observed.Generation++
	f.catalog.Sequence++
	f.catalog.Targets[0].Withdrawn = true
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Operation != nil || state.Phase != "healthy" || len(state.Coverage) != 0 || state.CoverageThrough != "" {
		t.Fatalf("operation reconciliation restored withdrawn coverage claims: %+v %v", state, err)
	}
}

func TestSecurityFinalRefreshRetractsSourceCoverageAndStagedSelection(t *testing.T) {
	e, f, d := fixtureEngine(t)
	source := f.catalog.Targets[0]
	d.observed.Identity = source.Identity
	remaining := f.catalog.Advisories[0]
	remaining.ID, remaining.Components = "HK-SA-RETAINED", []string{"other-component"}
	remaining.Affected = slices.Clone(remaining.Affected)
	pending := remaining
	pending.ID = "HK-SA-PENDING"
	pending.Affected = slices.Clone(pending.Affected)
	pending.Affected[0].Fixed = "1.4.0"
	f.catalog.Advisories = append(f.catalog.Advisories, remaining, pending)
	source.Fixes = []string{f.catalog.Advisories[0].ID, remaining.ID}
	target := source
	target.Identity = fixtureIdentity("1.4.0", 9)
	target.Fixes = append(slices.Clone(source.Fixes), pending.ID)
	f.catalog.Targets = []Target{source, target}
	enrollFixture(t, e)
	state, err := e.RunOnce(t.Context(), false)
	if err != nil || len(state.Coverage) != 2 || state.Selected == nil {
		t.Fatal("fixture did not establish covered source and applicable target", err)
	}
	f.calls = 0
	f.change = func(call int, c *Catalog) {
		if call == 2 {
			c.Sequence++
			c.Advisories[0].Affected[0].Fixed = "1.4.0"
			c.Targets[0].Fixes, c.Targets[0].CoverageThrough = []string{remaining.ID}, "2026-10-31"
			c.Targets[1].Withdrawn = true
		}
	}
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.CatalogSequence != 2 || state.Phase != "blocked" || state.Reason != "target_withdrawn_or_changed" || state.Operation != nil || state.Selected != nil || d.applies() != 0 || !slices.Equal(state.Coverage, []string{remaining.ID}) || !slices.Equal(state.CoveredComponents, remaining.Components) || state.CoverageThrough != "2026-10-31" || len(state.Applicable) != 2 {
		t.Fatalf("final metadata inherited retracted coverage/selection/applicability: %+v %v", state, err)
	}
}

func TestSecurityFreshFeedCannotRenewUnobservedCoverage(t *testing.T) {
	e, f, d := fixtureEngine(t)
	d.observed.Identity = f.catalog.Targets[0].Identity
	enrollFixture(t, e)
	if state, err := e.RunOnce(t.Context(), false); err != nil || len(state.Coverage) != 1 {
		t.Fatal("initial running coverage missing", err)
	}
	f.catalog.Sequence++
	d.observeFailure = errors.New("source cannot be observed")
	state, err := e.RunOnce(t.Context(), false)
	if err == nil || state.CatalogSequence != 2 || state.Reason != "installation_unavailable" || len(state.Coverage) != 0 || state.CoverageThrough != "" {
		t.Fatalf("fresh feed renewed old unobserved coverage: %+v %v", state, err)
	}
}

func TestSecurityOutcomeStatusUsesActualObservationAfterPossibleMutation(t *testing.T) {
	for _, scenario := range []string{"ambiguous apply unavailable", "ambiguous apply unhealthy", "success observation unavailable", "different running artifact", "definite refusal"} {
		t.Run(scenario, func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			source := f.catalog.Targets[0]
			d.observed.Identity = source.Identity
			pending := f.catalog.Advisories[0]
			pending.ID, pending.Affected = "HK-SA-PENDING", slices.Clone(pending.Affected)
			pending.Affected[0].Fixed = "1.4.0"
			f.catalog.Advisories = append(f.catalog.Advisories, pending)
			target := source
			target.Identity, target.Fixes = fixtureIdentity("1.4.0", 10), append(slices.Clone(source.Fixes), pending.ID)
			f.catalog.Targets = []Target{source, target}
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), false)
			if err != nil || len(state.Coverage) != 1 || state.Selected == nil {
				t.Fatal("fixture did not establish covered source and pending remediation", err)
			}
			switch scenario {
			case "ambiguous apply unavailable":
				d.applyFailure = true
				d.duringApply = func() { d.observeFailure = errors.New("workload outcome unavailable") }
			case "ambiguous apply unhealthy":
				d.applyFailure = true
				d.duringApply = func() {
					d.observed.Identity, d.observed.Ready, d.observed.Maintenance, d.observed.Phase = target.Identity, false, true, "schema-applied"
					d.observed.Generation++
				}
			case "success observation unavailable":
				d.duringApply = func() { d.observeFailure = errors.New("replacement cannot be observed") }
			case "different running artifact":
				d.falseSuccess = true
				d.duringApply = func() {
					d.observed.Identity = fixtureIdentity("1.3.2", 9)
					d.observed.Generation++
				}
			case "definite refusal":
				d.beforeApply = func(Target) error { return ErrNotStarted }
			}
			state, err = e.RunOnce(t.Context(), true)
			if err == nil {
				t.Fatal("unproven/refused update reported success")
			}
			if scenario == "definite refusal" {
				if state.Phase != "blocked" || state.Operation != nil || state.Running == nil || state.Running.Identity != source.Identity || !slices.Equal(state.Coverage, source.Fixes) {
					t.Fatalf("definite refusal lost newly observed unchanged source: %+v", state)
				}
				return
			}
			if state.Phase != "failed" || state.Operation == nil || state.Operation.Source.Identity != source.Identity || len(state.Coverage) != 0 || state.CoverageThrough != "" {
				t.Fatalf("possible mutation reused stopped source claims or lost recovery context: %+v", state)
			}
			if scenario == "different running artifact" {
				if state.Running == nil || state.Running.Identity != d.observed.Identity || !slices.Equal(state.Applicable, []string{pending.ID}) {
					t.Fatal("failure retained the predecessor rather than actual artifact")
				}
			} else if scenario == "ambiguous apply unhealthy" {
				if state.Running == nil || state.Running.Ready || state.Running.Identity != target.Identity {
					t.Fatal("failed target observation was replaced with the old healthy source")
				}
			} else if state.Running != nil || len(state.Applicable) != 0 {
				t.Fatal("unavailable observation retained current artifact claims")
			}
		})
	}
}

type fixtureFeed struct {
	catalog Catalog
	calls   int
	failAt  int
	change  func(int, *Catalog)
}

func (f *fixtureFeed) Refresh(context.Context) (securityfeed.Document, error) {
	f.calls++
	if f.failAt == f.calls {
		return securityfeed.Document{}, errors.New("test publisher unavailable")
	}
	if f.change != nil {
		f.change(f.calls, &f.catalog)
	}
	raw, err := json.Marshal(f.catalog)
	return securityfeed.Document{Data: raw, Version: f.catalog.Sequence, Expires: f.catalog.ExpiresAt}, err
}

type fixtureDriver struct {
	observed       Observation
	observeFailure error
	calls          []string
	prepareImpact  Impact
	duringPrepare  func()
	duringObserve  func()
	duringApply    func()
	beforeApply    func(Target) error
	applyFailure   bool
	falseSuccess   bool
}

func (d *fixtureDriver) Observe(context.Context) (Observation, error) {
	d.calls = append(d.calls, "observe")
	if d.duringObserve != nil {
		d.duringObserve()
	}
	return d.observed, d.observeFailure
}
func (d *fixtureDriver) Prepare(_ context.Context, target Target) (Preparation, error) {
	d.calls = append(d.calls, "prepare")
	source := d.observed
	if d.duringPrepare != nil {
		d.duringPrepare()
	}
	return Preparation{Target: target.Identity, Source: source, Image: target.Image, RouteSHA256: releaseidentity.Hash([]byte("verified route")), Impact: d.prepareImpact}, nil
}
func (d *fixtureDriver) Apply(_ context.Context, target Target) error {
	d.calls = append(d.calls, "apply")
	if d.beforeApply != nil {
		if err := d.beforeApply(target); err != nil {
			return err
		}
	}
	if d.duringApply != nil {
		d.duringApply()
	}
	if d.applyFailure {
		return errors.New("test failure after possible schema writes")
	}
	if !d.falseSuccess {
		d.observed.Identity = target.Identity
		d.observed.Generation++
		d.observed.Image = target.Image
	}
	return nil
}
func (d *fixtureDriver) applies() int {
	n := 0
	for _, c := range d.calls {
		if c == "apply" {
			n++
		}
	}
	return n
}

func fixtureEngine(t *testing.T) (*Engine, *fixtureFeed, *fixtureDriver) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local state ownership requires the Unix installation contract")
	}
	store, err := NewStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixtureFeed{catalog: fixtureCatalog()}
	d := &fixtureDriver{observed: fixtureObservation()}
	e := &Engine{Store: store, Feed: f, Driver: d, Platform: "linux/amd64", Policy: DefaultPolicy(), Now: func() time.Time { return fixtureTime }}
	return e, f, d
}

func TestSecurityMaintenanceWindowDeadlineSurvivesLongFinalValidation(t *testing.T) {
	for _, elapsed := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline delivered", true: "window closed during observation"}[elapsed], func(t *testing.T) {
			e, _, d := fixtureEngine(t)
			enrollFixture(t, e)
			now := fixtureTime.Add(30 * time.Second)
			e.Now = func() time.Time { return now }
			e.Policy.WindowStartMinute, e.Policy.WindowDurationMinutes = 12*60, 1
			observations := 0
			d.duringObserve = func() {
				observations++
				if elapsed && observations == 2 {
					now = fixtureTime.Add(time.Minute)
				}
			}
			d.beforeApply = func(target Target) error {
				if !target.EligibilityUntil.Equal(fixtureTime.Add(time.Minute)) {
					t.Fatalf("window end not bound to deployment admission: %s", target.EligibilityUntil)
				}
				return nil
			}
			state, err := e.RunOnce(t.Context(), true)
			if err != nil {
				t.Fatal(err)
			}
			if elapsed && (d.applies() != 0 || state.Reason != "maintenance_window" || state.Operation != nil) {
				t.Fatalf("closed window admitted mutation: %+v", state)
			}
			if !elapsed && (state.Phase != "healthy" || d.applies() != 1) {
				t.Fatalf("eligible operation refused: %+v", state)
			}
		})
	}
}

func TestSecurityMaintenanceRuntimeDigestProofMatchesDeploymentContract(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(map[bool]string{false: "Compose equivalent image proof", true: "runtime manifest required"}[required], func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			e.ImageDelivery, e.RequireRuntimeDigest = true, required
			f.catalog.Targets[0].Image = "ghcr.io/example/hikyo@sha256:" + strings.Repeat("a", 64)
			f.catalog.Targets[0].RuntimeImageSHA256 = releaseidentity.Digest(strings.Repeat("c", 64))
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), true)
			if !required {
				if err != nil || state.Phase != "healthy" || state.Operation != nil {
					t.Fatalf("equivalent Docker image/payload proof rejected: %+v %v", state, err)
				}
				return
			}
			if err == nil || state.Operation == nil || !state.Operation.RequireRuntimeDigest {
				t.Fatalf("missing runtime digest counted as completion: %+v %v", state, err)
			}
			// A restart or local setting change cannot weaken the pending proof.
			e.RequireRuntimeDigest = false
			if state, err = e.RunOnce(t.Context(), true); err == nil || state.Operation == nil || d.applies() != 1 {
				t.Fatal("reopened checker weakened the durable completion requirement", err)
			}
			d.observed.RuntimeImageSHA256 = f.catalog.Targets[0].RuntimeImageSHA256
			if state, err = e.RunOnce(t.Context(), true); err != nil || state.Operation != nil || state.Phase != "healthy" {
				t.Fatal("exact runtime proof did not reconcile original operation", err)
			}
		})
	}
}

func enrollFixture(t *testing.T, e *Engine) {
	t.Helper()
	if _, err := e.Enroll(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityMaintenanceEndToEndStateAndExactRunningProof(t *testing.T) {
	e, f, d := fixtureEngine(t)
	state, err := e.RunOnce(t.Context(), true)
	if err != nil || state.Reason != "explicit_enrollment_required" || d.applies() != 0 {
		t.Fatalf("implicit adoption: %+v %v", state, err)
	}
	enrollFixture(t, e)
	state, err = e.RunOnce(t.Context(), false)
	if err != nil || state.Phase != "applicable" || d.applies() != 0 {
		t.Fatalf("read-only check applied: %+v %v", state, err)
	}
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" || state.Operation != nil || state.Running.Identity != f.catalog.Targets[0].Identity || len(state.Applicable) != 0 || d.applies() != 1 {
		t.Fatalf("automatic replacement failed: %+v %v", state, err)
	}
	persisted, err := e.Store.Load()
	if err != nil || persisted.Phase != "healthy" || persisted.Running.Generation != 5 {
		t.Fatalf("outcome not durable: %+v %v", persisted, err)
	}
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "current" || d.applies() != 1 {
		t.Fatalf("already installed repeated: %+v %v", state, err)
	}
}

func TestSecurityMaintenanceRefusesWithdrawalOutageAndSourceDriftBeforeStop(t *testing.T) {
	for _, name := range []string{"withdrawal", "feed outage", "source drift", "measured maintenance"} {
		t.Run(name, func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			enrollFixture(t, e)
			switch name {
			case "withdrawal":
				f.change = func(call int, c *Catalog) {
					if call == 2 {
						c.Sequence++
						c.Targets[0].Withdrawn = true
					}
				}
			case "feed outage":
				f.failAt = 2
			case "source drift":
				d.duringPrepare = func() { d.observed.Generation++ }
			case "measured maintenance":
				e.Policy.AllowMaintenance = false
				f.catalog.Targets[0].Impact.Schema = false
				f.catalog.Targets[0].Impact.Maintenance = false
				d.prepareImpact.Maintenance = true
			}
			state, _ := e.RunOnce(t.Context(), true)
			if d.applies() != 0 || state.Operation != nil || state.Phase == "healthy" {
				t.Fatalf("unsafe apply: %+v calls=%v", state, d.calls)
			}
			if !d.observed.Ready {
				t.Fatal("healthy source was stopped by a preapply failure")
			}
		})
	}
}

func TestSecurityMaintenancePinsUncertainOperationAcrossRestart(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.falseSuccess = true
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil || state.Reason != "running_target_unproven" {
		t.Fatalf("false success: %+v %v", state, err)
	}
	selected := state.Operation.Target.Identity
	f.catalog.Sequence++
	newer := f.catalog.Targets[0]
	newer.Identity = fixtureIdentity("1.4.0", 9)
	f.catalog.Targets = append(f.catalog.Targets, newer)
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || d.applies() != 1 || state.Operation.Target.Identity != selected || state.Reason != "operation_requires_reconciliation" {
		t.Fatalf("pending operation stolen: %+v %v", state, err)
	}
	// Independent deployment-owner reconciliation can finish the same exact
	// operation. The checker observes that outcome; it never restarts old code.
	d.observed.Identity = selected
	d.observed.Generation++
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" || state.Operation != nil || d.applies() != 1 {
		t.Fatalf("exact recovery not observed: %+v %v", state, err)
	}
}

func TestSecurityMaintenanceOutageAndRestoreDoNotResetTrustOrEnrollment(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	if _, err := e.RunOnce(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	f.failAt = f.calls + 1
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Phase != "stale" || state.CatalogSequence != 1 || d.applies() != 0 || !d.observed.Healthy() {
		t.Fatalf("outage changed service or trust: %+v %v", state, err)
	}
	f.failAt = 0
	f.catalog.Sequence = 2
	d.observed.Incarnation = strings.Repeat("c", 64)
	d.observed.RestoreEpoch++
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || state.Reason != "recovery_incarnation_changed" || d.applies() != 0 {
		t.Fatalf("restored authority reused: %+v %v", state, err)
	}
	if _, err = e.Enroll(t.Context()); err == nil {
		t.Fatal("re-enroll silently reset recovery authority")
	}
	// The authenticated sequence survives the independent observation refusal.
	f.catalog.Sequence = 1
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || state.Reason != "catalog_rollback_or_equivocation" {
		t.Fatalf("floor lost after refusal: %+v %v", state, err)
	}
}

func TestSecurityMaintenanceStateRefusesCorruptionAndSymlink(t *testing.T) {
	e, _, d := fixtureEngine(t)
	enrollFixture(t, e)
	path := filepath.Join(e.Store.directory, "status.json")
	if err := os.WriteFile(path, []byte(`{"format":"hikyo.security-maintenance-status/v1","phase":"healthy","catalog_sequence":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RunOnce(t.Context(), true); err == nil {
		t.Fatal("corrupt floor accepted")
	}
	if d.applies() != 0 {
		t.Fatal("corrupt state applied")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(out, []byte("protected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, path); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Load(); err == nil {
		t.Fatal("state symlink followed")
	}
	raw, err := os.ReadFile(out)
	if err != nil || string(raw) != "protected" {
		t.Fatal("outside file changed")
	}
}
