package tools

type Spec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToAPIDefinition converts the spec into the JSON structure expected by the Opper API.
func (s Spec) ToAPIDefinition() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        s.Name,
			"description": s.Description,
			"parameters":  s.Parameters,
		},
	}
}

// SpecsToAPIDefinitions converts a slice of Spec into the payload format used by the Opper API.
func SpecsToAPIDefinitions(specs []Spec) []map[string]any {
	if len(specs) == 0 {
		return nil
	}
	defs := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		defs = append(defs, spec.ToAPIDefinition())
	}
	return defs
}
