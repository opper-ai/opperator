package argparser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CommandArgument represents a command parameter schema
type CommandArgument struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type,omitempty"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required,omitempty"`
	Default     interface{}            `json:"default,omitempty"`
	Enum        []interface{}          `json:"enum,omitempty"`
	Items       map[string]interface{} `json:"items,omitempty"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
}

// OpperClient interface for Opper API calls
type OpperClient interface {
	Stream(ctx context.Context, req StreamRequest) (<-chan SSEEvent, error)
}

// StreamRequest represents a request to the Opper API
type StreamRequest struct {
	Name         string
	Instructions *string
	Input        map[string]any
	OutputSchema map[string]any
}

// SSEEvent represents a streaming event from the Opper API
type SSEEvent struct {
	Data ChunkData
}

// ChunkData represents the data in a streaming chunk
type ChunkData struct {
	JSONPath  string
	ChunkType string
	Delta     interface{}
}

// JSONAggregator interface for aggregating JSON chunks
type JSONAggregator interface {
	Add(path string, delta interface{})
	Assemble() (string, error)
}

// ParseCommandArguments uses an LLM to parse a raw command string
// into a structured map of arguments based on the command's argument schema.
func ParseCommandArguments(ctx context.Context, apiKey, rawInput string, schema []CommandArgument, client OpperClient, aggregator JSONAggregator) (map[string]any, error) {
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

	// Check if LLM reported an error
	llmError, hasError := result["error"].(string)
	if hasError && strings.TrimSpace(llmError) != "" {
		// Remove the error field from the result since it's not an actual argument
		delete(result, "error")
	}

	// Clean up the result and apply defaults
	cleaned := cleanupParsedArguments(result, schema)

	// If LLM reported an error AND there are missing required arguments, return the error
	if hasError && strings.TrimSpace(llmError) != "" {
		if err := validateRequiredArguments(cleaned, schema); err != nil {
			// Return the LLM's error message instead of validation error
			return cleaned, fmt.Errorf(llmError)
		}
		// All required arguments are present despite the error, so continue with execution
		return cleaned, nil
	}

	// Validate required arguments
	if err := validateRequiredArguments(cleaned, schema); err != nil {
		return nil, err
	}

	return cleaned, nil
}

// buildArgumentSchema creates a JSON schema object for the argument definitions
func buildArgumentSchema(args []CommandArgument) map[string]any {
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

	// Add error field for LLM to report parsing issues
	properties["error"] = map[string]any{
		"type":        "string",
		"description": "If there's any issue parsing the arguments or determining the user's intent, describe the problem here. Otherwise, omit this field.",
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
func buildSchemaDescription(args []CommandArgument) string {
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
func cleanupParsedArguments(parsed map[string]any, schema []CommandArgument) map[string]any {
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
func validateRequiredArguments(parsed map[string]any, schema []CommandArgument) error {
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
