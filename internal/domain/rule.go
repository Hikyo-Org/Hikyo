package domain

import (
	"fmt"
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

// Where is a rule's reach. Items are bound per project: an environment or key
// item belongs to exactly one of the rule's projects, and a project with no
// items on an `all` axis is unnarrowed on it.
//
// AllProjects is the all-projects project axis (#1446): every project of the
// rule's org, including projects created later. Projects is then empty, the
// environment axis is `all` with no items (owner decision 2026-11-25: until
// #1447 binds environments by name, an id-bound except would not follow later
// projects), and key items are folders only, held under AnyProject because a
// folder path means the same in every project.
type Where struct {
	AllProjects bool
	Projects    []ProjectID
	EnvMode     AxisMode
	Envs        map[ProjectID][]EnvID
	KeyMode     AxisMode
	Keys        map[ProjectID][]RuleKeyItem
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
	// ErrRuleAllProjectsEnvironments refuses environment items on an
	// all-projects rule until environments bind by name (#1447).
	ErrRuleAllProjectsEnvironments = fmt.Errorf("%w: a rule over all projects covers every environment; naming environments needs environment-name binding (#1447)", ErrInvalid)
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

// RuleShapeOf reports the shape a capability may take on a rule, and whether
// a rule may carry it at all.
func RuleShapeOf(c Capability) (RuleShape, bool) {
	s, ok := ruleShapes[c]
	return s, ok
}

// EnvNarrowed reports whether the Where narrows environments anywhere.
func (w Where) EnvNarrowed() bool {
	if w.EnvMode != AxisAll {
		return true
	}
	for _, items := range w.Envs {
		if len(items) > 0 {
			return true
		}
	}
	return false
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
	if w.EnvMode != AxisAll && w.EnvMode != AxisOnly {
		return fmt.Errorf("%w: environment mode %q", ErrInvalid, w.EnvMode)
	}
	if w.KeyMode != AxisAll && w.KeyMode != AxisOnly {
		return fmt.Errorf("%w: key mode %q", ErrInvalid, w.KeyMode)
	}
	if _, ok := w.Keys[AnyProject]; ok {
		return fmt.Errorf("%w: key items name no project", ErrInvalid)
	}
	envTotal := 0
	for p, items := range w.Envs {
		if !seen[p] {
			return fmt.Errorf("%w: environment items name project %q outside the rule", ErrInvalid, p)
		}
		for i, e := range items {
			if e == "" || slices.Contains(items[:i], e) {
				return fmt.Errorf("%w: environment %q is empty or named twice", ErrInvalid, e)
			}
		}
		envTotal += len(items)
	}
	if w.EnvMode == AxisOnly && envTotal == 0 {
		return fmt.Errorf("%w: an only-environments rule names at least one environment", ErrInvalid)
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
	if len(w.Projects)+envTotal+keyTotal > MaxRuleItems {
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
// project list, every environment, and folder items only, all held under
// AnyProject.
func (r Rule) validateAllProjects(shape RuleShape) error {
	w := r.Where
	if len(w.Projects) > 0 {
		return fmt.Errorf("%w: a rule over all projects names no project", ErrInvalid)
	}
	if w.EnvMode != AxisAll || w.EnvNarrowed() {
		return ErrRuleAllProjectsEnvironments
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
	if keyTotal > MaxRuleItems {
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
//   - A project atom needs every environment of the project (all, with no
//     exception in that project).
//   - An environment atom needs the environment on the axis.
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
	if !w.Names(s.Project) {
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
		if w.EnvMode != AxisOnly || len(w.Envs[w.Projects[0]]) == 0 {
			return MachineRuleDepth
		}
		return MachineRuleOK
	default:
		return MachineRuleDepth
	}
}
