package domain

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Member access rules (member-access-rules ADR). A rule is one capability for
// one human principal inside one org, with a Where of projects, environments
// and keys. Rules are purely additive like grants: an except narrows only its
// own rule, and there are no deny rules.
//
// Everything here is written so the worst failure is a narrowed rule wrongly
// DENIED: a rule that does not validate reaches nothing, an `only` axis with
// no items for a project reaches nothing in it, and an atom the rule shape
// cannot carry is never satisfied.

// AxisMode reads an axis's items: exceptions under AxisAll, the complete list
// under AxisOnly.
type AxisMode string

const (
	AxisAll  AxisMode = "all"
	AxisOnly AxisMode = "only"
)

// RuleKeyItem is one key-axis item: a folder path (exact match on a key's
// folder, "" being the catalogue root) or a single key by its stable id.
type RuleKeyItem struct {
	Folder   string
	KeyID    string
	IsFolder bool
}

// Where is a rule's reach. Key items are bound per project: a key item
// belongs to exactly one of the rule's projects, and a project with no key
// items on an `all` axis is unnarrowed on it.
//
// Environment items are NAMES, rule-wide (ADR D3, #1447): "all, except
// `prod`" over `payments, web` excepts every environment called `prod` in
// either project, including one created or renamed to `prod` later. EnvNames
// is the stored truth. Envs is derived from it and never stored: BindEnvs
// fills it with the environments carrying those names in each covered
// project, read in the evaluating transaction. A Where that was never bound
// reaches nothing (Reaches), so a read path that forgets to bind fails closed.
//
// AllProjects is the all-projects project axis (#1446): every project of the
// rule's org, including projects created later. Projects is then empty, and
// key items are folders only, held under AnyProject because a folder path
// means the same in every project. Environment names apply in every project.
type Where struct {
	AllProjects bool
	Projects    []ProjectID
	EnvMode     AxisMode
	EnvNames    []string
	Envs        map[ProjectID][]EnvID
	KeyMode     AxisMode
	Keys        map[ProjectID][]RuleKeyItem

	// bound is set by BindEnvs and BoundEnvs; refs are what BindEnvs bound against.
	bound bool
	refs  []EnvRef
	// within, set by BindEnvsWithin, is the one project refs cover.
	within ProjectID
}

// EnvRef is one environment as name binding sees it.
type EnvRef struct {
	Project ProjectID
	ID      EnvID
	Name    string
}

// BindEnvs resolves the environment names against refs, the current
// environments of (at least) every project the Where covers. It is the one
// binding function: the chokepoint's rule reads, the rename and create census
// (over the environments before and after the change) and the web model's
// mirror of it all bind through it.
func (w Where) BindEnvs(refs []EnvRef) Where {
	w.Envs = map[ProjectID][]EnvID{}
	w.refs = refs
	w.bound = true
	for _, e := range refs {
		if w.Names(e.Project) && slices.Contains(w.EnvNames, e.Name) && !slices.Contains(w.Envs[e.Project], e.ID) {
			w.Envs[e.Project] = append(w.Envs[e.Project], e.ID)
		}
	}
	return w
}

// BindEnvsWithin binds the names against refs, the environments of project
// alone, and confines the Where to project: it reaches nothing elsewhere
// (Reaches), since its Envs and refs know no other project. The chokepoint
// binds a single-project check this way, reading one project's environments
// rather than the org's.
func (w Where) BindEnvsWithin(project ProjectID, refs []EnvRef) Where {
	w = w.BindEnvs(refs)
	w.within = project
	return w
}

// BoundEnvs marks an in-memory selector built directly over environment ids
// (a probe, or a grant's environment) as bound: its Envs are already the
// environments it reaches.
func (w Where) BoundEnvs() Where {
	w.bound = true
	return w
}

// WithEnvNames is w with other environment names, bound against the same
// environments as w.
func (w Where) WithEnvNames(names []string) Where {
	w.EnvNames = names
	if w.refs == nil {
		return w
	}
	return w.BindEnvs(w.refs)
}

