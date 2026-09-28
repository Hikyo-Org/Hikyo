package domain

import (
	"fmt"
	"slices"
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
type Where struct {
	Projects []ProjectID
	EnvMode  AxisMode
	Envs     map[ProjectID][]EnvID
	KeyMode  AxisMode
	Keys     map[ProjectID][]RuleKeyItem
}

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
// `read` is ShapeEnv, the conservative reading of D5: See is never narrowed
// by keys, so a key-narrowed rule may not carry it at all rather than
// silently reaching the whole environment.
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
	switch shape {
	case ShapeEnv:
		if w.KeyNarrowed() {
			return fmt.Errorf("%w: %s cannot be narrowed by keys; it needs all keys of an environment", ErrInvalid, r.Capability)
		}
	case ShapeProject:
		if w.KeyNarrowed() || w.EnvNarrowed() {
			return fmt.Errorf("%w: %s needs a whole project (all environments, all keys)", ErrInvalid, r.Capability)
		}
	}
	return nil
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
//     reveal, export, publish, delivery, ...) is out of its reach.
//   - manage-members is inert on rules until delegation containment exists:
//     it never satisfies anything, so it can neither create grants nor rules.
func (r Rule) Reaches(c Capability, at Level, s Scope, key *RuleKey) bool {
	if c != r.Capability || c == CapManageMembers || s.Org != r.Org {
		return false
	}
	w := r.Where
	if !slices.Contains(w.Projects, s.Project) {
		return false
	}
	envs := w.Envs[s.Project]
	switch at {
	case LevelProject:
		if w.EnvMode != AxisAll || len(envs) > 0 {
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
	items := w.Keys[s.Project]
	if w.KeyMode == AxisAll && len(items) == 0 {
		return true
	}
	if key == nil {
		return false
	}
	matched := slices.ContainsFunc(items, func(it RuleKeyItem) bool {
		if it.IsFolder {
			return it.Folder == key.Folder
		}
		return key.ID != "" && it.KeyID == key.ID
	})
	if w.KeyMode == AxisOnly {
		return matched
	}
	return !matched
}
