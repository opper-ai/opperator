package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"opperator/config"
	"opperator/internal/credentials"
	"opperator/internal/ipc"
	"opperator/internal/protocol"
	"tui/opper"
	"tui/coreagent"
	"tui/tools"

	_ "modernc.org/sqlite"
)

const maxFollowUpRounds = 10 // Prevent infinite loops

// execWithRetry executes a database operation with retry on busy/lock errors
func execWithRetry(ctx context.Context, db *sql.DB, query string, args ...any) error {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		_, err := db.ExecContext(ctx, query, args...)
		if err == nil {
			return nil
		}
		// Check if it's a lock error
		if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "SQLITE_BUSY") {
			if i < maxRetries-1 {
				time.Sleep(time.Millisecond * 100 * time.Duration(i+1)) // Exponential backoff
				continue
			}
		}
		return err
	}
	return nil
}

// Styles for CLI output (matching TUI theme)
var (
	primary   = lipgloss.Color("#f7c0af") // orangish/peach
	secondary = lipgloss.Color("#3ccad7") // cyan
	success   = lipgloss.Color("#87bf47") // green
	errorCol  = lipgloss.Color("#bf5d47") // red
	muted     = lipgloss.Color("#7f7f7f") // gray

	labelStyle       = lipgloss.NewStyle().Foreground(primary).Bold(true)
	valueStyle       = lipgloss.NewStyle().Foreground(secondary)
	mutedStyle       = lipgloss.NewStyle().Foreground(muted)
	successStyle     = lipgloss.NewStyle().Foreground(success)
	errorStyle       = lipgloss.NewStyle().Foreground(errorCol)
	sectionStyle     = lipgloss.NewStyle().Foreground(primary).Bold(true).Margin(1, 0, 0, 0)
	bracketStyle     = lipgloss.NewStyle().Foreground(muted)
	veryMutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5f5f5f"))
	veryMutedBracket = lipgloss.NewStyle().Foreground(lipgloss.Color("#4f4f4f"))

	// ANSI color codes for streaming (to avoid lipgloss breaking lines)
	responseColorStart = "\033[38;5;252m" // light gray
	colorReset         = "\033[0m"
)

