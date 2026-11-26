package ci_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A replay of the repository's Actions cache against GitHub's eviction, built from the listing taken
// on 2026-11-25 (testdata/cache-listing-2026-11-25.json: real keys, sizes, creation and last-access
// times) and the prune workflow's own jq. Assumptions that are not in the listing are labelled here
// and in docs/development/ci.md:
//   - the limit is 10 GB or 10 GiB (GitHub documents "10 GB" and does not say which it enforces), and an entry is evicted the moment the total passes it (the earliest
//     possible eviction, the worst case for the seeds);
//   - eviction takes the least recently accessed entry first, and a tie goes against the seeds;
//   - the feedback seeds (0.69 GB main, 0.76 GB dev) and the two pnpm stores (0.205 GB each, one
//     measured on macOS arm64 with zstd, the Playwright one assumed equal) are not in the listing;
//   - a weekday: every entry the listing shows touched within 90 minutes of the listing is touched
//     every hour; the others (CodeQL overlays, kind, buildx, the old feedback entries) are touched
//     only when created;
//   - a quiet weekend: nothing is touched after Friday 20:30 except what a schedule saves.

type modelEntry struct {
	apiCacheEntry
	created, accessed time.Time
	seed              string
	busy              bool
}

type cacheModel struct {
	t        *testing.T
	limit    int64
	program  string
	entries  []*modelEntry
	now      time.Time
	nextID   int
	start    int64
	demand   int64
	peak     int64
	low      int64
	evicted  []string
	seedLost []string
}

type modelListing struct {
	ListedAt string          `json:"listed_at"`
	Caches   []apiCacheEntry `json:"actions_caches"`
}

func (m *cacheModel) total() (sum int64) {
	for _, e := range m.entries {
		sum += e.Size
	}
	return sum
}

func (m *cacheModel) add(key, ref string, size int64, seed string, busy bool) *modelEntry {
	m.nextID++
	e := &modelEntry{apiCacheEntry: apiCacheEntry{ID: m.nextID, Ref: ref, Key: key, Size: size}, created: m.now, accessed: m.now, seed: seed, busy: busy}
	m.entries = append(m.entries, e)
	return e
}

// settle records the peak, then evicts least recently accessed first until the total fits.
func (m *cacheModel) settle() {
	if total := m.total(); total > m.peak {
		m.peak = total
	}
	for m.total() > m.limit {
		sort.SliceStable(m.entries, func(i, j int) bool {
			a, b := m.entries[i], m.entries[j]
			if !a.accessed.Equal(b.accessed) {
				return a.accessed.Before(b.accessed)
			}
			if (a.seed != "") != (b.seed != "") {
				return a.seed != ""
			}
			return a.created.Before(b.created)
		})
		victim := m.entries[0]
		m.entries = m.entries[1:]
		if victim.seed != "" && m.newestSeed(victim) {
			m.seedLost = append(m.seedLost, fmt.Sprintf("%s %s at %s", victim.seed, victim.Key[len(victim.Key)-60:len(victim.Key)-50], m.now.Format("Mon 15:04")))
		} else if victim.seed == "" {
			m.evicted = append(m.evicted, victim.Key[:min(len(victim.Key), 60)])
		}
	}
}

// newestSeed reports whether the entry belongs to its family's newest set; an older set is the one
// the seed's own prune is about to delete, so losing it costs nothing.
func (m *cacheModel) newestSeed(victim *modelEntry) bool {
	for _, e := range m.entries {
		if e.seed == victim.seed && e.created.After(victim.created) {
			return false
		}
	}
	return true
}

