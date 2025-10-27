package opper

// compatible with the Opper API fields `input_schema` and `output_schema`.
// It does not aim to cover the entire JSON Schema spec, but focuses on
// common primitives to keep call sites concise.
type JSONSchema map[string]any

func Object() JSONSchema {
	return JSONSchema{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func String() JSONSchema { return JSONSchema{"type": "string"} }

func Integer() JSONSchema { return JSONSchema{"type": "integer"} }

func Number() JSONSchema { return JSONSchema{"type": "number"} }

func Boolean() JSONSchema { return JSONSchema{"type": "boolean"} }

func Array(items JSONSchema) JSONSchema { return JSONSchema{"type": "array", "items": items} }

func Ref(path string) JSONSchema { return JSONSchema{"$ref": path} }

func (s JSONSchema) Title(t string) JSONSchema { s["title"] = t; return s }

func (s JSONSchema) Description(d string) JSONSchema { s["description"] = d; return s }

func (s JSONSchema) Format(f string) JSONSchema { s["format"] = f; return s }

func (s JSONSchema) Enum(vals ...any) JSONSchema { s["enum"] = vals; return s }

func (s JSONSchema) Default(v any) JSONSchema { s["default"] = v; return s }

// Property adds/updates a property on an object schema.
func (s JSONSchema) Property(name string, schema JSONSchema) JSONSchema {
	if props, ok := s["properties"].(map[string]any); ok {
		props[name] = schema
	}
	return s
}

// Properties bulk sets object properties from a map.
func (s JSONSchema) Properties(m map[string]JSONSchema) JSONSchema {
	if props, ok := s["properties"].(map[string]any); ok {
		for k, v := range m {
			props[k] = v
		}
	}
	return s
}

func (s JSONSchema) Require(names ...string) JSONSchema { s["required"] = names; return s }

func (s JSONSchema) Defs(defs map[string]JSONSchema) JSONSchema {
	m := map[string]any{}
	for k, v := range defs {
		m[k] = v
	}
	s["$defs"] = m
	return s
}