// ExecMessage sends a message to an agent and returns the response.
// Activity is streamed to stderr, final response to stdout.
func ExecMessage(messageText, agentName, conversationID string) error {
	ctx := context.Background()

	// Get API key
	apiKey, err := credentials.GetSecret(credentials.OpperAPIKeyName)
	if err != nil {
		return fmt.Errorf("failed to read Opper API key: %w (run: op secret create %s)", err, credentials.OpperAPIKeyName)
	}

	// Open database
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	dbPath := filepath.Join(home, ".config", "opperator", "opperator.db")
	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=30000")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Set connection pool limits to reduce contention
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Load or create conversation
	var convID, convTitle string
	var history []conversationMessage

	if conversationID != "" {
		// Resume existing conversation
		row := db.QueryRowContext(ctx,
			`SELECT id, title, active_agent FROM conversations WHERE id = ?`,
			conversationID)
		var activeAgent sql.NullString
		if err := row.Scan(&convID, &convTitle, &activeAgent); err != nil {
			return fmt.Errorf("conversation not found: %w", err)
		}

		// Use agent from conversation if not specified
		if agentName == "" && activeAgent.Valid {
			agentName = activeAgent.String
		}

		// Load message history
		rows, err := db.QueryContext(ctx,
			`SELECT role, metadata FROM messages WHERE session_id = ? ORDER BY id`,
			conversationID)
		if err != nil {
			return fmt.Errorf("failed to load conversation history: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var role, metadata string
			if err := rows.Scan(&role, &metadata); err != nil {
				continue
			}
			msg := parseMessageFromMetadata(role, metadata)
			history = append(history, msg)
		}
	} else {
		// Create new conversation
		// Use first 50 chars of message as title
		titleText := messageText
		if len(titleText) > 50 {
			titleText = titleText[:50] + "..."
		}
		convTitle = titleText
		convID = fmt.Sprintf("%d", time.Now().UnixNano())

		err = execWithRetry(ctx, db,
			`INSERT INTO conversations(id, title, created_at) VALUES(?, ?, ?)`,
			convID, convTitle, time.Now().Unix())
		if err != nil {
			return fmt.Errorf("failed to create conversation: %w", err)
		}
	}

	// Determine which agent to use
	if agentName == "" {
		agentName, err = getDefaultAgent()
		if err != nil {
			return fmt.Errorf("no agent specified and no default found: %w", err)
		}
	}

	// Check if this is a core agent
	var agentPrompt string
	var agentPromptReplace bool
	var toolSpecs []tools.Spec
	var isCoreAgent bool

	if coreDef, ok := coreagent.Lookup(agentName); ok {
		// Use core agent definition
		isCoreAgent = true
		agentPrompt = coreDef.Prompt
		toolSpecs = coreDef.Tools

		// Add agent tool for Opperator (allows spawning sub-agents)
		if coreDef.ID == coreagent.IDOpperator {
			// Get list of available agents for the agent tool spec
			agentOptions := getAgentOptions()
			toolSpecs = append(toolSpecs, tools.AgentSpec(agentOptions))
			// Note: Agent list context is now added by buildInstructions()
		}

		// Display core agent info
		fmt.Fprintln(os.Stderr, labelStyle.Render("Agent:")+" "+valueStyle.Render(coreDef.Name))
		fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render("Core agent"))
		if len(toolSpecs) > 0 {
			fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(fmt.Sprintf("Tools: %d available", len(toolSpecs))))
		}
		fmt.Fprintln(os.Stderr, mutedStyle.Render("────────────────────────────────────────────────"))
	} else {
		// Regular agent - get metadata via IPC
		agentDesc, prompt, promptReplace, commands, err := getAgentMetadataAndCommands(agentName)
		if err != nil {
			fmt.Fprintln(os.Stderr, errorStyle.Render("Warning:")+" "+mutedStyle.Render(err.Error()))
		}
		agentPrompt = prompt
		agentPromptReplace = promptReplace

		// Convert commands to tool specs
		toolSpecs = commandsToToolSpecs(agentName, commands)

		// Display agent info
		fmt.Fprintln(os.Stderr, labelStyle.Render("Agent:")+" "+valueStyle.Render(agentName))
		if agentDesc != "" {
			fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(agentDesc))
		}
		if len(toolSpecs) > 0 {
			fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(fmt.Sprintf("Tools: %d available", len(toolSpecs))))
		}
		fmt.Fprintln(os.Stderr, mutedStyle.Render("────────────────────────────────────────────────"))
	}

	// Update conversation active agent (NULL for core agents, agent name for managed agents)
	var activeAgentValue any
	if isCoreAgent {
		activeAgentValue = nil
	} else {
		activeAgentValue = agentName
	}
	err = execWithRetry(ctx, db,
		`UPDATE conversations SET active_agent = ? WHERE id = ?`,
		activeAgentValue, convID)
	if err != nil {
		return fmt.Errorf("failed to update active agent: %w", err)
	}

	// Add user message to history and save
	now := time.Now().Unix()
	userMetadata := createTextMetadata(messageText)
	err = execWithRetry(ctx, db,
		`INSERT INTO messages(session_id, role, metadata, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		convID, "user", userMetadata, now, now)
	if err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}
	history = append(history, conversationMessage{Role: "user", Content: messageText})

	// Convert tool specs to API definitions
	toolDefs := tools.SpecsToAPIDefinitions(toolSpecs)

	// Build instructions - match TUI behavior with agent context
	instructions := buildInstructions(agentName, agentPrompt, agentPromptReplace, isCoreAgent)

	// Create Opper client
	client := opper.New(apiKey)

	// Get IPC client for tool execution (not needed for core agents)
	var ipcClient *ipc.Client
	if !isCoreAgent {
		var err error
		ipcClient, _, err = getClientForAgent(agentName, "")
		if err != nil {
			return fmt.Errorf("failed to connect to agent daemon: %w", err)
		}
		defer ipcClient.Close()
	}

	// Stream response with full tool execution loop
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, sectionStyle.Render("Response"))
	fmt.Fprintln(os.Stderr, "")

	finalResponse, err := executeConversationLoop(ctx, client, ipcClient, agentName, history, toolDefs, instructions, db, convID)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, mutedStyle.Render("────────────────────────────────────────────────"))
	fmt.Fprintln(os.Stderr, mutedStyle.Render("Continue: ")+"op exec --resume "+valueStyle.Render(convID))
	fmt.Fprintln(os.Stderr, "")

	// Write final response to stdout
	fmt.Println(finalResponse)

	return nil
}

// executeConversationLoop handles the full conversation loop with tool execution
func executeConversationLoop(
	ctx context.Context,
	client *opper.Opper,
	ipcClient *ipc.Client,
	agentName string,
	history []conversationMessage,
	tools []map[string]any,
	instructions string,
	db *sql.DB,
	convID string,
) (string, error) {
	currentHistory := append([]conversationMessage{}, history...)
	roundCount := 0

	for {
		if roundCount >= maxFollowUpRounds {
			return "", fmt.Errorf("exceeded maximum follow-up rounds (%d)", maxFollowUpRounds)
		}
		roundCount++

		// Build conversation for API
		conversation := buildConversation(currentHistory)

		// Build request
		input := map[string]any{
			"conversation": conversation,
		}
		if len(tools) > 0 {
			input["tools"] = tools
		}

		req := opper.StreamRequest{
			Name:         "opperator.session",
			Input:        input,
			OutputSchema: sessionOutputSchema(),
			Model:        modelIdentifier(),
		}
		if instructions != "" {
			req.Instructions = &instructions
		}

		// Stream response
		events, err := client.Stream(ctx, req)
		if err != nil {
			return "", fmt.Errorf("failed to start stream: %w", err)
		}

		// Parse streaming response (no indentation for main agent)
		result, err := parseStreamingResponse(events, "")
		if err != nil {
			return "", err
		}

		// Add newline after streaming text completes
		if strings.TrimSpace(result.Text) != "" {
			fmt.Fprintln(os.Stderr, "")
		}

		// If no tool calls, we're done - return the text
		if len(result.ToolCalls) == 0 {
			// Save assistant message if we have text
			if strings.TrimSpace(result.Text) != "" {
				assistantMetadata := createTextMetadata(result.Text)
				now := time.Now().Unix()
				err = execWithRetry(ctx, db,
					`INSERT INTO messages(session_id, role, metadata, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
					convID, "assistant", assistantMetadata, now, now)
				if err != nil {
					fmt.Fprintln(os.Stderr, errorStyle.Render("Warning:")+" "+mutedStyle.Render(fmt.Sprintf("failed to save message: %v", err)))
				}
			}
			return result.Text, nil
		}

		// We have tool calls - save assistant message with text first
		if strings.TrimSpace(result.Text) != "" {
			currentHistory = append(currentHistory, conversationMessage{
				Role:    "assistant",
				Content: result.Text,
			})

			// Save assistant message to database
			assistantMetadata := createTextMetadata(result.Text)
			now := time.Now().Unix()
			err = execWithRetry(ctx, db,
				`INSERT INTO messages(session_id, role, metadata, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
				convID, "assistant", assistantMetadata, now, now)
			if err != nil {
				fmt.Fprintln(os.Stderr, errorStyle.Render("Warning:")+" "+mutedStyle.Render(fmt.Sprintf("failed to save assistant message: %v", err)))
			}
		}

		// Save each tool call as separate message with role "tool_call"
		for _, tc := range result.ToolCalls {
			currentHistory = append(currentHistory, conversationMessage{
				Role:      "tool_call",
				ToolCalls: []ToolCall{tc},
			})

			// Save tool call to database
			toolCallMetadata := createToolCallMetadata(tc)
			now := time.Now().Unix()
			err = execWithRetry(ctx, db,
				`INSERT INTO messages(session_id, role, metadata, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
				convID, "tool_call", toolCallMetadata, now, now)
			if err != nil {
				fmt.Fprintln(os.Stderr, errorStyle.Render("Warning:")+" "+mutedStyle.Render(fmt.Sprintf("failed to save tool call: %v", err)))
			}
		}

		// Execute tool calls
		fmt.Fprintln(os.Stderr, bracketStyle.Render("[")+mutedStyle.Render(fmt.Sprintf("Executing %d tool(s)", len(result.ToolCalls)))+bracketStyle.Render("]"))
		toolResults := executeToolCalls(ctx, ipcClient, agentName, result.ToolCalls)

		// Add tool results to history and save to database as "tool_call_response"
		for _, toolResult := range toolResults {
			currentHistory = append(currentHistory, conversationMessage{
				Role:       "tool_call_response",
				ToolCallID: toolResult.ID,
				Content:    toolResult.Output,
			})

			// Save tool result to database
			toolResponseMetadata := createToolCallResponseMetadata(toolResult.ID, toolResult.Name, toolResult.Output)
			now := time.Now().Unix()
			err = execWithRetry(ctx, db,
				`INSERT INTO messages(session_id, role, metadata, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
				convID, "tool_call_response", toolResponseMetadata, now, now)
			if err != nil {
				fmt.Fprintln(os.Stderr, errorStyle.Render("Warning:")+" "+mutedStyle.Render(fmt.Sprintf("failed to save tool response: %v", err)))
			}
		}

		// Loop continues with updated history
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, veryMutedBracket.Render("[")+veryMutedStyle.Render("Continuing...")+veryMutedBracket.Render("]"))
		fmt.Fprintln(os.Stderr, "")
	}
}

// StreamResult holds parsed streaming response
type StreamResult struct {
	Text      string
	ToolCalls []ToolCall
}

// ToolCall represents a tool invocation
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult holds tool execution result
type ToolResult struct {
	ID     string
	Name   string
	Output string
	Error  bool
}

// parseStreamingResponse parses the SSE stream and extracts text and tool calls
func parseStreamingResponse(events <-chan opper.SSEEvent, indent string) (StreamResult, error) {
	var result StreamResult
	var textBuilder strings.Builder
	aggregator := opper.NewJSONChunkAggregator()

	// Track if we've started streaming to set color once
	streamStarted := false
	atLineStart := true // Track if we're at the beginning of a line

	for event := range events {
		chunk := event.Data

		// Handle JSON chunks (structured output)
		if chunk.JSONPath != "" || chunk.ChunkType == "json" {
			path := chunk.JSONPath
			if path == "" {
				path = "text"
			}
			aggregator.Add(path, chunk.Delta)

			// Capture and display text field
			isTextField := path == "text" || strings.HasSuffix(path, ".text")
			if isTextField {
				if deltaStr, ok := chunk.Delta.(string); ok && deltaStr != "" {
					if !streamStarted {
						if indent != "" {
							fmt.Fprint(os.Stderr, indent)
						}
						fmt.Fprint(os.Stderr, responseColorStart)
						streamStarted = true
						atLineStart = false // We just printed the indent
					}
					textBuilder.WriteString(deltaStr)
					// Add indentation at the start of each line
					for _, ch := range deltaStr {
						if atLineStart && indent != "" {
							fmt.Fprint(os.Stderr, indent)
						}
						fmt.Fprint(os.Stderr, string(ch))
						atLineStart = (ch == '\n')
					}
				}
			}
		} else if deltaStr, ok := chunk.Delta.(string); ok && deltaStr != "" {
			// Plain text streaming
			if !streamStarted {
				if indent != "" {
					fmt.Fprint(os.Stderr, indent)
				}
				fmt.Fprint(os.Stderr, responseColorStart)
				streamStarted = true
				atLineStart = false // We just printed the indent
			}
			textBuilder.WriteString(deltaStr)
			// Add indentation at the start of each line
			for _, ch := range deltaStr {
				if atLineStart && indent != "" {
					fmt.Fprint(os.Stderr, indent)
				}
				fmt.Fprint(os.Stderr, string(ch))
				atLineStart = (ch == '\n')
			}
		}
	}

	// Reset color at end of stream
	if streamStarted {
		fmt.Fprint(os.Stderr, colorReset)
	}

	result.Text = strings.TrimSpace(textBuilder.String())

	// Try to parse structured output for tool calls
	assembled, err := aggregator.Assemble()
	if err == nil && assembled != "" {
		var output struct {
			Text  string `json:"text"`
			Tools []struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"tools"`
		}

		if err := json.Unmarshal([]byte(assembled), &output); err == nil {
			for i, tool := range output.Tools {
				result.ToolCalls = append(result.ToolCalls, ToolCall{
					ID:        fmt.Sprintf("tool_%d_%d", time.Now().UnixNano(), i),
					Name:      tool.Name,
					Arguments: tool.Arguments,
				})
			}
		}
	}

	return result, nil
}

