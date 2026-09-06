package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

var errNextRootEnrollment = errors.New("HIKYO_NEW_ROOT_KEY_FILE requires an enrolled root source: for initial setup, unset it until enrollment is installed; enroll and mount the candidate in rollout.rootSources, then set the file to /run/hikyo/rollout/sources/root/<alias>/root-key before starting the server")

// This app-local seam grants only reads of installation-enrolled root aliases.
// It neither signs deployments nor creates wrappers. The primary source remains
// independent and is the source the current process actually booted with.
type nextRootSources interface {
	nextRootAlias(string) (string, error)
	rootSource(string) ([]byte, error)
}

type selectedRootKeySource struct {
	primary rootKeySource
	sources nextRootSources
	alias   string
}

func (s selectedRootKeySource) Current(ctx context.Context) ([]byte, error) {
	return s.primary.Current(ctx)
}
func (s selectedRootKeySource) Next(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.sources.rootSource(s.alias)
}

type enrolledRootSources struct {
	enrollment configrollout.Enrollment
	directory  string
}

func loadEnrolledRootSources(cfg *config.Config, directory string) (enrolledRootSources, error) {
	raw, err := readDeploymentFile(cfg.ConfigRolloutEnrollment, false)
	if err != nil {
		return enrolledRootSources{}, errNextRootEnrollment
	}
	enrollment, err := configrollout.ParseEnrollment(raw)
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	if err != nil || cfg.HA && len(enrollment.Target.TopologyNodeIDs) == 0 || (enrollment.Target.StableNodeID != nodeID && !slices.Contains(enrollment.Target.TopologyNodeIDs, nodeID)) {
		return enrolledRootSources{}, errNextRootEnrollment
	}
	return enrolledRootSources{enrollment: enrollment, directory: directory}, nil
}

func (d *bootstrapDeployment) nextRootAlias(path string) (string, error) {
	return (enrolledRootSources{enrollment: d.enrollment, directory: d.sourcesDirectory}).nextRootAlias(path)
}
func (d enrolledRootSources) nextRootAlias(path string) (string, error) {
	// Matching is lexical and exact. Never follow an arbitrary operator path to
	// discover content equivalence or read a file outside the enrolled projection.
	for alias := range d.enrollment.Target.RootSources {
		if config.ValidManagedNodeID(alias) && path == filepath.Join(d.directory, "root", alias, "root-key") {
			root, err := d.rootSource(alias)
			crypto.Zero(root)
			if err != nil {
				return "", errNextRootEnrollment
			}
			return alias, nil
		}
	}
	return "", errNextRootEnrollment
}

func (d enrolledRootSources) rootSource(alias string) ([]byte, error) {
	if _, ok := d.enrollment.Target.RootSources[alias]; !ok || !config.ValidManagedNodeID(alias) {
		return nil, configrollout.ErrUnsupported
	}
	raw, err := readDeploymentFile(filepath.Join(d.directory, "root", alias, "root-key"), true)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	defer crypto.Zero(raw)
	root, err := crypto.ReadRootKey("", string(raw))
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	return root, nil
}

func seedNodeWithNextRoot(cfg *config.Config, sources nextRootSources) (map[string]string, error) {
	node, err := cfg.SeedNodeValues()
	if err != nil || cfg.NewRootKeyFile == "" {
		return node, err
	}
	if sources == nil {
		return nil, errNextRootEnrollment
	}
	alias, err := sources.nextRootAlias(cfg.NewRootKeyFile)
	if err != nil {
		return nil, err
	}
	node[config.ManagedNewRootSourceKey] = alias
	return node, nil
}

func (o *ownerRuntime) rotationRootSource(ctx context.Context, cfg *config.Config) (service.RootKeySource, error) {
	primary := rootKeySource{cfg: cfg, log: o.server.log}
	if cfg.NewRootSource == "" {
		return primary, nil
	}
	sources, ok := o.selfConfig.Deployment.(nextRootSources)
	if !ok {
		return nil, errors.New("HIKYO_NEW_ROOT_SOURCE: enroll and mount a root source before applying its alias")
	}
	source := selectedRootKeySource{primary: primary, sources: sources, alias: cfg.NewRootSource}
	root, err := source.Next(ctx)
	crypto.Zero(root)
	if err != nil {
		return nil, errors.New("HIKYO_NEW_ROOT_SOURCE: enrolled root source is unavailable or invalid")
	}
	return source, nil
}