// prune applies the workflow's jq to the simulated listing.
func (m *cacheModel) prune() {
	page := make([]apiCacheEntry, 0, len(m.entries))
	for _, e := range m.entries {
		c := e.apiCacheEntry
		c.Created = e.created.UTC().Format(time.RFC3339Nano)
		c.Accessed = e.accessed.UTC().Format(time.RFC3339Nano)
		page = append(page, c)
	}
	raw, err := json.Marshal(map[string]any{"actions_caches": page})
	if err != nil {
		m.t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), "jq", "-s", "-r", "--argjson", "now", strconv.FormatInt(m.now.Unix(), 10), m.program)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		m.t.Fatalf("jq: %v", err)
	}
	gone := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		id, err := strconv.Atoi(strings.Split(line, "\t")[0])
		if err != nil {
			m.t.Fatal(err)
		}
		gone[id] = true
	}
	kept := m.entries[:0]
	for _, e := range m.entries {
		if !gone[e.ID] {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	if total := m.total(); m.low == 0 || total < m.low {
		m.low = total
	}
}

// replaceSeed saves a new feedback set and then drops the family's older one, as the seed's own
// prune job does right after the save.
func (m *cacheModel) replaceSeed(family string, sizes [3]int64) {
	var old []*modelEntry
	for _, e := range m.entries {
		if e.seed == family {
			old = append(old, e)
		}
	}
	ref := "refs/heads/" + family
	for i, entry := range []string{"application", "cli", "tooling"} {
		m.add(fmt.Sprintf("go-feedback-v2-Linux-X64-ubuntu26-%s-%s-%s-%s-%s", family, entry, strings.Repeat("c", 64), strings.Repeat("d", 64), m.now.Format("2006-01-02")), ref, sizes[i], family, true)
	}
	m.settle()
	for _, o := range old {
		for i, e := range m.entries {
			if e == o {
				m.entries = append(m.entries[:i], m.entries[i+1:]...)
				break
			}
		}
	}
}

func (m *cacheModel) bump() {
	for _, e := range m.entries {
		if e.busy {
			// Runs restore an entry at some point in the hour, not all at the same second.
			e.accessed = m.now.Add(-time.Duration(fnv32(e.Key)%50) * time.Minute)
		}
	}
}

func fnv32(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}

type modelEvent struct {
	at  time.Time
	run func()
}

func (m *cacheModel) play(events []modelEvent) {
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	for _, ev := range events {
		m.now = ev.at
		ev.run()
		m.settle()
	}
}

func hourly(from, to time.Time, minute int, run func()) []modelEvent {
	var events []modelEvent
	for t := from.Truncate(time.Hour).Add(time.Duration(minute) * time.Minute); t.Before(to); t = t.Add(time.Hour) {
		if t.After(from) {
			events = append(events, modelEvent{t, run})
		}
	}
	return events
}

var (
	seedSizesMain = [3]int64{230e6, 220e6, 240e6}
	seedSizesDev  = [3]int64{250e6, 240e6, 270e6}
	overlayTail   = regexp.MustCompile(`-[0-9a-f]{40}-[0-9]+-[0-9]+$`)
	unfamilied    = regexp.MustCompile(`^go-feedback-v2-Linux-X64-ubuntu26-[0-9a-f]{64}-[0-9a-f]{40}$`)
)

// modelOpts selects the repository's limit and its state: quiet marks a weekend (nothing is busy)
// and scales are measured shrinks applied to the entries whose key starts with the map key.
type modelOpts struct {
	quiet  bool
	scales map[string]float64
	limit  int64
}

// newCacheModel loads the listing, applies the prune once and the by-hand deletion of the unfamilied
// seeds, and adds what the listing does not hold.
func newCacheModel(t *testing.T, o modelOpts) (*cacheModel, []apiCacheEntry, time.Time) {
	t.Helper()
	repo := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(repo, cachePruneWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	program := strings.SplitN(strings.SplitN(string(raw), "--argjson now \"$now\" '", 2)[1], "' >", 2)[0]
	raw, err = os.ReadFile(filepath.Join(repo, "scripts/ci/testdata/cache-listing-2026-11-25.json"))
	if err != nil {
		t.Fatal(err)
	}
	var listing modelListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	listedAt, err := time.Parse(time.RFC3339, listing.ListedAt)
	if err != nil {
		t.Fatal(err)
	}
	m := &cacheModel{t: t, program: program, now: listedAt, nextID: 1 << 20, limit: o.limit}
	for _, c := range listing.Caches {
		created, err := time.Parse(time.RFC3339Nano, c.Created)
		if err != nil {
			t.Fatal(err)
		}
		accessed, err := time.Parse(time.RFC3339Nano, c.Accessed)
		if err != nil {
			t.Fatal(err)
		}
		for prefix, scale := range o.scales {
			if strings.HasPrefix(c.Key, prefix) {
				c.Size = int64(float64(c.Size) * scale)
			}
		}
		m.entries = append(m.entries, &modelEntry{apiCacheEntry: c, created: created, accessed: accessed, busy: !o.quiet && listedAt.Sub(accessed) <= 90*time.Minute})
	}
	before := append([]apiCacheEntry(nil), listing.Caches...)
	m.prune()
	kept := m.entries[:0]
	for _, e := range m.entries {
		if !unfamilied.MatchString(e.Key) {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	m.demand = m.total() + 205e6*2 + seedSizesMain[0] + seedSizesMain[1] + seedSizesMain[2] + seedSizesDev[0] + seedSizesDev[1] + seedSizesDev[2]
	m.add("pnpm-store-v2-host-Linux-X64-ubuntu26-"+strings.Repeat("e", 64), "refs/heads/main", 205e6, "", !o.quiet)
	m.add("pnpm-store-v2-playwright-Linux-X64-ubuntu26-"+strings.Repeat("e", 64), "refs/heads/main", 205e6, "", !o.quiet)
	m.replaceSeed("main", seedSizesMain)
	m.replaceSeed("dev", seedSizesDev)
	for _, e := range m.entries {
		if e.seed != "" {
			e.busy = !o.quiet
		}
	}
	m.peak, m.low = 0, 0
	m.evicted, m.seedLost = nil, nil
	m.start = m.total()
	return m, before, listedAt
}

// requireDemand pins the documented figure: the bytes the live entries, the seeds and the pnpm
// stores need once the prune has run, before GitHub evicts anything.
func (m *cacheModel) requireDemand(t *testing.T, wantGB float64) {
	t.Helper()
	if got := float64(m.demand) / 1e9; got < wantGB-0.011 || got > wantGB+0.011 {
		t.Fatalf("demand after the prune is %.2f GB, documented %.2f GB", got, wantGB)
	}
}

// requirePeak pins the documented peak: the most bytes the repository holds at any moment of the
// replay, before GitHub evicts for it.
func (m *cacheModel) requirePeak(t *testing.T, wantGB float64) {
	t.Helper()
	if got := float64(m.peak) / 1e9; got < wantGB-0.011 || got > wantGB+0.011 {
		t.Fatalf("peak is %.2f GB, documented %.2f GB", got, wantGB)
	}
}

func (m *cacheModel) report(name string) string {
	return fmt.Sprintf("%s (limit %.2f GB): demand after the prune %.2f GB (margin %+.2f), start %.2f, low point %.2f, peak %.2f (margin %+.2f), evicted %d live entries %v, seeds lost %v",
		name, float64(m.limit)/1e9, float64(m.demand)/1e9, float64(m.limit-m.demand)/1e9, float64(m.start)/1e9, float64(m.low)/1e9, float64(m.peak)/1e9, float64(m.limit-m.peak)/1e9, len(m.evicted), m.evicted, m.seedLost)
}

// plan is what the repository does: the shrinks it adopts, whether the prune also runs minutes after
// each writer finishes (the workflow_run trigger of ci-cache-prune.yml; the hourly :47 run always
// exists) and whether dev's seed is refreshed daily at 03:44, before the Monday rollover.
type plan struct {
	scales      map[string]float64
	writerPrune bool
	devRefresh  bool
}

var (
	// Before this round: nothing adopted, only the hourly prune.
	planBefore = plan{}
	// The adopted cuts: DWARF off in the k8s-e2e entry (898 to 755 MB zstd, x0.84) and in the lint
	// driver (1135 to 905 MB, x0.80), the module cache holding only cache/download (791 to 437 MB,
	// x0.55; see docs/development/ci.md for what the restore costs), the prune after each writer,
	// and the daily dev seed refresh.
	planAdopted = plan{
		scales:      map[string]float64{"go-k8s-e2e-v2-": 0.84, "go-lintdriver-v2-": 0.80, "go-mod-v2-": 0.55},
		writerPrune: true,
		devRefresh:  true,
	}
	limits = []struct {
		name  string
		bytes int64
	}{{"10 GB", 10_000_000_000}, {"10 GiB", 10 << 30}}
)

// afterWriter runs the prune two minutes after a writer saved, when the plan has that trigger.
func (p plan) afterWriter(m *cacheModel, at time.Time) []modelEvent {
	if !p.writerPrune {
		return nil
	}
	return []modelEvent{{at.Add(2 * time.Minute), m.prune}}
}

// weekday replays 24 hours from the listing: the measured per-push CodeQL churn (the listing's overlay
// entries, one day later), the main seed at 03:32 (saved by 03:42), the dev seed refreshed at 03:44
// (saved by 03:55) or else saved by a dev push at 09:10, and the prune every hour at :47.
func weekday(t *testing.T, p plan, limit int64) *cacheModel { return weekdayWith(t, p, limit, nil) }

// weekdayWith is weekday plus the events of the day's own saves.
func weekdayWith(t *testing.T, p plan, limit int64, extra func(*cacheModel) []modelEvent) *cacheModel {
	m, before, listedAt := newCacheModel(t, modelOpts{scales: p.scales, limit: limit})
	m.low, m.peak = 0, 0
	start, end := listedAt, listedAt.Add(24*time.Hour)
	m.now = start
	var events []modelEvent
	events = append(events, hourly(start, end, 0, m.bump)...)
	events = append(events, hourly(start, end, 47, m.prune)...)
	n := 0
	for _, c := range before {
		if !strings.HasPrefix(c.Key, "codeql-overlay-base-database-") {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, c.Created)
		if err != nil {
			t.Fatal(err)
		}
		key := overlayTail.ReplaceAllString(c.Key, fmt.Sprintf("-%040x-%d-1", n, 900+n))
		size := c.Size
		n++
		at := created.Add(24 * time.Hour)
		events = append(events, modelEvent{at, func() { m.add(key, "refs/heads/main", size, "", false) }})
		events = append(events, p.afterWriter(m, at)...)
	}
	events = append(events, modelEvent{time.Date(2026, 11, 26, 3, 42, 0, 0, time.UTC), func() { m.replaceSeed("main", seedSizesMain) }})
	if p.devRefresh {
		events = append(events, modelEvent{time.Date(2026, 11, 26, 3, 55, 0, 0, time.UTC), func() { m.replaceSeed("dev", seedSizesDev) }})
	} else {
		events = append(events, modelEvent{time.Date(2026, 11, 26, 9, 10, 0, 0, time.UTC), func() { m.replaceSeed("dev", seedSizesDev) }})
	}
	if extra != nil {
		events = append(events, extra(m)...)
	}
	m.play(events)
	return m
}

var hash64 = regexp.MustCompile(`[0-9a-f]{64}`)

// rotate saves a new generation of every main entry whose key starts with prefix, as a go.mod or
// go.sum change does: the same lane with a new content hash, next to the old entry until the prune.
func (m *cacheModel) rotate(prefix string, busy bool) {
	var old []*modelEntry
	for _, e := range m.entries {
		if strings.HasPrefix(e.Key, prefix) && e.Ref == "refs/heads/main" {
			old = append(old, e)
		}
	}
	if len(old) == 0 {
		m.t.Fatalf("no %s entry to rotate", prefix)
	}
	for _, e := range old {
		m.add(hash64.ReplaceAllString(e.Key, strings.Repeat("f", 64)), e.Ref, e.Size, "", busy)
	}
}

// touch marks every entry whose key starts with prefix as restored now, as a run that restores the
// old generation before it saves the new one does.
func (m *cacheModel) touch(prefix string) {
	for _, e := range m.entries {
		if strings.HasPrefix(e.Key, prefix) {
			e.accessed = m.now
		}
	}
}

// rotationDay is an ordinary weekday on which go.mod or go.sum changed on main. Everything keyed on
// them rotates at its next save: the daily race seed (03:41, which also saves go-mod-v2 on a miss),
// the daily go-feedback-v1 save (04:00 assumed; the time is not in the listing) and the lint driver
// seed (04:11), each next to its old generation until the prune two minutes later.
func rotationDay(t *testing.T, p plan, limit int64) *cacheModel {
	return weekdayWith(t, p, limit, func(m *cacheModel) []modelEvent {
		day := func(h, min int) time.Time { return time.Date(2026, 11, 26, h, min, 0, 0, time.UTC) }
		var events []modelEvent
		for _, w := range []struct {
			at       time.Time
			prefixes []string
		}{
			{day(3, 41), []string{"go-race-v2-", "go-mod-v2-"}},
			{day(4, 0), []string{"go-feedback-v1-"}},
			{day(4, 11), []string{"go-lintdriver-v2-"}},
		} {
			events = append(events, modelEvent{w.at, func() {
				for _, prefix := range w.prefixes {
					m.rotate(prefix, true)
				}
			}})
			events = append(events, p.afterWriter(m, w.at)...)
		}
		return events
	})
}

// sundayGoSum is the weekly ci-main run (13:23) after a go.sum change landed since the daily seeds
// ran: it restores the old generation of the module, test, lint, generated, k8s-e2e and fuzz entries
// and saves the new one of each, about 3.3 GB at once, on a quiet weekend.
func sundayGoSum(t *testing.T, p plan, limit int64) *cacheModel {
	m, _, _ := newCacheModel(t, modelOpts{quiet: true, scales: p.scales, limit: limit})
	friday := time.Date(2026, 11, 27, 20, 30, 0, 0, time.UTC)
	for _, e := range m.entries {
		e.accessed, e.created = friday, friday
		switch e.seed {
		case "main":
			e.accessed = time.Date(2026, 11, 29, 3, 42, 0, 0, time.UTC)
		case "dev":
			if p.devRefresh {
				e.accessed = time.Date(2026, 11, 29, 3, 55, 0, 0, time.UTC)
			}
		}
	}
	m.now = time.Date(2026, 11, 29, 13, 0, 0, 0, time.UTC)
	m.low, m.peak = 0, 0
	save := time.Date(2026, 11, 29, 13, 23, 0, 0, time.UTC)
	events := []modelEvent{{save, func() {
		for _, prefix := range []string{"go-mod-v2-", "go-test-v2-", "go-lint-v2-", "go-generated-v2-", "go-k8s-e2e-v2-", "go-fuzz-v2-"} {
			m.touch(prefix)
			m.rotate(prefix, false)
		}
	}}}
	events = append(events, p.afterWriter(m, save)...)
	events = append(events, hourly(m.now, m.now.Add(8*time.Hour), 47, m.prune)...)
	m.play(events)
	return m
}

// monday replays Monday 04:00 to 12:00 after a quiet weekend: the weekly lint driver entry is saved at
// 04:11. The old week's entry was last looked up at 04:10, fresher than every seed that nothing
// refreshed; the main seed was saved at 03:32 (at 03:42 by the time it is in), and the dev seed was
// last restored on Friday unless the daily refresh ran at 03:44.
func monday(t *testing.T, p plan, limit int64) *cacheModel {
	m, _, _ := newCacheModel(t, modelOpts{quiet: true, scales: p.scales, limit: limit})
	friday := time.Date(2026, 11, 27, 20, 30, 0, 0, time.UTC)
	start := time.Date(2026, 11, 30, 4, 0, 0, 0, time.UTC)
	var old *modelEntry
	for _, e := range m.entries {
		if e.seed != "main" && e.created.Before(start) {
			e.accessed = friday
		}
		if strings.HasPrefix(e.Key, "go-lintdriver-v2-") {
			old = e
		}
		if e.seed == "dev" {
			e.accessed = friday
		}
		if e.seed == "main" {
			e.created, e.accessed = time.Date(2026, 11, 30, 3, 42, 0, 0, time.UTC), time.Date(2026, 11, 30, 3, 42, 0, 0, time.UTC)
		}
	}
	old.accessed = time.Date(2026, 11, 30, 4, 10, 0, 0, time.UTC)
	size := old.Size
	m.now = start
	m.low, m.peak = 0, 0
	var events []modelEvent
	if p.devRefresh {
		events = append(events, modelEvent{time.Date(2026, 11, 30, 3, 55, 0, 0, time.UTC), func() { m.replaceSeed("dev", seedSizesDev) }})
	}
	save := time.Date(2026, 11, 30, 4, 11, 0, 0, time.UTC)
	events = append(events, modelEvent{save, func() {
		m.add("go-lintdriver-v2-Linux-X64-ubuntu26-"+strings.Repeat("a", 64)+"-2026-W49", "refs/heads/main", size, "", false)
	}})
	events = append(events, p.afterWriter(m, save)...)
	events = append(events, hourly(start, start.Add(8*time.Hour), 47, m.prune)...)
	m.play(events)
	return m
}

// sunday replays Sunday 20:00 to 02:00: the weekly fuzz entries (0.44 GB) are saved on top of a quiet
// weekend.
func sunday(t *testing.T, p plan, limit int64) *cacheModel {
	m, before, _ := newCacheModel(t, modelOpts{quiet: true, scales: p.scales, limit: limit})
	friday := time.Date(2026, 11, 27, 20, 30, 0, 0, time.UTC)
	start := time.Date(2026, 11, 29, 20, 0, 0, 0, time.UTC)
	for _, e := range m.entries {
		if e.seed != "main" {
			e.accessed = friday
		}
		e.created = friday
	}
	m.now = start
	m.low, m.peak = 0, 0
	var events []modelEvent
	save := time.Date(2026, 11, 29, 20, 31, 0, 0, time.UTC)
	for _, c := range before {
		if strings.HasPrefix(c.Key, "go-fuzz-v2-") {
			key := strings.Replace(c.Key, "-2026-W47", "-2026-W48", 1)
			size := c.Size
			events = append(events, modelEvent{save, func() { m.add(key, "refs/heads/main", size, "", false) }})
		}
	}
	events = append(events, p.afterWriter(m, save)...)
	events = append(events, hourly(start, start.Add(6*time.Hour), 47, m.prune)...)
	m.play(events)
	return m
}

func TestCacheBudgetModel(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T, plan, int64) *cacheModel
	}{{"weekday", weekday}, {"monday rollover", monday}, {"sunday fuzz rollover", sunday}}

	// Before: the budget does not fit. The demand alone is over 10 GB; a weekday evicts live entries,
	// and the Monday rollover after a quiet weekend evicts the newest dev seed, with 10 GB or 10 GiB.
	// Peaks per scenario at 10 GB and at 10 GiB (they differ once an eviction has run).
	peaks := map[string][2]float64{
		"before/weekday": {10.44, 10.80}, "before/monday rollover": {11.46, 11.75}, "before/sunday fuzz rollover": {10.02, 10.48},
		"adopted/weekday": {9.74, 9.74}, "adopted/monday rollover": {10.34, 10.34}, "adopted/sunday fuzz rollover": {9.42, 9.42},
	}
	t.Run("before", func(t *testing.T) {
		for i, l := range limits {
			for _, s := range scenarios {
				m := s.run(t, planBefore, l.bytes)
				t.Log(m.report("before, " + s.name))
				m.requireDemand(t, 10.04)
				m.requirePeak(t, peaks["before/"+s.name][i])
				if l.bytes == 10_000_000_000 && s.name == "weekday" && len(m.evicted) == 0 {
					t.Fatal("the model no longer evicts on a weekday at 10 GB; update the budget section of docs/development/ci.md")
				}
				if s.name == "monday rollover" && len(m.seedLost) == 0 {
					t.Fatalf("the model no longer loses the dev seed at the Monday rollover at %s; update the budget section of docs/development/ci.md", l.name)
				}
			}
		}
	})

	// After: the newest main and dev seeds survive a weekday and the Monday rollover at either limit.
	// What else is evicted is reported and pinned in docs/development/ci.md.
	t.Run("adopted", func(t *testing.T) {
		for i, l := range limits {
			for _, s := range scenarios {
				m := s.run(t, planAdopted, l.bytes)
				t.Log(m.report("adopted, " + s.name))
				m.requireDemand(t, 8.97)
				m.requirePeak(t, peaks["adopted/"+s.name][i])
				// At 10 GiB nothing live is evicted at all; at 10 GB only the Monday instant, when the
				// old and new lint driver entries coexist for the two minutes before the prune, takes
				// the four least recently used idle entries (three pinned tools, go-mod-v2).
				if l.bytes != 10_000_000_000 && len(m.evicted) != 0 {
					t.Fatalf("%s evicts %v at %s", s.name, m.evicted, l.name)
				}
				if l.bytes == 10_000_000_000 && s.name != "monday rollover" && len(m.evicted) != 0 {
					t.Fatalf("%s evicts %v at %s", s.name, m.evicted, l.name)
				}
				if l.bytes == 10_000_000_000 && s.name == "monday rollover" && len(m.evicted) != 4 {
					t.Fatalf("monday rollover evicts %d live entries at 10 GB, documented 4: %v", len(m.evicted), m.evicted)
				}
				if len(m.seedLost) != 0 {
					t.Fatalf("%s at %s loses a newest seed: %v", s.name, l.name, m.seedLost)
				}
			}
		}
	})

	// Days that rotate keys. go.mod or go.sum changed on main on 7 of the last 30 days (committer
	// date, origin/main at 2026-11-25), one of them a Sunday. The rotated entries coexist with the old
	// generation until the prune, so these days peak above an ordinary weekday and, at 10.00 GB, take
	// idle entries (never a seed); at 10 GiB they take two. The Sunday case is the worst: the weekly
	// ci-main run saves about 3.3 GB at once.
	t.Run("go.mod and go.sum change days", func(t *testing.T) {
		days := []struct {
			name string
			run  func(*testing.T, plan, int64) *cacheModel
			peak [2]float64
			live [2]int
		}{
			{"rotation day", rotationDay, [2]float64{10.66, 10.95}, [2]int{7, 2}},
			{"sunday go.sum", sundayGoSum, [2]float64{12.27, 12.27}, [2]int{8, 7}},
		}
		for i, l := range limits {
			for _, d := range days {
				m := d.run(t, planAdopted, l.bytes)
				t.Log(m.report("adopted, " + d.name))
				m.requireDemand(t, 8.97)
				m.requirePeak(t, d.peak[i])
				if len(m.evicted) != d.live[i] {
					t.Fatalf("%s evicts %d live entries at %s, documented %d: %v", d.name, len(m.evicted), l.name, d.live[i], m.evicted)
				}
				if len(m.seedLost) != 0 {
					t.Fatalf("%s at %s loses a newest seed: %v", d.name, l.name, m.seedLost)
				}
			}
		}
		// The module cache is a casualty of the Monday instant at 10.00 GB; the daily race seed saves
		// it again at 03:41 on Tuesday, where before only the Sunday ci-main run did.
		m := monday(t, planAdopted, 10_000_000_000)
		if !slices.ContainsFunc(m.evicted, func(k string) bool { return strings.HasPrefix(k, "go-mod-v2-") }) {
			t.Fatalf("the Monday instant no longer evicts go-mod-v2; update the budget section of docs/development/ci.md: %v", m.evicted)
		}
	})

	// What each adopted measure does in the model.
	t.Run("each measure is load-bearing", func(t *testing.T) {
		const gb = int64(10_000_000_000)
		without := func(f func(*plan)) plan {
			p := planAdopted
			f(&p)
			return p
		}
		// The daily dev refresh keeps the dev seed alive across the weekend.
		if m := monday(t, without(func(p *plan) { p.devRefresh = false }), gb); len(m.seedLost) == 0 {
			t.Fatalf("without the dev refresh the Monday rollover no longer loses the dev seed: %s", m.report("monday"))
		}
		// The shrinks take the weekly peak from 11.46 to 10.34 GB and the demand from 10.04 to 8.98.
		if m := monday(t, without(func(p *plan) { p.scales = nil }), gb); float64(m.peak)/1e9 < 11.4 || len(m.evicted) <= 4 {
			t.Fatalf("without the shrinks the Monday peak should be 11.46 GB and evict more than four entries: %s", m.report("monday"))
		}
		// The prune after a writer changes nothing here: with eviction at the moment of the save the
		// old and new generation coexist at the peak whatever follows. It shortens the window in
		// which they coexist from up to an hour to two minutes, which only matters if GitHub's
		// eviction is lazy (docs/development/ci.md).
		for _, s := range scenarios {
			a, b := s.run(t, planAdopted, gb), s.run(t, without(func(p *plan) { p.writerPrune = false }), gb)
			if a.peak != b.peak || len(a.evicted) != len(b.evicted) {
				t.Fatalf("%s: the prune after a writer now changes the replay, update the budget section of docs/development/ci.md", s.name)
			}
		}
	})
}
