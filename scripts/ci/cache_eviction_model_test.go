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
//   - the limit is 10 GB, and an entry is evicted the moment the total passes it (the earliest
//     possible eviction, the worst case for the seeds);
//   - eviction takes the least recently accessed entry first, and a tie goes against the seeds;
//   - the feedback seeds (0.69 GB main, 0.76 GB dev) and the two pnpm stores (0.205 GB each, one
//     measured on macOS arm64 with zstd, the Playwright one assumed equal) are not in the listing;
//   - a weekday: every entry the listing shows touched within 90 minutes of the listing is touched
//     every hour; the others (CodeQL overlays, kind, buildx, the old feedback entries) are touched
//     only when created;
//   - a quiet weekend: nothing is touched after Friday 20:30 except what a schedule saves.

const cacheModelLimit = int64(10_000_000_000)

type modelEntry struct {
	apiCacheEntry
	created, accessed time.Time
	seed              string
	busy              bool
}

type cacheModel struct {
	t        *testing.T
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
	for m.total() > cacheModelLimit {
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

// newCacheModel loads the listing, applies the prune once and the by-hand deletion of the unfamilied
// seeds, and adds what the listing does not hold. quiet marks a weekend: nothing is busy.
func newCacheModel(t *testing.T, quiet bool) (*cacheModel, []apiCacheEntry, time.Time) {
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
	m := &cacheModel{t: t, program: program, now: listedAt, nextID: 1 << 20}
	for _, c := range listing.Caches {
		created, err := time.Parse(time.RFC3339Nano, c.Created)
		if err != nil {
			t.Fatal(err)
		}
		accessed, err := time.Parse(time.RFC3339Nano, c.Accessed)
		if err != nil {
			t.Fatal(err)
		}
		m.entries = append(m.entries, &modelEntry{apiCacheEntry: c, created: created, accessed: accessed, busy: !quiet && listedAt.Sub(accessed) <= 90*time.Minute})
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
	m.add("pnpm-store-v2-host-Linux-X64-ubuntu26-"+strings.Repeat("e", 64), "refs/heads/main", 205e6, "", !quiet)
	m.add("pnpm-store-v2-playwright-Linux-X64-ubuntu26-"+strings.Repeat("e", 64), "refs/heads/main", 205e6, "", !quiet)
	m.replaceSeed("main", seedSizesMain)
	m.replaceSeed("dev", seedSizesDev)
	for _, e := range m.entries {
		if e.seed != "" {
			e.busy = !quiet
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

func (m *cacheModel) report(name string) string {
	return fmt.Sprintf("%s: demand after the prune %.2f GB, start %.2f GB, low point %.2f GB, peak %.2f GB, evicted %d live entries %v, seeds lost %v",
		name, float64(m.demand)/1e9, float64(m.start)/1e9, float64(m.low)/1e9, float64(m.peak)/1e9, len(m.evicted), m.evicted, m.seedLost)
}

func TestCacheBudgetModel(t *testing.T) {
	// A weekday: the measured per-push CodeQL churn (the listing's overlay entries, one day later),
	// the main seed at 03:32, a dev push at 09:10, the prune every hour at :47.
	t.Run("weekday", func(t *testing.T) {
		m, before, listedAt := newCacheModel(t, false)
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
			events = append(events, modelEvent{created.Add(24 * time.Hour), func() { m.add(key, "refs/heads/main", size, "", false) }})
		}
		events = append(events,
			modelEvent{time.Date(2026, 11, 26, 3, 32, 0, 0, time.UTC), func() { m.replaceSeed("main", seedSizesMain) }},
			modelEvent{time.Date(2026, 11, 26, 9, 10, 0, 0, time.UTC), func() { m.replaceSeed("dev", seedSizesDev) }},
		)
		m.play(events)
		t.Log(m.report("weekday"))
		m.requireDemand(t, 10.04)
		// The documented verdict: the demand alone is over the limit, so even a weekday evicts live
		// entries (CodeQL overlays and the pinned tools first).
		if len(m.evicted) == 0 {
			t.Fatal("the model no longer evicts on a weekday; update the budget section of docs/development/ci.md")
		}
	})

	// Monday 04:11: the weekly lint driver entry (1.7 GB) is saved after a quiet weekend. The old
	// week's entry was last looked up at 04:10, fresher than every seed; the main seed was saved at
	// 03:32, the dev seed was last restored on Friday.
	t.Run("monday rollover", func(t *testing.T) {
		m, _, _ := newCacheModel(t, true)
		friday := time.Date(2026, 11, 27, 20, 30, 0, 0, time.UTC)
		monday := time.Date(2026, 11, 30, 4, 0, 0, 0, time.UTC)
		var old *modelEntry
		for _, e := range m.entries {
			if e.seed != "main" && e.created.Before(monday) {
				e.accessed = friday
			}
			if strings.HasPrefix(e.Key, "go-lintdriver-v2-") {
				old = e
			}
			if e.seed == "dev" {
				e.accessed = friday
			}
			if e.seed == "main" {
				e.created, e.accessed = time.Date(2026, 11, 30, 3, 32, 0, 0, time.UTC), time.Date(2026, 11, 30, 3, 32, 0, 0, time.UTC)
			}
		}
		old.accessed = time.Date(2026, 11, 30, 4, 10, 0, 0, time.UTC)
		size := old.Size
		m.now = monday
		m.low, m.peak = 0, 0
		events := []modelEvent{{time.Date(2026, 11, 30, 4, 11, 0, 0, time.UTC), func() {
			m.add("go-lintdriver-v2-Linux-X64-ubuntu26-"+strings.Repeat("a", 64)+"-2026-W49", "refs/heads/main", size, "", false)
		}}}
		events = append(events, hourly(monday, monday.Add(8*time.Hour), 47, m.prune)...)
		m.play(events)
		t.Log(m.report("monday rollover"))
		m.requireDemand(t, 10.04)
		if len(m.seedLost) == 0 {
			t.Fatal("the model no longer loses the dev seed at the Monday rollover; update the budget section of docs/development/ci.md")
		}
	})

	// Sunday 20:31: the weekly fuzz entries (0.44 GB) are saved on top of a quiet weekend.
	t.Run("sunday fuzz rollover", func(t *testing.T) {
		m, before, _ := newCacheModel(t, true)
		friday := time.Date(2026, 11, 27, 20, 30, 0, 0, time.UTC)
		sunday := time.Date(2026, 11, 29, 20, 0, 0, 0, time.UTC)
		for _, e := range m.entries {
			if e.seed != "main" {
				e.accessed = friday
			}
			e.created = friday
		}
		m.now = sunday
		m.low, m.peak = 0, 0
		var events []modelEvent
		for _, c := range before {
			if strings.HasPrefix(c.Key, "go-fuzz-v2-") {
				key := strings.Replace(c.Key, "-2026-W47", "-2026-W48", 1)
				size := c.Size
				events = append(events, modelEvent{time.Date(2026, 11, 29, 20, 31, 0, 0, time.UTC), func() { m.add(key, "refs/heads/main", size, "", false) }})
			}
		}
		events = append(events, hourly(sunday, sunday.Add(6*time.Hour), 47, m.prune)...)
		m.play(events)
		t.Log(m.report("sunday fuzz rollover"))
		m.requireDemand(t, 10.04)
	})
}
