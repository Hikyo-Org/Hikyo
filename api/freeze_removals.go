package api

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/oasdiff/checker/schemawalk"
	"github.com/oasdiff/oasdiff/diff"
)

// freezeRemovals enforces the no-removal promise on the structural diff. The
// upstream checker deliberately omits some deprecated removals after x-sunset;
// neither an INFO threshold nor our change-ID allowlist can recover them.
// Keep lifecycle metadata out of this check: a sunset never permits removal.
func freezeRemovals(report *diff.Diff) []Violation {
	if report == nil {
		return nil
	}
	check := freezeRemovalCheck{seenPaths: make(map[*diff.PathsDiff]bool)}
	check.paths(report.PathsDiff)
	return check.violations
}

type freezeRemovalCheck struct {
	violations []Violation
	seenPaths  map[*diff.PathsDiff]bool
}

func (c *freezeRemovalCheck) add(id, operation, path, text string) {
	c.violations = append(c.violations, Violation{
		ID: id, Operation: operation, Path: path, Text: text, Level: "ERR",
	})
}

func (c *freezeRemovalCheck) paths(paths *diff.PathsDiff) {
	if paths == nil || c.seenPaths[paths] {
		return
	}
	c.seenPaths[paths] = true
	for _, path := range paths.Deleted {
		for operation := range paths.Base.Value(path).Operations() {
			c.add("hikyo-endpoint-removed", operation, path, "endpoint removed from the frozen API")
		}
	}
	for path, item := range paths.Modified {
		if item.OperationsDiff == nil {
			continue
		}
		for _, operation := range item.OperationsDiff.Deleted {
			c.add("hikyo-endpoint-removed", operation, path, "endpoint removed from the frozen API")
		}
		for operation, method := range item.OperationsDiff.Modified {
			c.parameters(method.ParametersDiff, operation, path)
			if method.RequestBodyDiff != nil {
				c.content(method.RequestBodyDiff.ContentDiff, operation, path, "request body")
			}
			if method.ResponsesDiff != nil {
				for status, response := range method.ResponsesDiff.Modified {
					c.content(response.ContentDiff, operation, path, "response "+status)
					if response.HeadersDiff != nil {
						for name, header := range response.HeadersDiff.Modified {
							context := "response " + status + " header " + name
							c.schema(header.SchemaDiff, operation, path, context)
							c.content(header.ContentDiff, operation, path, context)
						}
					}
				}
			}
			if method.CallbacksDiff != nil {
				for _, callback := range method.CallbacksDiff.Modified {
					c.paths(callback)
				}
			}
		}
	}
}

func (c *freezeRemovalCheck) parameters(parameters *diff.ParametersDiffByLocation, operation, path string) {
	if parameters == nil {
		return
	}
	for location, names := range parameters.Deleted {
		for _, name := range names {
			c.add("hikyo-parameter-removed", operation, path, fmt.Sprintf("%s parameter %q removed from the frozen API", location, name))
		}
	}
	for location, parameters := range parameters.Modified {
		for name, parameter := range parameters {
			context := fmt.Sprintf("%s parameter %q", location, name)
			c.schema(parameter.SchemaDiff, operation, path, context)
			c.content(parameter.ContentDiff, operation, path, context)
		}
	}
}

func (c *freezeRemovalCheck) content(content *diff.ContentDiff, operation, path, context string) {
	if content == nil {
		return
	}
	for mediaType, media := range content.MediaTypeModified {
		c.schema(media.SchemaDiff, operation, path, context+" "+mediaType)
	}
}

func (c *freezeRemovalCheck) schema(schema *diff.SchemaDiff, operation, path, context string) {
	// The upstream structural walker handles nested objects, arrays, composed
	// schemas and shared/recursive references without applying lifecycle rules.
	schemawalk.DeletedProperties(schema, func(parentPath, name string, _ *openapi3.Schema, _ *diff.SchemaDiff, _ bool) {
		c.add("hikyo-property-removed", operation, path,
			fmt.Sprintf("%s property %q removed from the frozen API", context, schemawalk.PropertyFullName(parentPath, name)))
	})
}
