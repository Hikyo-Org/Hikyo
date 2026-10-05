package domain

import (
	"fmt"
	"slices"
	"strings"
)

// WhereUnionContainsInProject checks the additive union, including selectors
// whose exceptions complement one another. The finite witnesses partition
// the symbolic selector space, rather than enumerating the current catalogue:
// an unnamed environment and unnamed keys in every folder region represent
// future objects too.
func WhereUnionContainsInProject(outers []Where, inner Where, project ProjectID, keyFolders map[string]string) bool {
	envs := slices.Clone(inner.Envs[project])
	folders := []string{""}
	ids := []string{}
	for _, where := range append(slices.Clone(outers), inner) {
		for _, env := range where.Envs[project] {
			if !slices.Contains(envs, env) {
				envs = append(envs, env)
			}
		}
		for _, item := range where.Keys[project] {
			if item.IsFolder {
				if !slices.Contains(folders, item.Folder) {
					folders = append(folders, item.Folder)
				}
			} else if !slices.Contains(ids, item.KeyID) {
				ids = append(ids, item.KeyID)
			}
		}
	}
	for i := 0; ; i++ {
		env := EnvID(fmt.Sprintf("selector-witness-%d", i))
		if !slices.Contains(envs, env) {
			envs = append(envs, env)
			break
		}
	}
	regions := slices.Clone(folders)
	for _, folder := range folders {
		for i := 0; ; i++ {
			child := fmt.Sprintf("selector-witness-%d", i)
			if folder != "" {
				child = folder + "/" + child
			}
			if !slices.ContainsFunc(folders, func(named string) bool { return folderWithin(named, child) }) {
				regions = append(regions, child)
				break
			}
		}
	}
	keys := make([]RuleKey, 0, len(regions)+len(ids))
	for _, folder := range regions {
		// Empty id cannot match a named stable key and represents all keys
		// not named individually by any selector.
		keys = append(keys, RuleKey{Folder: folder})
	}
	for _, id := range ids {
		folder, ok := keyFolders[id]
		if !ok {
			return false
		}
		keys = append(keys, RuleKey{ID: id, Folder: folder})
	}
	for _, env := range envs {
		if !selectorEnvMatches(inner, project, env) {
			continue
		}
		for _, key := range keys {
			if !selectorKeyMatches(inner, project, key) {
				continue
			}
			if !slices.ContainsFunc(outers, func(outer Where) bool {
				return slices.Contains(outer.Projects, project) && selectorEnvMatches(outer, project, env) && selectorKeyMatches(outer, project, key)
			}) {
				return false
			}
		}
	}
	return true
}

func selectorEnvMatches(where Where, project ProjectID, env EnvID) bool {
	matched := slices.Contains(where.Envs[project], env)
	return where.EnvMode == AxisOnly && matched || where.EnvMode == AxisAll && !matched
}

func selectorKeyMatches(where Where, project ProjectID, key RuleKey) bool {
	matched := slices.ContainsFunc(where.Keys[project], func(item RuleKeyItem) bool {
		if !item.IsFolder {
			return key.ID != "" && key.ID == item.KeyID
		}
		if where.KeyMode == AxisAll {
			return folderWithin(key.Folder, item.Folder)
		}
		return key.Folder == item.Folder
	})
	return where.KeyMode == AxisOnly && matched || where.KeyMode == AxisAll && !matched
}

// ContainsWhereInProject compares selectors symbolically, including future
// environments and keys. keyFolders must come from the current transaction;
// missing key metadata refuses containment instead of assuming a folder.
func (outer Where) ContainsWhereInProject(inner Where, project ProjectID, keyFolders map[string]string) bool {
	if !slices.Contains(outer.Projects, project) || !slices.Contains(inner.Projects, project) {
		return false
	}
	if !axisContains(outer.EnvMode, outer.Envs[project], inner.EnvMode, inner.Envs[project]) {
		return false
	}
	o, i := outer.Keys[project], inner.Keys[project]
	if inner.KeyMode == AxisOnly {
		for _, selected := range i {
			switch outer.KeyMode {
			case AxisOnly:
				if !slices.ContainsFunc(o, func(item RuleKeyItem) bool { return keySelectionContains(item, selected, keyFolders) }) {
					return false
				}
			case AxisAll:
				if slices.ContainsFunc(o, func(excluded RuleKeyItem) bool { return keySelectionOverlapsExclusion(selected, excluded, keyFolders) }) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if inner.KeyMode != AxisAll || outer.KeyMode != AxisAll {
		return false
	}
	// Every exclusion in the containing selector must also be excluded by
	// the contained selector. Folder exclusions include their descendants.
	for _, excluded := range o {
		if !slices.ContainsFunc(i, func(item RuleKeyItem) bool { return exclusionContains(item, excluded, keyFolders) }) {
			return false
		}
	}
	return true
}

func axisContains[T comparable](outerMode AxisMode, outer []T, innerMode AxisMode, inner []T) bool {
	if innerMode == AxisOnly {
		for _, value := range inner {
			if outerMode == AxisOnly && !slices.Contains(outer, value) || outerMode == AxisAll && slices.Contains(outer, value) {
				return false
			}
		}
		return outerMode == AxisOnly || outerMode == AxisAll
	}
	if innerMode != AxisAll || outerMode != AxisAll {
		return false
	}
	for _, excluded := range outer {
		if !slices.Contains(inner, excluded) {
			return false
		}
	}
	return true
}

func folderWithin(folder, ancestor string) bool {
	// The catalogue root is an exact folder selector, not a universal
	// subtree. This must match Rule.Reaches, where only nonempty folder
	// exceptions include descendants.
	return folder == ancestor || ancestor != "" && strings.HasPrefix(folder, ancestor+"/")
}

func keySelectionContains(outer, inner RuleKeyItem, folders map[string]string) bool {
	if inner.IsFolder {
		return outer.IsFolder && outer.Folder == inner.Folder
	}
	if !outer.IsFolder {
		return outer.KeyID == inner.KeyID
	}
	folder, ok := folders[inner.KeyID]
	return ok && folder == outer.Folder
}

func keySelectionOverlapsExclusion(selected, excluded RuleKeyItem, folders map[string]string) bool {
	if selected.IsFolder {
		if excluded.IsFolder {
			return folderWithin(selected.Folder, excluded.Folder)
		}
		folder, ok := folders[excluded.KeyID]
		return !ok || folder == selected.Folder
	}
	if !excluded.IsFolder {
		return selected.KeyID == excluded.KeyID
	}
	folder, ok := folders[selected.KeyID]
	return !ok || folderWithin(folder, excluded.Folder)
}

func exclusionContains(outer, inner RuleKeyItem, folders map[string]string) bool {
	if inner.IsFolder {
		return outer.IsFolder && folderWithin(inner.Folder, outer.Folder)
	}
	if !outer.IsFolder {
		return outer.KeyID == inner.KeyID
	}
	folder, ok := folders[inner.KeyID]
	return ok && folderWithin(folder, outer.Folder)
}