// executeToolCalls executes tool calls via IPC or directly for core agents
func executeToolCalls(ctx context.Context, ipcClient *ipc.Client, agentName string, toolCalls []ToolCall) []ToolResult {
	results := make([]ToolResult, 0, len(toolCalls))

	// Check if this is a core agent
	isCoreAgent := ipcClient == nil

	for _, call := range toolCalls {
		// Extract command name from tool name (format: agentName__commandName)
		commandName := strings.TrimPrefix(call.Name, agentName+"__")

		// Display tool name without agentName__ prefix
		displayName := call.Name
		if isCoreAgent {
			displayName = commandName
		}
		fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render("→")+" "+labelStyle.Render(displayName))

		var output string
		var isError bool

		if isCoreAgent {
			// Execute core agent tool directly
			output, isError = executeCoreAgentTool(ctx, call.Name, call.Arguments)
			if isError {
				fmt.Fprintln(os.Stderr, "    "+errorStyle.Render("✗")+" "+mutedStyle.Render("failed"))
			} else {
				fmt.Fprintln(os.Stderr, "    "+successStyle.Render("✓")+" "+mutedStyle.Render("success"))
			}
		} else {
			// Execute command via IPC (use very long timeout for async commands)
			// Track progress messages (limit to 5 lines)
			progressLines := make([]string, 0, 5)
			progressFn := func(prog protocol.CommandProgressMessage) {
				if prog.Text != "" {
					progressLines = append(progressLines, prog.Text)
					// Keep only last 5 lines
					if len(progressLines) > 5 {
						progressLines = progressLines[len(progressLines)-5:]
					}
					// Clear previous lines and redraw
					fmt.Fprint(os.Stderr, "\r\033[K")  // Clear current line
					for i := 0; i < len(progressLines)-1; i++ {
						fmt.Fprint(os.Stderr, "\033[1A\033[K")  // Move up and clear
					}
					// Print all progress lines
					for _, line := range progressLines {
						fmt.Fprintln(os.Stderr, "    "+mutedStyle.Render(line))
					}
				}
			}
			resp, err := ipcClient.InvokeCommandWithProgress(agentName, commandName, call.Arguments, 30*time.Minute, progressFn)

			if err != nil {
				output = fmt.Sprintf("Error: %v", err)
				isError = true
				fmt.Fprintln(os.Stderr, "    "+errorStyle.Render("✗")+" "+mutedStyle.Render(err.Error()))
			} else if !resp.Success {
				output = fmt.Sprintf("Command failed: %s", resp.Error)
				isError = true
				fmt.Fprintln(os.Stderr, "    "+errorStyle.Render("✗")+" "+mutedStyle.Render(resp.Error))
			} else {
				// Convert result to string
				if resp.Result != nil {
					resultJSON, _ := json.Marshal(resp.Result)
					output = string(resultJSON)

					// Display result output (limit to last 5 lines for long outputs)
					lines := strings.Split(output, "\n")
					displayLines := lines
					if len(lines) > 5 {
						fmt.Fprintln(os.Stderr, "    "+mutedStyle.Render(fmt.Sprintf("... (%d lines omitted)", len(lines)-5)))
						displayLines = lines[len(lines)-5:]
					}
					for _, line := range displayLines {
						if strings.TrimSpace(line) != "" && line != "\"\"" && line != "{}" && line != "null" {
							fmt.Fprintln(os.Stderr, "    "+mutedStyle.Render(line))
						}
					}
				} else {
					output = "Command completed successfully"
				}
				fmt.Fprintln(os.Stderr, "    "+successStyle.Render("✓")+" "+mutedStyle.Render("success"))
			}
		}

		results = append(results, ToolResult{
			ID:     call.ID,
			Name:   call.Name,
			Output: output,
			Error:  isError,
		})
	}

	return results
}