// EnvName is the name of environment id of project among the environments w
// was bound against.
func (w Where) EnvName(project ProjectID, id EnvID) (string, bool) {
	return EnvRefName(w.refs, project, id)
}

// EnvRefName is the name of environment id of project among refs.
func EnvRefName(refs []EnvRef, project ProjectID, id EnvID) (string, bool) {
	for _, e := range refs {
		if e.Project == project && e.ID == id {
			return e.Name, true
		}
	}
	return "", false
}

// AnyProject keys the folder items of an all-projects Where.
const AnyProject ProjectID = ""

// Names reports whether the Where's project axis covers project p.
func (w Where) Names(p ProjectID) bool {
	if w.AllProjects {
		return p != ""
	}
	return slices.Contains(w.Projects, p)
}

// KeysIn is the key-axis items that apply in project p.
func (w Where) KeysIn(p ProjectID) []RuleKeyItem {
	if w.AllProjects {
		return w.Keys[AnyProject]
	}
	return w.Keys[p]
}

var (
	// ErrRuleAllProjectsKeys refuses single-key items on an all-projects rule:
	// a key id belongs to one project (owner decision 2026-11-20).
	ErrRuleAllProjectsKeys = fmt.Errorf("%w: a rule over all projects narrows keys by folder only; a single key belongs to one project", ErrInvalid)
)

// Rule is one stored rule row with its selector.
type Rule struct {
	ID         string
	Principal  PrincipalID
	Capability Capability
	Org        OrgID
	Where      Where
}

// RuleKey is the key a key-aware authorization addresses, resolved from the
// database inside the authorizing transaction. ID is empty for a key being
// created, which only folder items can match.
type RuleKey struct {
	ID     string
	Folder string
}

// MaxRuleItems bounds one rule's selector items (projects, environments and
// keys together), below the rule audit events' list bounds so an oversized
// rule is refused by name rather than failing at its audit write.
const MaxRuleItems = 500

// AllProjectsExpansion bounds the selector size of an all-projects rule as a
// v1 response lists it: every one of projects, each with the rule's folder
// items repeated under it and at most one environment item per name (names are
// unique within a project). The names themselves are a revision-22 field with
// their own bound. It stays within
// MaxRuleItems so the listing fits the v1 bounds and remains a valid create
// (#1446, #1447): a rule create or a project create that would pass the bound
// is refused, never truncated. It counts every name in every project, so no
// environment create or rename can push a listing past it.
func AllProjectsExpansion(projects, folders, names int) int {
	return projects * (1 + folders + names)
}

// ListedItems bounds the selector size of a named-projects rule as a v1
// response lists it, the same way: its projects, at most one environment item
// per name in each project, and its key items. Migration 00102 splits any
// converted rule above it, so every listed rule can be saved again. An
// all-projects rule is bounded by AllProjectsExpansion instead.
func (w Where) ListedItems() int {
	keys := 0
	for _, items := range w.Keys {
		keys += len(items)
	}
	return len(w.Projects)*(1+len(w.EnvNames)) + keys
}

// RuleItemCount is an all-projects rule id with its stored folder and
// environment-name item counts.
type RuleItemCount struct {
	ID      string
	Folders int
	Names   int
}

// RuleShape is the narrowest Where a capability may sit on (ADR D2, D5).
type RuleShape int

const (
	// ShapeKey may be narrowed by environment and by key.
	ShapeKey RuleShape = iota
	// ShapeEnv may be narrowed by environment, never by key.
	ShapeEnv
	// ShapeProject needs every environment and every key of its projects.
	ShapeProject
)

