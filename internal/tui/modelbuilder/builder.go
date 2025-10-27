package modelbuilder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tui/internal/conversation"
	"tui/internal/inputhistory"
	"tui/internal/message"
	"tui/internal/plan"
	"tui/internal/preferences"
	"tui/internal/pubsub"
	llm "tui/llm"
	"tui/lsp"
	"tui/permission"
	"tui/secretprompt"
	tooling "tui/tools"
)

// Builder assembles dependencies required to construct the TUI model.
type Builder struct {
	workingDir   string
	allowedTools []string
}

// Dependencies captures the services needed by the model constructor.
type Dependencies struct {
	ConversationStore *conversation.Store
	MessageStore      message.Service
	InputStore        inputhistory.Service
	PreferencesStore  *preferences.Store
	PlanStore         *plan.Store
	SessionID         string
	WorkingDir        string
	InvocationDir     string

	PermissionService     permission.Service
	PermissionRequestCh   <-chan pubsub.Event[permission.PermissionRequest]
	PermissionNotifCh     <-chan pubsub.Event[permission.PermissionNotification]
	PermissionRequestStop context.CancelFunc
	PermissionNotifStop   context.CancelFunc

	SecretPromptService secretprompt.Service
	SecretPromptCh      <-chan pubsub.Event[secretprompt.PromptRequest]
	SecretPromptStop    context.CancelFunc

	FocusAgentCh   <-chan pubsub.Event[tooling.FocusAgentEvent]
	FocusAgentStop context.CancelFunc

	PlanCh   <-chan pubsub.Event[tooling.PlanEvent]
	PlanStop context.CancelFunc

	LSPManager *lsp.Manager
	LLMEngine  *llm.Engine
}

func New() *Builder {
	return &Builder{
		allowedTools: []string{
			"list_agents",
			"start_agent",
			"stop_agent",
			"get_logs",
			"view",
			"ls",
			"glob",
			"grep",
			"rg",
			"diagnostics",
		},
	}
}

// WorkingDir overrides the default working directory detection.
func (b *Builder) WorkingDir(dir string) *Builder {
	b.workingDir = strings.TrimSpace(dir)
	return b
}

// AllowedTools overrides the default tool allow-list for the permission service.
func (b *Builder) AllowedTools(tools []string) *Builder {
	b.allowedTools = append([]string(nil), tools...)
	return b
}

func (b *Builder) Build() (*Dependencies, error) {
	convStore, err := conversation.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open conversation store: %w", err)
	}

	msgStore := message.NewSQLiteService(convStore.DB())
	inputStore := inputhistory.NewSQLiteService(convStore.DB())
	planStore := plan.NewStore(convStore.DB())

	prefsStore, err := preferences.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open preferences store: %w", err)
	}

	sessionID, err := getOrCreateInitialSession(convStore)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session: %w", err)
	}

	workingDir := b.workingDir
	if workingDir == "" {
		workingDir = defaultWorkingDir()
	}
	invocationDir := detectInvocationDir()
	if invocationDir == "" {
		invocationDir = workingDir
	}

	permSvc := permission.NewService(workingDir, false, b.allowedTools)
	secretSvc := secretprompt.NewService()

	debugLSP := strings.EqualFold(os.Getenv("OPPERATOR_DEBUG_LSP"), "1") ||
		strings.EqualFold(os.Getenv("OPPERATOR_DEBUG_LSP"), "true")
	lspManager := lsp.NewManager(workingDir, lsp.DefaultConfigs(), debugLSP)

	reqCtx, reqCancel := context.WithCancel(context.Background())
	notifCtx, notifCancel := context.WithCancel(context.Background())

	promptCtx, promptCancel := context.WithCancel(context.Background())

	focusAgentCtx, focusAgentCancel := context.WithCancel(context.Background())

	planCtx, planCancel := context.WithCancel(context.Background())

	// Set plan context for database operations
	tooling.SetPlanContext(planStore, sessionID)

	deps := &Dependencies{
		ConversationStore:     convStore,
		MessageStore:          msgStore,
		InputStore:            inputStore,
		PreferencesStore:      prefsStore,
		PlanStore:             planStore,
		SessionID:             sessionID,
		WorkingDir:            workingDir,
		InvocationDir:         invocationDir,
		PermissionService:     permSvc,
		PermissionRequestCh:   permSvc.Subscribe(reqCtx),
		PermissionNotifCh:     permSvc.SubscribeNotifications(notifCtx),
		PermissionRequestStop: reqCancel,
		PermissionNotifStop:   notifCancel,
		SecretPromptService:   secretSvc,
		SecretPromptCh:        secretSvc.Subscribe(promptCtx),
		SecretPromptStop:      promptCancel,
		FocusAgentCh:          tooling.SubscribeFocusAgentEvents(focusAgentCtx),
		FocusAgentStop:        focusAgentCancel,
		PlanCh:                tooling.SubscribePlanEvents(planCtx),
		PlanStop:              planCancel,
		LSPManager:            lspManager,
		LLMEngine:             llm.NewEngine(permSvc, secretSvc, workingDir, invocationDir, lspManager),
	}

	return deps, nil
}

func defaultWorkingDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	dir := filepath.Join(home, ".config", "opperator")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

func detectInvocationDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return cwd
	}
	return abs
}

func getOrCreateInitialSession(convStore *conversation.Store) (string, error) {
	convs, err := convStore.List(context.Background())
	if err != nil {
		return "", err
	}

	if len(convs) == 0 {
		c, err := convStore.Create(context.Background(), "")
		return c.ID, err
	}

	return convs[0].ID, nil
}