// executeCoreAgentTool executes a core agent tool directly without IPC
func executeCoreAgentTool(ctx context.Context, toolName string, arguments map[string]any) (string, bool) {
	// Marshal arguments to JSON string
	argsJSON, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Sprintf("Error marshaling arguments: %v", err), true
	}
	argsStr := string(argsJSON)

	// Execute based on tool name - call tools.Run* functions directly
	lower := strings.ToLower(toolName)
	switch lower {
	case "agent":
		// Sub-agent invocation - run in a nested context
		output, isError := executeSubAgent(ctx, arguments)
		return output, isError

	case tools.ListAgentsToolName:
		output, _ := tools.RunListAgents(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.StartAgentToolName:
		output, _ := tools.RunStartAgent(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.StopAgentToolName:
		output, _ := tools.RunStopAgent(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.RestartAgentToolName:
		output, _ := tools.RunRestartAgent(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.GetLogsToolName:
		output, _ := tools.RunGetLogs(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.MoveAgentToolName:
		output, _ := tools.RunMoveAgent(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.FocusAgentToolName:
		output, _ := tools.RunFocusAgent(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.BootstrapNewAgentToolName:
		output, _ := tools.RunBootstrapNewAgent(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	case tools.ReadDocumentationToolName:
		output, _ := tools.RunReadDocumentation(ctx, argsStr)
		return output, strings.HasPrefix(strings.ToLower(output), "error")

	default:
		return fmt.Sprintf("Unknown core agent tool: %s", toolName), true
	}
}

// executeSubAgent handles sub-agent invocation via the "agent" tool
func executeSubAgent(ctx context.Context, arguments map[string]any) (string, bool) {
	// Extract parameters
	prompt, _ := arguments["prompt"].(string)
	taskDef, _ := arguments["task_definition"].(string)
	agentName, _ := arguments["agent"].(string)

	if strings.TrimSpace(prompt) == "" {
		return "Error: prompt is required for sub-agent invocation", true
	}

	// Agent must be specified for CLI usage
	if strings.TrimSpace(agentName) == "" {
		return "Error: agent parameter is required. Please specify which managed agent to use for this task.", true
	}

	subAgentDisplay := agentName
	taskDisplay := strings.TrimSpace(taskDef)
	if taskDisplay == "" {
		taskDisplay = "Task"
	}

	fmt.Fprintln(os.Stderr, "\n"+bracketStyle.Render("[")+labelStyle.Render("Sub-Agent")+bracketStyle.Render("]")+" "+valueStyle.Render(subAgentDisplay))
	fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(taskDisplay))

	// Get API key
	apiKey, err := credentials.GetSecret(credentials.OpperAPIKeyName)
	if err != nil {
		return fmt.Sprintf("Error: failed to read Opper API key: %v", err), true
	}

	// Create Opper client for sub-agent
	client := opper.New(apiKey)

	// Get managed agent metadata (Builder not allowed in CLI)
	agentDesc, subAgentPrompt, subAgentPromptReplace, commands, err := getAgentMetadataAndCommands(agentName)
	if err != nil {
		return fmt.Sprintf("Error: managed agent %s not found or not available: %v", agentName, err), true
	}
	_ = agentDesc // Currently unused in this context
	_ = subAgentPromptReplace // Currently unused in this context

	// Use managed agent
	subAgentTools := commandsToToolSpecs(agentName, commands)

	// Get IPC client
	ipcClient, _, err := getClientForAgent(agentName, "")
	if err != nil {
		return fmt.Sprintf("Error: failed to connect to agent %s: %v", agentName, err), true
	}
	defer ipcClient.Close()

	fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(agentDesc)+"\n")

	// Build conversation with user prompt
	history := []conversationMessage{
		{Role: "user", Content: prompt},
	}

	// Convert tool specs to API definitions
	toolDefs := tools.SpecsToAPIDefinitions(subAgentTools)

	// Execute sub-agent conversation loop
	result, err := executeSubAgentLoop(ctx, client, ipcClient, agentName, history, toolDefs, subAgentPrompt)
	if err != nil {
		return fmt.Sprintf("Error: sub-agent execution failed: %v", err), true
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, bracketStyle.Render("[")+mutedStyle.Render("Sub-Agent Complete")+bracketStyle.Render("]"))
	fmt.Fprintln(os.Stderr, "")

	return result, false
}

// executeSubAgentLoop runs the conversation loop for a managed sub-agent
func executeSubAgentLoop(
	ctx context.Context,
	client *opper.Opper,
	ipcClient *ipc.Client,
	agentName string,
	history []conversationMessage,
	tools []map[string]any,
	instructions string,
) (string, error) {
	currentHistory := append([]conversationMessage{}, history...)
	roundCount := 0

	for {
		if roundCount >= maxFollowUpRounds {
			return "", fmt.Errorf("sub-agent exceeded maximum follow-up rounds (%d)", maxFollowUpRounds)
		}
		roundCount++

		// Build conversation for API
		conversation := buildConversation(currentHistory)

		// Build request
		input := map[string]any{
			"conversation": conversation,
		}
		if len(tools) > 0 {
			input["tools"] = tools
		}

		req := opper.StreamRequest{
			Name:         "opperator.agent_tool",
			Input:        input,
			OutputSchema: sessionOutputSchema(),
			Model:        modelIdentifier(),
		}
		if instructions != "" {
			req.Instructions = &instructions
		}

		// Stream response
		events, err := client.Stream(ctx, req)
		if err != nil {
			return "", fmt.Errorf("failed to start stream: %w", err)
		}

		// Parse streaming response (with 2-space indentation for sub-agent)
		result, err := parseStreamingResponse(events, "  ")
		if err != nil {
			return "", err
		}

		// Add newline after streaming text completes
		if strings.TrimSpace(result.Text) != "" {
			fmt.Fprintln(os.Stderr, "")
		}

		// If no tool calls, we're done - return the text
		if len(result.ToolCalls) == 0 {
			return result.Text, nil
		}

		// We have tool calls - add assistant message with tool calls to history
		currentHistory = append(currentHistory, conversationMessage{
			Role:      "assistant",
			Content:   result.Text,
			ToolCalls: result.ToolCalls,
		})

		// Execute tool calls via IPC
		fmt.Fprintln(os.Stderr, "  "+bracketStyle.Render("[")+mutedStyle.Render(fmt.Sprintf("Executing %d tool(s)", len(result.ToolCalls)))+bracketStyle.Render("]"))
		toolResults := executeToolCalls(ctx, ipcClient, agentName, result.ToolCalls)

		// Add tool results to history
		for _, toolResult := range toolResults {
			currentHistory = append(currentHistory, conversationMessage{
				Role:       "tool_call_output",
				ToolCallID: toolResult.ID,
				Content:    toolResult.Output,
			})
		}

		// Loop continues with updated history
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  "+veryMutedBracket.Render("[")+veryMutedStyle.Render("Continuing...")+veryMutedBracket.Render("]"))
		fmt.Fprintln(os.Stderr, "")
	}
}

// conversationMessage represents a message in the conversation
type conversationMessage struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string // For tool_call_output role
}

// buildConversation converts message history to API format
func buildConversation(history []conversationMessage) []map[string]any {
	conversation := make([]map[string]any, 0, len(history))

	for i, msg := range history {
		switch msg.Role {
		case "user":
			if strings.TrimSpace(msg.Content) != "" {
				conversation = append(conversation, map[string]any{
					"role":    "user",
					"content": msg.Content,
				})
			}

		case "assistant":
			entry := map[string]any{
				"role": "assistant",
			}
			if strings.TrimSpace(msg.Content) != "" {
				entry["content"] = msg.Content
			}

			// Check if next messages are tool_call messages
			toolCalls := []ToolCall{}
			j := i + 1
			for j < len(history) && history[j].Role == "tool_call" {
				if len(history[j].ToolCalls) > 0 {
					toolCalls = append(toolCalls, history[j].ToolCalls[0])
				}
				j++
			}

			if len(toolCalls) > 0 {
				apiToolCalls := make([]map[string]any, 0, len(toolCalls))
				for _, tc := range toolCalls {
					argJSON, _ := json.Marshal(tc.Arguments)
					apiToolCalls = append(apiToolCalls, map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": string(argJSON),
						},
					})
				}
				entry["tool_calls"] = apiToolCalls
			}
			conversation = append(conversation, entry)

		case "tool_call":
			// Skip - these are collected and attached to assistant message above
			continue

		case "tool_call_response":
			conversation = append(conversation, map[string]any{
				"role":    "tool_call_output",
				"tool_id": msg.ToolCallID,
				"content": msg.Content,
			})

		case "tool_call_output":
			conversation = append(conversation, map[string]any{
				"role":    "tool_call_output",
				"tool_id": msg.ToolCallID,
				"content": msg.Content,
			})
		}
	}

	return conversation
}

// parseMessageFromMetadata parses a complete message from metadata JSON
func parseMessageFromMetadata(role, metadata string) conversationMessage {
	msg := conversationMessage{Role: role}

	if metadata == "" {
		return msg
	}

	// Try to parse as array of content parts
	var parts []map[string]any
	if err := json.Unmarshal([]byte(metadata), &parts); err != nil {
		return msg
	}

	for _, part := range parts {
		// Extract text content
		if text, ok := part["Text"].(string); ok && text != "" {
			msg.Content = text
		}
		if text, ok := part["text"].(string); ok && text != "" {
			msg.Content = text
		}

		// Extract tool_call metadata (role: tool_call)
		if id, hasID := part["id"].(string); hasID {
			if name, hasName := part["name"].(string); hasName {
				toolCall := ToolCall{
					ID:   id,
					Name: name,
				}
				// Parse input JSON string back to map
				if input, ok := part["input"].(string); ok && input != "" {
					var args map[string]any
					if err := json.Unmarshal([]byte(input), &args); err == nil {
						toolCall.Arguments = args
					}
				}
				msg.ToolCalls = append(msg.ToolCalls, toolCall)
			}
		}

		// Extract tool_call_id and content for tool_call_response
		if toolCallID, ok := part["tool_call_id"].(string); ok {
			msg.ToolCallID = toolCallID
		}
		if content, ok := part["content"].(string); ok {
			msg.Content = content
		}

		// Also handle tool_id for backward compatibility with tool_call_output
		if toolID, ok := part["tool_id"].(string); ok {
			msg.ToolCallID = toolID
		}
	}

	return msg
}

// extractTextFromMetadata extracts text content from message metadata JSON
func extractTextFromMetadata(metadata string) string {
	msg := parseMessageFromMetadata("", metadata)
	return msg.Content
}

// createTextMetadata creates message metadata JSON for text content
func createTextMetadata(text string) string {
	metadata := []map[string]string{
		{"Text": text},
	}
	data, _ := json.Marshal(metadata)
	return string(data)
}

// createToolCallMetadata creates metadata for tool_call message (matches TUI format)
func createToolCallMetadata(tc ToolCall) string {
	argsJSON, _ := json.Marshal(tc.Arguments)
	parts := []map[string]any{
		{
			"id":       tc.ID,
			"name":     tc.Name,
			"input":    string(argsJSON),
			"type":     "function",
			"finished": true,
			"reason":   "",
		},
	}
	data, _ := json.Marshal(parts)
	return string(data)
}

// createToolCallResponseMetadata creates metadata for tool_call_response message (matches TUI format)
func createToolCallResponseMetadata(toolCallID, name, content string) string {
	parts := []map[string]any{
		{
			"tool_call_id": toolCallID,
			"name":         name,
			"content":      content,
			"metadata":     "",
			"is_error":     false,
			"pending":      false,
		},
	}
	data, _ := json.Marshal(parts)
	return string(data)
}

// getDefaultAgent returns the default core agent ID
func getDefaultAgent() (string, error) {
	return coreagent.IDOpperator, nil
}

// getAgentOptions retrieves list of available agents for the agent tool spec
func getAgentOptions() []tools.AgentOption {
	// Load daemon registry to query all enabled daemons
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return nil
	}

	options := make([]tools.AgentOption, 0)
	seen := make(map[string]struct{})

	// Query each enabled daemon
	for _, daemon := range registry.Daemons {
		if !daemon.Enabled {
			continue
		}

		client, err := ipc.NewClientFromRegistry(daemon.Name)
		if err != nil {
			continue
		}

		agents, err := client.ListAgents()
		client.Close()
		if err != nil {
			continue
		}

		// Add agents, avoiding duplicates
		for _, agent := range agents {
			name := strings.TrimSpace(agent.Name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}

			options = append(options, tools.AgentOption{
				Value:       name,
				Label:       name,
				Description: strings.TrimSpace(agent.Description),
			})
		}
	}

	return options
}

// buildAgentListInstructions builds the agent list section to append to system prompt
func buildAgentListInstructions(options []tools.AgentOption) string {
	var b strings.Builder
	b.WriteString("Available managed sub-agents for the `agent` tool (set the `agent` parameter to one of these values):\n")

	if len(options) == 0 {
		b.WriteString("\n(no managed sub-agents detected)")
		return b.String()
	}

	// Add managed agents only (no Builder in CLI)
	first := true
	for _, opt := range options {
		name := strings.TrimSpace(opt.Value)
		if name == "" {
			continue
		}
		desc := strings.TrimSpace(opt.Description)
		if desc == "" {
			desc = "Managed agent"
		}

		if !first {
			b.WriteString("\n\n")
		}
		first = false
		b.WriteString(name)
		b.WriteString("\n")
		b.WriteString(desc)
	}

	return b.String()
}

// CommandDescriptor represents an agent command (simplified from protocol.CommandDescriptor)
type CommandDescriptor struct {
	Name        string
	Title       string
	Description string
	Arguments   []CommandArgument
}

// CommandArgument represents a command parameter
type CommandArgument struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     any
}

