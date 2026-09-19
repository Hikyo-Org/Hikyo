package upgrade

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

// PreviewConfigurationMigrations projects release-defined additive migrations
// for backup preparation. It grants no writes and leaves the retained source
// snapshot untouched. The candidate repeats this plan under its migration fence.
func PreviewConfigurationMigrations(source *CandidateConfiguration, values map[string]string) (*CandidateConfiguration, map[string]string, error) {
	if source == nil {
		return nil, values, nil
	}
	projection := *source
	projection.Catalogue = slices.Clone(source.Catalogue)
	next := maps.Clone(values)
	if next == nil {
		next = make(map[string]string)
	}
	_, err := planConfigurationMigrations(&projection, next, 0)
	if err != nil {
		clear(next)
		return nil, nil, err
	}
	return &projection, next, nil
}

func planConfigurationMigrations(projection *CandidateConfiguration, values map[string]string, version int64) ([]runtimeconfig.Key, error) {
	return planConfigurationMigrationsWith(projection, values, version, runtimeconfig.Migrations())
}

func planConfigurationMigrationsWith(projection *CandidateConfiguration, values map[string]string, version int64, migrations []runtimeconfig.Migration) ([]runtimeconfig.Key, error) {
	if projection.SchemaVersion != runtimeconfig.SchemaVersion || version < 0 || version > runtimeconfig.MigrationVersion {
		return nil, fmt.Errorf("candidate configuration schema is incompatible")
	}
	additions := make(map[string]runtimeconfig.Migration)
	for _, migration := range migrations {
		if migration.Version > version {
			additions[migration.Name] = migration
		}
	}
	expected := make(map[string]runtimeconfig.Key)
	for _, key := range runtimeconfig.Catalogue() {
		expected[key.Name] = key
	}
	stored := make(map[string]bool)
	for _, key := range projection.Catalogue {
		target, ok := expected[key.Name]
		if !ok || stored[key.Name] {
			return nil, fmt.Errorf("candidate configuration catalogue has changed: %s", key.Name)
		}
		compiled, err := schema.CompileClassified(target.Classification, target.Declaration)
		if err != nil {
			return nil, err
		}
		canonical, err := compiled.Canonical()
		if err != nil {
			return nil, err
		}
		if key.Classification != string(target.Classification) || key.Declaration != string(canonical) || key.RequiredMode != string(schema.PresenceNone) || key.ForbiddenMode != string(schema.PresenceNone) || key.GroupID != "" || key.FolderPath != "" {
			return nil, fmt.Errorf("candidate configuration declaration has changed: %s", key.Name)
		}
		stored[key.Name] = true
	}
	var added []runtimeconfig.Key
	for _, target := range runtimeconfig.Catalogue() {
		migration, migratable := additions[target.Name]
		if !stored[target.Name] && !migratable {
			return nil, fmt.Errorf("candidate configuration catalogue has changed: %s has no release-defined migration; operator input required", target.Name)
		}
		compiled, err := schema.CompileClassified(target.Classification, target.Declaration)
		if err != nil {
			return nil, err
		}
		value, present := values[target.Name]
		if !present && migratable {
			if migration.Default == nil || !compiled.Validate(*migration.Default).Valid {
				return nil, fmt.Errorf("candidate configuration setting %s requires operator input: no valid release-defined default", target.Name)
			}
			value, present = *migration.Default, true
			values[target.Name] = value
		}
		if present && !compiled.Validate(value).Valid {
			return nil, fmt.Errorf("candidate configuration setting %s has an invalid stored value", target.Name)
		}
		if !stored[target.Name] {
			canonical, err := compiled.Canonical()
			if err != nil {
				return nil, err
			}
			projection.Catalogue = append(projection.Catalogue, CandidateConfigurationKey{Name: target.Name, Classification: string(target.Classification), Declaration: string(canonical), RequiredMode: string(schema.PresenceNone), ForbiddenMode: string(schema.PresenceNone)})
			added = append(added, target)
		}
	}
	for name := range values {
		if _, ok := expected[name]; !ok {
			return nil, fmt.Errorf("candidate configuration contains unexpected setting %s", name)
		}
	}
	return added, nil
}
