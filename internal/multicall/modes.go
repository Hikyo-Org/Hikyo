// Package multicall owns the closed host-mode inventory shared by dispatch,
// usage coverage, and security classification. Client verbs have their own
// canonical inventory in internal/cli.
package multicall

import "slices"

const (
	RootKeyStage          = "__hikyo-stage-root-key"
	TLSStage              = "__hikyo-stage-tls"
	RolloutAuthorityStage = "__hikyo-stage-rollout-authority"
	ImportSubprocess      = "__hikyo-import-subprocess"
)

type Mode struct {
	Name string
	// Hidden modes and machine-print aliases have no public help section.
	Hidden bool
}

var modes = []Mode{
	{Name: "server"}, {Name: "migrate"}, {Name: "upgrade"},
	{Name: "operator"}, {Name: "config-rollout"}, {Name: "updater"},
	{Name: "version"}, {Name: "about"}, {Name: "welcome"}, {Name: "admin"},
	{Name: "backup"}, {Name: "escrow"}, {Name: "restore"},
	{Name: "--version", Hidden: true}, {Name: "--upgrade-bundle-formats", Hidden: true},
	{Name: RootKeyStage, Hidden: true}, {Name: TLSStage, Hidden: true},
	{Name: RolloutAuthorityStage, Hidden: true}, {Name: ImportSubprocess, Hidden: true},
}

func Modes() []Mode { return slices.Clone(modes) }

func Lookup(name string) (Mode, bool) {
	for _, mode := range modes {
		if mode.Name == name {
			return mode, true
		}
	}
	return Mode{}, false
}