// getAgentMetadataAndCommands retrieves agent description, system prompt, and commands
func getAgentMetadataAndCommands(agentName string) (description, systemPrompt string, systemPromptReplace bool, commands []CommandDescriptor, err error) {
	client, foundDaemon, err := getClientForAgent(agentName, "")
	if err != nil {
		return "", "", false, nil, err
	}
	defer client.Close()

	agents, err := client.ListAgents()
	if err != nil {
		return "", "", false, nil, err
	}

	var agentDesc, agentPrompt string
	var promptReplace bool
	for _, agent := range agents {
		if agent.Name == agentName {
			agentDesc = agent.Description
			agentPrompt = agent.SystemPrompt
			promptReplace = agent.SystemPromptReplace
			break
		}
	}

	if agentDesc == "" && agentPrompt == "" {
		return "", "", false, nil, fmt.Errorf("agent not found on daemon %s", foundDaemon)
	}

	// Get agent commands
	cmdDescs, err := client.ListCommands(agentName)
	if err != nil {
		// Non-fatal - just log warning (silently)
		cmdDescs = nil
	}

	// Convert to simplified format
	commands = make([]CommandDescriptor, 0, len(cmdDescs))
	for _, cmd := range cmdDescs {
		args := make([]CommandArgument, 0, len(cmd.Arguments))
		for _, arg := range cmd.Arguments {
			args = append(args, CommandArgument{
				Name:        arg.Name,
				Type:        arg.Type,
				Description: arg.Description,
				Required:    arg.Required,
				Default:     arg.Default,
			})
		}
		commands = append(commands, CommandDescriptor{
			Name:        cmd.Name,
			Title:       cmd.Title,
			Description: cmd.Description,
			Arguments:   args,
		})
	}

	return agentDesc, agentPrompt, promptReplace, commands, nil
}