// ruleShapes is the closed set of capabilities a rule may carry. Anything
// absent (manage-projects, every instance atom, and atoms other amendments
// add until they get a row in the ADR's table) is refused.
//
// See may share a key selector with other permissions, but ignores that axis
// when evaluated (ADR D5). Pin requires every key of its environment.
var ruleShapes = map[Capability]RuleShape{
	CapRead:             ShapeEnv,
	CapEdit:             ShapeKey,
	CapPublish:          ShapeKey,
	CapPin:              ShapeEnv,
	CapReveal:           ShapeKey,
	CapRevealHistory:    ShapeKey,
	CapDefinitionsEdit:  ShapeKey,
	CapManageMembers:    ShapeKey,
	CapManageIdentities: ShapeProject,
	CapManageAdapters:   ShapeProject,
	CapProjectSettings:  ShapeProject,
}

// RuleCapabilities is every capability a rule may carry, in a stable order.
func RuleCapabilities() []Capability {
	return slices.Sorted(maps.Keys(ruleShapes))
}

// RuleShapeOf reports the shape a capability may take on a rule, and whether
// a rule may carry it at all.
func RuleShapeOf(c Capability) (RuleShape, bool) {
	s, ok := ruleShapes[c]
	return s, ok
}

// EnvNarrowed reports whether the Where narrows environments anywhere.
func (w Where) EnvNarrowed() bool {
	return w.EnvMode != AxisAll || len(w.EnvNames) > 0
}

// validateEnvNames checks the environment axis: a known mode, names that are
// non-empty and distinct, and at least one name under `only`.
func (w Where) validateEnvNames() error {
	if w.EnvMode != AxisAll && w.EnvMode != AxisOnly {
		return fmt.Errorf("%w: environment mode %q", ErrInvalid, w.EnvMode)
	}
	for i, name := range w.EnvNames {
		if name == "" || slices.Contains(w.EnvNames[:i], name) {
			return fmt.Errorf("%w: environment %q is empty or named twice", ErrInvalid, name)
		}
	}
	if w.EnvMode == AxisOnly && len(w.EnvNames) == 0 {
		return fmt.Errorf("%w: an only-environments rule names at least one environment", ErrInvalid)
	}
	return nil
}

// KeyNarrowed reports whether the Where narrows keys anywhere.
func (w Where) KeyNarrowed() bool {
	if w.KeyMode != AxisAll {
		return true
	}
	for _, items := range w.Keys {
		if len(items) > 0 {
			return true
		}
	}
	return false
}

// KeyNarrowedIn reports whether the Where narrows keys in one project.
func (w Where) KeyNarrowedIn(p ProjectID) bool {
	return w.KeyMode != AxisAll || len(w.KeysIn(p)) > 0
}

// Validate is the shape authority for a rule, applied on every write AND on
// every read: a stored row that does not validate reaches nothing.
func (r Rule) Validate() error {
	if r.Org == "" {
		return fmt.Errorf("%w: a rule lives in exactly one organization", ErrInvalid)
	}
	shape, ok := ruleShapes[r.Capability]
	if !ok {
		return fmt.Errorf("%w: capability %q cannot be held on a rule", ErrInvalid, r.Capability)
	}
	w := r.Where
	if w.AllProjects {
		return r.validateAllProjects(shape)
	}
	if len(w.Projects) == 0 {
		return fmt.Errorf("%w: a rule names at least one project", ErrInvalid)
	}
	seen := map[ProjectID]bool{}
	for _, p := range w.Projects {
		if p == "" || seen[p] {
			return fmt.Errorf("%w: project %q is empty or named twice", ErrInvalid, p)
		}
		seen[p] = true
	}
	if err := w.validateEnvNames(); err != nil {
		return err
	}
	if w.KeyMode != AxisAll && w.KeyMode != AxisOnly {
		return fmt.Errorf("%w: key mode %q", ErrInvalid, w.KeyMode)
	}
	if _, ok := w.Keys[AnyProject]; ok {
		return fmt.Errorf("%w: key items name no project", ErrInvalid)
	}
	keyTotal := 0
	for p, items := range w.Keys {
		if !seen[p] {
			return fmt.Errorf("%w: key items name project %q outside the rule", ErrInvalid, p)
		}
		for i, k := range items {
			if !k.IsFolder && k.KeyID == "" || k.IsFolder && k.KeyID != "" || slices.Contains(items[:i], k) {
				return fmt.Errorf("%w: a key item is malformed or named twice", ErrInvalid)
			}
		}
		keyTotal += len(items)
	}
	if w.KeyMode == AxisOnly && keyTotal == 0 {
		return fmt.Errorf("%w: an only-keys rule names at least one folder or key", ErrInvalid)
	}
	if len(w.Projects)+len(w.EnvNames)+keyTotal > MaxRuleItems {
		return fmt.Errorf("%w: a rule names at most %d projects, environments and keys", ErrLimitExceeded, MaxRuleItems)
	}
	return r.validateShape(shape)
}

