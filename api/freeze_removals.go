package api

import (
	"fmt"
	"reflect"
	"slices"

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
	var check freezeRemovalCheck
	check.paths(report.PathsDiff)
	return check.violations
}

type freezeRemovalCheck struct {
	violations []Violation
}

func (c *freezeRemovalCheck) add(id, operation, path, text string) {
	c.violations = append(c.violations, Violation{
		ID: id, Operation: operation, Path: path, Text: text, Level: "ERR",
	})
}

func (c *freezeRemovalCheck) paths(paths *diff.PathsDiff) {
	if paths == nil {
		return
	}
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
				// Upstream checks omit wildcard and default responses.
				for _, status := range method.ResponsesDiff.Deleted {
					c.add("hikyo-response-removed", operation, path, fmt.Sprintf("response %s removed from the frozen API", status))
				}
				for status, response := range method.ResponsesDiff.Modified {
					c.content(response.ContentDiff, operation, path, "response "+status)
					if response.LinksDiff != nil {
						for _, name := range response.LinksDiff.Deleted {
							c.add("hikyo-link-removed", operation, path, fmt.Sprintf("response %s link %q removed from the frozen API", status, name))
						}
					}
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
				for _, name := range method.CallbacksDiff.Deleted {
					c.add("hikyo-callback-removed", operation, path, fmt.Sprintf("callback %q removed from the frozen API", name))
				}
				// Upstream checks do not descend into callbacks, so nothing can
				// prove a change inside one is additive: refuse them all.
				for name := range method.CallbacksDiff.Modified {
					c.add("hikyo-callback-changed", operation, path, fmt.Sprintf("callback %q changed in the frozen API", name))
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
		if media.EncodingsDiff != nil {
			for _, name := range media.EncodingsDiff.Deleted {
				c.add("hikyo-encoding-removed", operation, path, fmt.Sprintf("%s %s encoding %q removed from the frozen API", context, mediaType, name))
			}
		}
	}
}

func (c *freezeRemovalCheck) schema(schema *diff.SchemaDiff, operation, path, context string) {
	if schema == nil {
		return
	}
	// The upstream structural walker handles nested objects, arrays, composed
	// schemas and shared/recursive references without applying lifecycle rules.
	schemawalk.DeletedProperties(schema, func(parentPath, name string, _ *openapi3.Schema, _ *diff.SchemaDiff, _ bool) {
		c.add("hikyo-property-removed", operation, path,
			fmt.Sprintf("%s property %q removed from the frozen API", context, schemawalk.PropertyFullName(parentPath, name)))
	})
	c.schemaNode(schema, operation, path, context)
	schemawalk.Walker{Enter: func(parentPath, name string, node, parent *diff.SchemaDiff, _ bool) {
		if deletedEmptySubschema(node, parent) {
			return
		}
		location := parentPath
		if name != "" {
			location = schemawalk.PropertyFullName(parentPath, name)
		}
		c.schemaNode(node, operation, path, context+" at "+location)
	}}.Walk(schema)
}

// An empty schema constrains nothing, so deleting one removes nothing. A deleted
// node carries no base of its own: the parent holds what was declared.
func deletedEmptySubschema(node, parent *diff.SchemaDiff) bool {
	if !node.SchemaDeleted || parent == nil || parent.Base == nil {
		return false
	}
	var ref *openapi3.SchemaRef
	switch node {
	case parent.ItemsDiff:
		ref = parent.Base.Items
	case parent.AdditionalPropertiesDiff:
		ref = parent.Base.AdditionalProperties.Schema
	}
	if ref == nil || ref.Value == nil {
		return false
	}
	raw, err := ref.Value.MarshalJSON()
	return err == nil && string(raw) == "{}"
}

// schemaNode reports what no upstream check does: a whole subschema deleted,
// such as an array's items, takes its nested properties with it unvisited, and
// upstream checks open-enum removals on requests only.
func (c *freezeRemovalCheck) schemaNode(node *diff.SchemaDiff, operation, path, context string) {
	if node.SchemaDeleted {
		c.add("hikyo-schema-removed", operation, path, context+" schema removed from the frozen API")
		return
	}
	if node.Base == nil || node.Revision == nil {
		return
	}
	base, _ := node.Base.Extensions[ExtOpenEnum].([]any)
	revised, _ := node.Revision.Extensions[ExtOpenEnum].([]any)
	for _, value := range base {
		if !slices.ContainsFunc(revised, func(other any) bool { return reflect.DeepEqual(value, other) }) {
			c.add("hikyo-enum-value-removed", operation, path, fmt.Sprintf("%s open enum value %v removed from the frozen API", context, value))
		}
	}
}