// commandsToToolSpecs converts agent commands to tool specs
func commandsToToolSpecs(agentName string, commands []CommandDescriptor) []tools.Spec {
	if len(commands) == 0 {
		return nil
	}

	specs := make([]tools.Spec, 0, len(commands))
	for _, cmd := range commands {
		// Build parameters schema
		properties := make(map[string]any)
		required := []string{}

		for _, arg := range cmd.Arguments {
			paramSchema := map[string]any{
				"type": arg.Type,
			}
			if arg.Description != "" {
				paramSchema["description"] = arg.Description
			}
			if arg.Default != nil {
				paramSchema["default"] = arg.Default
			}

			properties[arg.Name] = paramSchema

			if arg.Required {
				required = append(required, arg.Name)
			}
		}

		parameters := map[string]any{
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			parameters["required"] = required
		}

		// Build tool spec
		toolName := fmt.Sprintf("%s__%s", agentName, cmd.Name)
		description := cmd.Description
		if description == "" {
			title := cmd.Title
			if title == "" {
				title = cmd.Name
			}
			description = fmt.Sprintf("Execute %s command on agent %s", title, agentName)
		}

		specs = append(specs, tools.Spec{
			Name:        toolName,
			Description: description,
			Parameters:  parameters,
		})
	}

	return specs
}

