package service

import (
	"context"
	"fmt"
	"path"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// AdapterKeySelection is the configuration-time convenience for choosing a
// target's key subset (#157). It resolves against the project catalogue when
// the target is saved; the resolved ids become the explicit membership and
// the selection itself is never stored. A key created later that would have
// matched is not added: the deployment-adapter ADR binds membership to
// immutable ids and refuses any living "all"-shaped set.
type AdapterKeySelection struct {
	// Names are exact key names; an unknown name is refused by name.
	Names []string
	// Include and Exclude are bounded glob patterns over key names
	// (`*`, `?`, `[...]` as path.Match understands them).
	Include []string
	Exclude []string
	// Classification keeps only keys of that classification when set.
	Classification string
}

func (s AdapterKeySelection) empty() bool {
	return len(s.Names) == 0 && len(s.Include) == 0 && len(s.Exclude) == 0 && s.Classification == ""
}

// resolveKeySelection folds explicit ids, exact names, and pattern selection
// into one sorted id set. Exclude and classification narrow only what the
// patterns selected; explicit ids and names are the operator's own list.
func resolveKeySelection(catalogue []store.CatalogueKey, explicit []string, selection AdapterKeySelection) ([]string, error) {
	byName := make(map[string]store.CatalogueKey, len(catalogue))
	for _, key := range catalogue {
		byName[key.Name] = key
	}
	for _, pattern := range slices.Concat(selection.Include, selection.Exclude) {
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("%w: key pattern %q is not valid", domain.ErrInvalid, pattern)
		}
	}
	switch selection.Classification {
	case "", "secret", "config":
	default:
		return nil, fmt.Errorf("%w: key classification must be secret or config", domain.ErrInvalid)
	}
	chosen := make(map[string]struct{}, len(explicit)+len(selection.Names))
	for _, id := range explicit {
		chosen[id] = struct{}{}
	}
	for _, name := range selection.Names {
		key, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w: key %q does not exist in this project", domain.ErrInvalid, name)
		}
		chosen[key.ID] = struct{}{}
	}
	if len(selection.Include) != 0 || selection.Classification != "" {
		for _, key := range catalogue {
			if selection.Classification != "" && key.Classification != selection.Classification {
				continue
			}
			if len(selection.Include) != 0 && !matchesAny(selection.Include, key.Name) {
				continue
			}
			if matchesAny(selection.Exclude, key.Name) {
				continue
			}
			chosen[key.ID] = struct{}{}
		}
	}
	out := make([]string, 0, len(chosen))
	for id := range chosen {
		out = append(out, id)
	}
	slices.Sort(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: key selection resolved to no keys; a target needs an explicit non-empty subset", domain.ErrInvalid)
	}
	return out, nil
}

func matchesAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// resolveTargetKeys applies the input's selection under the configure
// operation and rewrites KeyIDs to the resolved explicit set. It runs before
// any ceremony so a refusal never spends a reauthentication window.
func (s *Adapters) resolveTargetKeys(ctx context.Context, actor Actor, scope domain.Scope, input *AdapterTargetInput) error {
	if input.KeySelection == nil || input.KeySelection.empty() {
		return nil
	}
	var catalogue []store.CatalogueKey
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpAdapterConfigure, scope)
		if err != nil {
			return err
		}
		catalogue, err = r.Catalogue().List(ctx, p)
		return err
	})
	if err != nil {
		return err
	}
	resolved, err := resolveKeySelection(catalogue, input.KeyIDs, *input.KeySelection)
	if err != nil {
		return err
	}
	input.KeyIDs = resolved
	input.KeySelection = nil
	return nil
}

// attachTargetKeys fills the resolved membership by name on every target the
// response echoes, so an operator sees what a save actually selected.
func attachTargetKeys(ctx context.Context, adapters store.AdapterReader, p authz.Proof, targets []store.AdapterTarget) error {
	for i := range targets {
		keys, err := adapters.TargetKeys(ctx, p, targets[i].ID)
		if err != nil {
			return err
		}
		targets[i].Keys = keys
	}
	return nil
}
