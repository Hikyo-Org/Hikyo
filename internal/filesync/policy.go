package filesync

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// ResolvePolicy turns the destination settings into a publication policy,
// resolving owner and group names to ids. An unknown name is refused: a
// policy that cannot be applied exactly is never approximated.
func ResolvePolicy(cfg *Config) (Policy, error) {
	p := Policy{Mode: os.FileMode(cfg.FileMode()), UID: -1, GID: -1, OnRemoved: cfg.Destination.OnRemoved}
	if o := cfg.Destination.Owner; o != "" {
		id, err := resolveID(o, func(name string) (string, error) {
			u, err := user.Lookup(name)
			if err != nil {
				return "", err
			}
			return u.Uid, nil
		})
		if err != nil {
			return p, fmt.Errorf("destination.owner %q: %w", o, err)
		}
		p.UID = id
	}
	if g := cfg.Destination.Group; g != "" {
		id, err := resolveID(g, func(name string) (string, error) {
			grp, err := user.LookupGroup(name)
			if err != nil {
				return "", err
			}
			return grp.Gid, nil
		})
		if err != nil {
			return p, fmt.Errorf("destination.group %q: %w", g, err)
		}
		p.GID = id
	}
	return p, nil
}

func resolveID(s string, lookup func(string) (string, error)) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 {
			return 0, fmt.Errorf("must not be negative")
		}
		return n, nil
	}
	raw, err := lookup(s)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("resolves to non-numeric id %q", raw)
	}
	return n, nil
}