// agentOption represents an available agent for context building
type agentOption struct {
	Name        string
	Status      string
	Description string
}

// getAgentListForContext retrieves the list of available agents for context building
func getAgentListForContext() ([]agentOption, error) {
	// Load daemon registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load daemon registry: %w", err)
	}

	// Try to connect to first enabled daemon
	for _, daemon := range registry.Daemons {
		if !daemon.Enabled {
			continue
		}

		client, err := ipc.NewClientWithAuth(daemon.Address, daemon.AuthToken)
		if err != nil {
			continue // Try next daemon
		}

		// List agents
		processes, err := client.ListAgents()
		client.Close()

		if err != nil {
			continue // Try next daemon
		}

		// Successfully got agent list
		options := make([]agentOption, 0, len(processes))
		for _, p := range processes {
			options = append(options, agentOption{
				Name:        p.Name,
				Status:      string(p.Status),
				Description: p.Description,
			})
		}
		return options, nil
	}

	// No daemon available
	return nil, fmt.Errorf("no daemon available")
}

// buildAgentListSection creates the agent list section for system prompt
func buildAgentListSection(options []agentOption, listErr error) string {
	blocks := make([]string, 0, len(options)+1)

	// Add Builder
	builderLabel := "Builder"
	if def, ok := coreagent.Lookup(coreagent.IDBuilder); ok {
		if trimmed := strings.TrimSpace(def.Name); trimmed != "" {
			builderLabel = trimmed
		}
	}
	builderDesc := "Built-in helper agent with access to project tools."
	builderBlock := builderLabel + "\n" + builderDesc + " — running"
	blocks = append(blocks, builderBlock)

	// Add managed agents
	for _, opt := range options {
		desc := strings.TrimSpace(opt.Description)
		status := strings.TrimSpace(opt.Status)
		descriptor := desc
		if status != "" {
			if descriptor != "" {
				descriptor = descriptor + " — " + status
			} else {
				descriptor = status
			}
		}
		block := opt.Name + "\n" + descriptor
		blocks = append(blocks, block)
	}

	var b strings.Builder
	b.WriteString("Available managed sub-agents for the `agent` tool (set the `agent` parameter to one of these values):\n")
	if len(blocks) == 0 {
		if listErr != nil {
			b.WriteString("\n(managed agent list unavailable; see warning below)")
		} else {
			b.WriteString("\n(no managed sub-agents detected)")
		}
	} else {
		for i, block := range blocks {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(block)
		}
	}
	if listErr != nil {
		b.WriteString("\n\nWarning: failed to refresh the managed agent list — ")
		b.WriteString(strings.TrimSpace(listErr.Error()))
		b.WriteString(". Use local tools if unsure.")
	}
	return b.String()
}