func (r Rule) validateShape(shape RuleShape) error {
	w := r.Where
	switch shape {
	case ShapeEnv:
		if w.KeyNarrowed() && r.Capability != CapRead {
			return fmt.Errorf("%w: %s cannot be narrowed by keys; it needs all keys of an environment", ErrInvalid, r.Capability)
		}
	case ShapeProject:
		if w.KeyNarrowed() || w.EnvNarrowed() {
			return fmt.Errorf("%w: %s needs a whole project (all environments, all keys)", ErrInvalid, r.Capability)
		}
	}
	return nil
}

// validateAllProjects is Validate for the all-projects project axis: no
// project list, environment names (which apply in every project), and folder
// items only, all held under AnyProject.
func (r Rule) validateAllProjects(shape RuleShape) error {
	w := r.Where
	if len(w.Projects) > 0 {
		return fmt.Errorf("%w: a rule over all projects names no project", ErrInvalid)
	}
	if err := w.validateEnvNames(); err != nil {
		return err
	}
	if w.KeyMode != AxisAll && w.KeyMode != AxisOnly {
		return fmt.Errorf("%w: key mode %q", ErrInvalid, w.KeyMode)
	}
	keyTotal := 0
	for p, items := range w.Keys {
		for _, k := range items {
			if !k.IsFolder {
				return ErrRuleAllProjectsKeys
			}
		}
		if p != AnyProject {
			return fmt.Errorf("%w: a rule over all projects names folders in every project, not in one", ErrInvalid)
		}
		for i, k := range items {
			if k.KeyID != "" || slices.Contains(items[:i], k) {
				return fmt.Errorf("%w: a key item is malformed or named twice", ErrInvalid)
			}
		}
		keyTotal += len(items)
	}
	if w.KeyMode == AxisOnly && keyTotal == 0 {
		return fmt.Errorf("%w: an only-keys rule names at least one folder or key", ErrInvalid)
	}
	if keyTotal+len(w.EnvNames) > MaxRuleItems {
		return fmt.Errorf("%w: a rule names at most %d projects, environments and keys", ErrLimitExceeded, MaxRuleItems)
	}
	return r.validateShape(shape)
}

