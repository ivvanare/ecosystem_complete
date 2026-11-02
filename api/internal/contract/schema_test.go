package contract

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// A property declared `object` with neither `properties` nor `additionalProperties` reads as a
// CLOSED EMPTY record in generators: openapi-typescript turns it into Record<string, never>, so a
// client cannot even assign the payload the API actually stores. The API bug this pins was
// exactly that: AuditEvent.metadata was a bare `object` while Go marshals map[string]any into it.
//
// `additionalProperties: true` (or a schema) is the way to say "any object", and the contract
// already used it for Error.details. This test keeps the next one from being forgotten.
func TestObjectPropertiesDeclareWhetherTheyAreOpen(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal([]byte(read(t, openapiDoc)), &spec); err != nil {
		t.Fatalf("parsing openapi.yaml: %v", err)
	}
	if len(spec.Components.Schemas) == 0 {
		t.Fatal("openapi.yaml declares no components.schemas")
	}

	checked := 0
	for schemaName, schema := range spec.Components.Schemas {
		props, ok := objectAt(schema)["properties"].(map[string]any)
		if !ok {
			continue
		}
		for propName, raw := range props {
			prop, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if !declaresObject(prop["type"]) {
				continue
			}
			checked++
			// A property that describes its own fields is complete on its own.
			if _, hasProperties := prop["properties"]; hasProperties {
				continue
			}
			if _, ok := prop["additionalProperties"]; !ok {
				t.Errorf("%s.%s is an object with no properties and no additionalProperties: "+
					"generators read that as a closed empty record, not as free-form JSON",
					schemaName, propName)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no object-typed properties to check; the walk is broken")
	}
}

// objectAt normalises a YAML node that is expected to be a mapping.
func objectAt(node any) map[string]any {
	m, _ := node.(map[string]any)
	return m
}

// declaresObject reports whether a `type` value names an object, alone or inside a list such as
// [object, "null"].
func declaresObject(node any) bool {
	switch v := node.(type) {
	case string:
		return v == "object"
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == "object" {
				return true
			}
		}
	}
	return false
}
