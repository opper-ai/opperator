package opper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tui/internal/keyring"
	"tui/internal/protocol"
)

// ParseCommandArguments uses an LLM to parse a raw slash command
// string into a structured map of arguments based on the command's argument schema.
func ParseCommandArguments(ctx context.Context, rawInput string, schema []protocol.CommandArgument) (map[string]any, error) {
	if len(schema) == 0 {
		// No schema defined, return empty args
		return map[string]any{}, nil
	}

	trimmed := strings.TrimSpace(rawInput)
	if trimmed == "" {
		// For empty input, return defaults or error if required
		result := map[string]any{}
		for _, arg := range schema {
			if arg.Required && arg.Default == nil {
				return nil, fmt.Errorf("missing required argument '%s'", arg.Name)
			}
			if arg.Default != nil {
				result[arg.Name] = arg.Default
			}
		}
		return result, nil
	}

	// Get API key
	apiKey, err := keyring.GetAPIKey()
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, fmt.Errorf("Opper API key not configured. Run: opperator secret create --name=%s", keyring.OpperAPIKeyName)
		}
		return nil, fmt.Errorf("failed to read Opper API key: %w", err)
	}

	// Build the argument schema for the LLM
	outputSchema := buildArgumentSchema(schema)

	// Build context description from the schema
	schemaDescription := buildSchemaDescription(schema)

	// Build the prompt for the LLM
	instructions := fmt.Sprintf(`You are a parameter parser for a command-line interface. Your task is to parse user input into structured command arguments.

The user will provide text that should be parsed into the following parameters:

%s

Extract the values from the user input and return them as a JSON object. For missing optional parameters, omit them from the response. If a required parameter cannot be determined from the input, indicate this in an error message instead of leaving it blank.

Be flexible in interpreting the input - users may provide values in various formats or orders. Extract the intended meaning.`, schemaDescription)

	client := New(apiKey)
	req := StreamRequest{
		Name:         "opperator.slash_command_parser",
		Instructions: &instructions,
		Input: map[string]any{
			"raw_input": trimmed,
		},
		OutputSchema: outputSchema,
	}

	// Stream the response from Opper
	events, err := client.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("API error during argument parsing: %w", err)
	}

	// Aggregate the JSON chunks
	aggregator := NewJSONChunkAggregator()
	for event := range events {
		chunk := event.Data
		if chunk.JSONPath != "" || chunk.ChunkType == "json" {
			aggregator.Add(chunk.JSONPath, chunk.Delta)
		}
	}

	assembled, err := aggregator.Assemble()
	if err != nil {
		return nil, fmt.Errorf("failed to assemble parsed arguments: %w", err)
	}

	// Parse the assembled JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(assembled), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
	}

	// Clean up the result and apply defaults
	cleaned := cleanupParsedArguments(result, schema)

	// Validate required arguments
	if err := validateRequiredArguments(cleaned, schema); err != nil {
		return nil, err
	}

	return cleaned, nil
}

// buildArgumentSchema creates a JSON schema object for the argument definitions
func buildArgumentSchema(args []protocol.CommandArgument) map[string]any {
	properties := map[string]any{}
	required := []string{}

	for _, arg := range args {
		argSchema := map[string]any{
			"type":        arg.Type,
			"description": arg.Description,
		}

		if arg.Default != nil {
			argSchema["default"] = arg.Default
		}

		if len(arg.Enum) > 0 {
			argSchema["enum"] = arg.Enum
		}

		if arg.Items != nil && len(arg.Items) > 0 {
			argSchema["items"] = arg.Items
		}

		if arg.Properties != nil && len(arg.Properties) > 0 {
			argSchema["properties"] = arg.Properties
		}

		properties[arg.Name] = argSchema

		if arg.Required {
			required = append(required, arg.Name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// buildSchemaDescription creates a human-readable description of the argument schema
func buildSchemaDescription(args []protocol.CommandArgument) string {
	var parts []string

	for _, arg := range args {
		var desc strings.Builder
		desc.WriteString(fmt.Sprintf("- %s", arg.Name))

		if arg.Type != "" && arg.Type != "string" {
			desc.WriteString(fmt.Sprintf(" (%s)", arg.Type))
		}

		if arg.Required {
			desc.WriteString(" [REQUIRED]")
		}

		if arg.Description != "" {
			desc.WriteString(fmt.Sprintf(": %s", arg.Description))
		}

		if arg.Default != nil {
			desc.WriteString(fmt.Sprintf(" (default: %v)", arg.Default))
		}

		if len(arg.Enum) > 0 {
			enumStrs := make([]string, 0, len(arg.Enum))
			for _, e := range arg.Enum {
				enumStrs = append(enumStrs, fmt.Sprintf("%v", e))
			}
			desc.WriteString(fmt.Sprintf(" [options: %s]", strings.Join(enumStrs, ", ")))
		}

		parts = append(parts, desc.String())
	}

	return strings.Join(parts, "\n")
}

// cleanupParsedArguments removes nil/empty values and applies defaults
func cleanupParsedArguments(parsed map[string]any, schema []protocol.CommandArgument) map[string]any {
	result := map[string]any{}

	for _, arg := range schema {
		value, exists := parsed[arg.Name]

		// If value exists and is not nil/empty, use it
		if exists && value != nil {
			// Handle empty strings - might want to treat them as missing
			if str, ok := value.(string); ok && strings.TrimSpace(str) == "" {
				if arg.Default != nil {
					result[arg.Name] = arg.Default
				}
				continue
			}
			result[arg.Name] = value
			continue
		}

		// Use default if available
		if arg.Default != nil {
			result[arg.Name] = arg.Default
		}
	}

	return result
}

// validateRequiredArguments checks that all required arguments have values
func validateRequiredArguments(parsed map[string]any, schema []protocol.CommandArgument) error {
	for _, arg := range schema {
		if !arg.Required {
			continue
		}

		value, exists := parsed[arg.Name]
		if !exists || value == nil {
			return fmt.Errorf("missing required argument '%s'", arg.Name)
		}

		// Check for empty strings
		if str, ok := value.(string); ok && strings.TrimSpace(str) == "" {
			return fmt.Errorf("required argument '%s' cannot be empty", arg.Name)
		}
	}

	return nil
}