// Reaches reports whether the rule satisfies one formula atom (capability c
// evaluated at level `at`) against the resolved chain s, optionally for one
// key. The caller has already validated the rule.
//
//   - Org and instance atoms are never satisfied: rules cover listed projects
//     only.
//   - A Where narrowed by environment names must have been bound
//     (Where.BindEnvs); unbound, it reaches nothing. A Where bound within
//     one project (Where.BindEnvsWithin) reaches nothing in another.
//   - A project atom needs every environment of the project (all, with no
//     environment of the project carrying an excepted name).
//   - An environment atom needs the environment on the axis, by its name.
//   - A key-narrowed rule satisfies an atom ONLY when the operation passed a
//     key and that key matches; every operation that passes no key (bulk
//     reveal, export, delivery, ...) is out of its reach. Publish proves its
//     complete changed-key selection before materializing a snapshot.
//   - manage-members never satisfies an ordinary atom: rule creation and
//     revocation use selector containment at the chokepoint, so narrowed
//     management cannot escape through a legacy scope-wide grant operation.
func (r Rule) Reaches(c Capability, at Level, s Scope, key *RuleKey) bool {
	if c != r.Capability || c == CapManageMembers || s.Org != r.Org {
		return false
	}
	w := r.Where
	if !w.Names(s.Project) || w.EnvNarrowed() && !w.bound || w.within != "" && s.Project != w.within {
		return false
	}
	envs := w.Envs[s.Project]
	switch at {
	case LevelProject:
		if c == CapDefinitionsEdit && key != nil {
			// The catalogue is shared by every environment. A key-aware
			// catalogue mutation may use an environment-scoped Define rule;
			// existing shared semantic changes independently require authority
			// in every environment. New empty keys need no value writes.
			// Bulk definitions stay project-wide.
			if w.EnvMode == AxisOnly && len(envs) == 0 {
				return false
			}
		} else if w.EnvMode != AxisAll || len(envs) > 0 {
			return false
		}
	case LevelEnv:
		if s.Env == "" {
			return false
		}
		listed := slices.Contains(envs, s.Env)
		if w.EnvMode == AxisOnly && !listed || w.EnvMode == AxisAll && listed {
			return false
		}
	default:
		return false
	}
	// See always covers the environment catalogue, even in a mixed key-scoped card.
	if c == CapRead {
		return true
	}
	items := w.KeysIn(s.Project)
	if w.KeyMode == AxisAll && len(items) == 0 {
		return true
	}
	if key == nil {
		return false
	}
	// Folder paths are hierarchical. An except of a folder also excepts its
	// subfolders, and an only-pick of a folder admits that folder exactly:
	// both readings err towards denial. The root folder "" has no subfolders
	// in this sense, so excepting it excepts only root keys.
	matched := slices.ContainsFunc(items, func(it RuleKeyItem) bool {
		if !it.IsFolder {
			return key.ID != "" && it.KeyID == key.ID
		}
		if it.Folder == key.Folder {
			return true
		}
		return w.KeyMode == AxisAll && it.Folder != "" && strings.HasPrefix(key.Folder, it.Folder+"/")
	})
	if w.KeyMode == AxisOnly {
		return matched
	}
	return !matched
}

// MachineRuleFault names why a machine principal may not hold a rule.
type MachineRuleFault int

const (
	// MachineRuleOK admits the rule.
	MachineRuleOK MachineRuleFault = iota
	// MachineRuleCapability: the capability is not on the class's allowlist.
	MachineRuleCapability
	// MachineRuleKeys: the rule names keys or folders. The machine widening
	// gate counts reach per environment, so a key-narrowed machine rule
	// would be undercounted there; machine rules never narrow keys.
	MachineRuleKeys
	// MachineRuleDepth: the rule reaches shallower than the class's depth.
	MachineRuleDepth
)

// CheckMachineRule is the machine half of rule validation, applied on every
// write AND on every read like Validate. Only workload and automation
// credentials hold rules, with the same allowlists and depth bound as their
// grants. `reveal` and `reveal-history` stay on legacy grants: their machine
// admission depends on a live opt-in and pin that a rule does not carry.
//
// A machine rule names exactly one project, its owning project. A rule that
// names every project (present or future) is therefore never a machine's.
func CheckMachineRule(c PrincipalClass, r Rule) MachineRuleFault {
	if !IsServiceAccountKind(c) || !MachineMayHold(c, r.Capability) {
		return MachineRuleCapability
	}
	w := r.Where
	if w.KeyNarrowed() {
		return MachineRuleKeys
	}
	depth, ok := MachineScopeDepth(c)
	if !ok || w.AllProjects || len(w.Projects) != 1 {
		return MachineRuleDepth
	}
	switch depth {
	case LevelProject:
		return MachineRuleOK
	case LevelEnv:
		// Explicit (project, environment) only: "all, except" includes
		// environments created later, which is the opposite of explicit.
		if w.EnvMode != AxisOnly || len(w.EnvNames) == 0 {
			return MachineRuleDepth
		}
		return MachineRuleOK
	default:
		return MachineRuleDepth
	}
}
