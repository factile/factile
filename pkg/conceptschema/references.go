package conceptschema

import (
	"github.com/santhosh-tekuri/jsonschema/v6"
	"strconv"
)

var schemaMaps = []string{"$defs", "dependentSchemas", "patternProperties", "properties"}
var schemaSingles = []string{"additionalProperties", "contains", "contentSchema", "else", "if", "items", "not", "propertyNames", "then", "unevaluatedItems", "unevaluatedProperties"}
var schemaArrays = []string{"allOf", "anyOf", "oneOf", "prefixItems"}

type schemaNode struct {
	value   any
	pointer string
}

func executableSchemas(root any, meta *jsonschema.Schema, b *budget) ([]any, bool) {
	pending := []schemaNode{{root, ""}}
	seen := map[string]bool{}
	nodes := []any{}
	for len(pending) > 0 {
		item := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[item.pointer] {
			continue
		}
		seen[item.pointer] = true
		b.size(item.value, 0, true)
		if meta.Validate(item.value) != nil {
			return nil, false
		}
		nodes = append(nodes, item.value)
		object, ok := item.value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range schemaSingles {
			if child, ok := object[key]; ok {
				pending = append(pending, schemaNode{child, item.pointer + "/" + key})
			}
		}
		for _, key := range schemaArrays {
			if list, ok := object[key].([]any); ok {
				for i, child := range list {
					pending = append(pending, schemaNode{child, item.pointer + "/" + key + "/" + strconv.Itoa(i)})
				}
			}
		}
		for _, key := range schemaMaps {
			if children, ok := object[key].(map[string]any); ok {
				for name, child := range children {
					pending = append(pending, schemaNode{child, item.pointer + "/" + key + "/" + escapePointer(name)})
				}
			}
		}
		if ref, ok := object["$ref"].(string); ok {
			if target, path, ok := pointer(root, ref); ok {
				switch target.(type) {
				case bool, map[string]any:
					pending = append(pending, schemaNode{target, path})
				}
			}
		}
	}
	return nodes, true
}

func safeReferences(root any, nodes []any) bool {
	for _, node := range nodes {
		object, ok := node.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"$id", "$anchor", "$dynamicAnchor", "$dynamicRef"} {
			if _, ok := object[key]; ok {
				return false
			}
		}
		if raw, ok := object["$ref"]; ok {
			ref, ok := raw.(string)
			if !ok {
				return false
			}
			target, _, ok := pointer(root, ref)
			if !ok {
				return false
			}
			switch target.(type) {
			case bool, map[string]any:
			default:
				return false
			}
		}
	}
	return true
}