// buildInstructions creates the system instructions for the agent
// Matches TUI behavior by providing context about available agents and the current interaction mode
func buildInstructions(agentName, agentPrompt string, agentPromptReplace, isCoreAgent bool) string {
	// Get base prompt
	base := strings.TrimSpace(coreagent.Default().Prompt)

	// Get agent list for context
	agentOptions, agentListErr := getAgentListForContext()
	listSection := buildAgentListSection(agentOptions, agentListErr)

	// If interacting with a managed agent
	trimmedAgent := strings.TrimSpace(agentName)
	if !isCoreAgent && trimmedAgent != "" {
		trimmedPrompt := strings.TrimSpace(agentPrompt)

		// If agent wants full replacement
		if agentPromptReplace && trimmedPrompt != "" {
			return trimmedPrompt
		}

		// Build augmented prompt for managed agent interaction
		var b strings.Builder
		opperatorPrompt := strings.TrimSpace(coreagent.Default().Prompt)
		if opperatorPrompt == "" {
			opperatorPrompt = base
		}
		b.WriteString(opperatorPrompt)

		if listSection != "" {
			b.WriteString("\n\n")
			b.WriteString(listSection)
		}

		b.WriteString("\n\nYou are currently interacting directly with the managed agent '")
		b.WriteString(trimmedAgent)
		b.WriteString("'. Use the available command tools to operate it. If arguments are required, construct valid JSON objects in the tool call.")

		if trimmedPrompt != "" {
			b.WriteString("\n\nSub-agent instructions:\n")
			b.WriteString(trimmedPrompt)
			b.WriteString("\n\nImportant:\nPlace priority on following these sub-agent instructions over any previous instructions.\n")
		}

		return b.String()
	}

	// For core agents (Opperator or Builder)
	// Only show agent list for non-Builder agents (Builder doesn't have the agent tool)
	coreAgentID := ""
	if coreDef, ok := coreagent.Lookup(agentName); ok {
		coreAgentID = coreDef.ID
	}
	isBuilder := strings.EqualFold(strings.TrimSpace(coreAgentID), coreagent.IDBuilder)
	includeAgentList := !isBuilder

	if includeAgentList && listSection != "" {
		var b strings.Builder
		b.WriteString(base)
		b.WriteString("\n\n")
		b.WriteString(listSection)
		return b.String()
	}

	return base
}

// sessionOutputSchema returns the exact same output schema as the TUI
func sessionOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "Assistant response text",
			},
			"tools": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":      map[string]any{"type": "string"},
						"arguments": map[string]any{"type": "object"},
						"reason": map[string]any{
							"type":        "string",
							"description": "Very short tool explaining why the tool was chosen or the reasoning behind using it",
						},
					},
					"required": []string{"name"},
				},
			},
		},
		"required": []string{"text"},
	}
}

// modelIdentifier returns the model identifier (same as TUI default)
func modelIdentifier() any {
	return "gcp/gemini-flash-latest"
}
