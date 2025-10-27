package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
)

const (
	ReadDocumentationToolName = "read_documentation"
)

//go:embed docs/agent-basics-guide.md
var agentBasicsGuideDoc string

//go:embed docs/system-prompt-guide.md
var systemPromptGuideDoc string

//go:embed docs/agent-tools-guide.md
var agentToolsGuideDoc string

//go:embed docs/sections-guide.md
var sectionsGuideDoc string

//go:embed docs/lifecycle-events-guide.md
var lifecycleEventsGuideDoc string

//go:embed docs/secret-keys-guide.md
var secretKeysGuideDoc string

//go:embed docs/working-directory-guide.md
var workingDirectoryGuideDoc string

//go:embed docs/agents-yaml-guide.md
var agentsYamlGuideDoc string

//go:embed docs/background-tasks-guide.md
var backgroundTasksGuideDoc string

//go:embed docs/opper-sdk-guide.md
var opperSDKGuideDoc string

//go:embed docs/python-dependencies-guide.md
var pythonDependenciesGuideDoc string

//go:embed docs/docs.json
var docsJSON string

// DocumentInfo represents metadata about a documentation file
type DocumentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GetAvailableDocuments returns the list of available documentation files
func GetAvailableDocuments() ([]DocumentInfo, error) {
	var docs []DocumentInfo
	if err := json.Unmarshal([]byte(docsJSON), &docs); err != nil {
		return nil, fmt.Errorf("failed to parse docs.json: %w", err)
	}
	return docs, nil
}

// ReadDocumentationParams represents the parameters for the read_documentation command
type ReadDocumentationParams struct {
	Names []string `json:"names"`
}

// ReadDocumentationSpec returns the spec for the read_documentation command
func ReadDocumentationSpec() Spec {
	// Load available documents dynamically
	docs, err := GetAvailableDocuments()
	var enumValues []string
	var descriptions []string

	if err == nil && len(docs) > 0 {
		for _, doc := range docs {
			enumValues = append(enumValues, doc.Name)
			descriptions = append(descriptions, fmt.Sprintf("- %s: %s", doc.Name, doc.Description))
		}
	} else {
		// Fallback to hardcoded values if docs.json can't be loaded
		enumValues = []string{"sections-guide.md"}
		descriptions = []string{
			"- sections-guide.md: Guide for custom sidebar sections",
		}
	}

	// Build description with available docs
	descriptionText := "Array of documentation file names to read. Available documents:\n" +
		fmt.Sprintf("%s", descriptions[0])
	for i := 1; i < len(descriptions); i++ {
		descriptionText += "\n" + descriptions[i]
	}

	return Spec{
		Name:        "read_documentation",
		Description: "Read embedded documentation files. Useful for accessing reference materials and guides that are built into the binary.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"names": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": enumValues,
					},
					"description": descriptionText,
				},
			},
			"required": []string{"names"},
		},
	}
}

// RunReadDocumentation executes the read_documentation command
func RunReadDocumentation(ctx context.Context, argsJSON string) (string, string) {
	var params ReadDocumentationParams
	if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
		return fmt.Sprintf("Error: failed to parse arguments: %v", err), ""
	}

	if len(params.Names) == 0 {
		return "Error: no documentation names provided", ""
	}

	// Map of available documentation
	docs := map[string]string{
		"agent-basics-guide.md":        agentBasicsGuideDoc,
		"system-prompt-guide.md":       systemPromptGuideDoc,
		"agent-tools-guide.md":         agentToolsGuideDoc,
		"sections-guide.md":            sectionsGuideDoc,
		"lifecycle-events-guide.md":    lifecycleEventsGuideDoc,
		"secret-keys-guide.md":         secretKeysGuideDoc,
		"working-directory-guide.md":   workingDirectoryGuideDoc,
		"agents-yaml-guide.md":         agentsYamlGuideDoc,
		"background-tasks-guide.md":    backgroundTasksGuideDoc,
		"opper-sdk-guide.md":           opperSDKGuideDoc,
		"python-dependencies-guide.md": pythonDependenciesGuideDoc,
	}

	var result string
	var readDocs []string

	for _, name := range params.Names {
		content, exists := docs[name]
		if !exists {
			return fmt.Sprintf("Error: documentation file not found: %s", name), ""
		}
		result += content + "\n\n"
		readDocs = append(readDocs, name)
	}

	// Create metadata for the renderer
	metadataMap := map[string]any{
		"read_docs": readDocs,
	}
	metadata, err := json.Marshal(metadataMap)
	if err != nil {
		return fmt.Sprintf("Error: failed to create metadata: %v", err), ""
	}

	// Return: content for Claude (the actual documentation), metadata for renderer
	return result, string(metadata)
}
