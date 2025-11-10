package protocol

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MessageHandler processes incoming messages
type MessageHandler interface {
	HandleMessage(msg *Message) error
}

type MessageHandlerFunc func(msg *Message) error

func (f MessageHandlerFunc) HandleMessage(msg *Message) error {
	return f(msg)
}

// ProcessProtocol manages bidirectional communication with a process
type ProcessProtocol struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	handlers map[MessageType][]MessageHandler
	mu       sync.RWMutex

	responseMu       sync.Mutex
	pendingResponses map[string]*pendingResponse
	registryMu       sync.RWMutex
	registeredCmds   []CommandDescriptor

	doneCh    chan struct{}
	started   atomic.Bool
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup

	rawOutputHandler func(line string)
}

type pendingResponse struct {
	ch       chan *ResponseMessage
	progress func(CommandProgressMessage)
}

func NewProcessProtocol(stdin io.WriteCloser, stdout, stderr io.ReadCloser) *ProcessProtocol {
	return &ProcessProtocol{
		stdin:            stdin,
		stdout:           stdout,
		stderr:           stderr,
		handlers:         make(map[MessageType][]MessageHandler),
		pendingResponses: make(map[string]*pendingResponse),
		doneCh:           make(chan struct{}),
	}
}

// RegisterHandler registers a handler for a specific message type
func (p *ProcessProtocol) RegisterHandler(msgType MessageType, handler MessageHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.handlers[msgType] = append(p.handlers[msgType], handler)
}

// RegisterHandlerFunc registers a handler function for a specific message type
func (p *ProcessProtocol) RegisterHandlerFunc(msgType MessageType, fn func(msg *Message) error) {
	p.RegisterHandler(msgType, MessageHandlerFunc(fn))
}

func (p *ProcessProtocol) SetRawOutputHandler(handler func(line string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rawOutputHandler = handler
}

// Start begins processing messages from stdout
func (p *ProcessProtocol) Start() {
	p.startOnce.Do(func() {
		p.started.Store(true)
		if p.stdout != nil {
			p.wg.Add(1)
			go p.readMessages()
		}
		if p.stderr != nil {
			p.wg.Add(1)
			go p.drainStderr()
		}
		go func() {
			p.wg.Wait()
			close(p.doneCh)
		}()
	})
}

// Stop stops processing messages
func (p *ProcessProtocol) Stop() {
	p.stopOnce.Do(func() {
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		if p.stderr != nil {
			_ = p.stderr.Close()
		}
	})

	if !p.started.Load() {
		return
	}

	select {
	case <-p.doneCh:
		// Clean shutdown
	case <-time.After(2 * time.Second):
		// Timeout - continue anyway
	}
}

// readMessages reads and processes messages from stdout
func (p *ProcessProtocol) readMessages() {
	defer p.wg.Done()

	reader := bufio.NewReader(p.stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			p.handleStdoutLine(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return
			}
			log.Printf("protocol stdout read error: %v", err)
			return
		}
	}
}

// dispatchMessage sends the message to registered handlers
func (p *ProcessProtocol) dispatchMessage(msg *Message) {
	p.mu.RLock()
	handlers := p.handlers[msg.Type]
	p.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler.HandleMessage(msg); err != nil {
			log.Printf("protocol handler error for %s: %v", msg.Type, err)
		}
	}
}

func (p *ProcessProtocol) handleStdoutLine(line []byte) {
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(trimmed) == 0 {
		return
	}

	msg, err := ParseMessage(trimmed)
	if err != nil {
		p.dispatchRaw(string(trimmed))
		return
	}

	p.handleResponse(msg)
	p.handleProgress(msg)
	p.dispatchMessage(msg)
}

func (p *ProcessProtocol) dispatchRaw(line string) {
	p.mu.RLock()
	handler := p.rawOutputHandler
	p.mu.RUnlock()

	if handler != nil {
		handler(line)
	}
}

// SendMessage writes a protocol message to the process stdin
func (p *ProcessProtocol) SendMessage(msg *Message) error {
	if p.stdin == nil {
		return fmt.Errorf("stdin not configured for protocol")
	}

	data, err := msg.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if _, err := p.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// SendLifecycleEvent sends a lifecycle event message to the process
func (p *ProcessProtocol) SendLifecycleEvent(eventType string, data map[string]interface{}) error {
	msg, err := NewMessage(MsgLifecycleEvent, LifecycleEventMessage{
		EventType: eventType,
		Data:      data,
	})
	if err != nil {
		return fmt.Errorf("failed to create lifecycle event message: %w", err)
	}
	return p.SendMessage(msg)
}

// SendCommand sends a command message and waits for the response or context cancellation.
func (p *ProcessProtocol) SendCommand(ctx context.Context, command string, args map[string]interface{}, workingDir string) (*ResponseMessage, error) {
	return p.sendCommand(ctx, command, args, strings.TrimSpace(workingDir), nil)
}

// SendCommandWithProgress sends a command and streams progress callbacks until completion.
func (p *ProcessProtocol) SendCommandWithProgress(ctx context.Context, command string, args map[string]interface{}, workingDir string, progress func(CommandProgressMessage)) (*ResponseMessage, error) {
	return p.sendCommand(ctx, command, args, strings.TrimSpace(workingDir), progress)
}

func (p *ProcessProtocol) sendCommand(ctx context.Context, command string, args map[string]interface{}, workingDir string, progress func(CommandProgressMessage)) (*ResponseMessage, error) {
	if p.stdin == nil {
		return nil, fmt.Errorf("stdin not configured for protocol")
	}

	cmd := CommandMessage{
		Command:    command,
		Args:       args,
		ID:         generateID(),
		WorkingDir: workingDir,
	}

	listener := &pendingResponse{ch: make(chan *ResponseMessage, 1), progress: progress}

	p.responseMu.Lock()
	p.pendingResponses[cmd.ID] = listener
	p.responseMu.Unlock()

	msg, err := NewMessage(MsgCommand, cmd)
	if err != nil {
		p.clearPendingResponse(cmd.ID)
		return nil, err
	}

	if err := p.SendMessage(msg); err != nil {
		p.clearPendingResponse(cmd.ID)
		return nil, err
	}

	select {
	case resp := <-listener.ch:
		return resp, nil
	case <-ctx.Done():
		p.clearPendingResponse(cmd.ID)
		return nil, ctx.Err()
	}
}

func (p *ProcessProtocol) handleResponse(msg *Message) {
	if msg.Type != MsgResponse {
		return
	}

	var data ResponseMessage
	if err := msg.ExtractData(&data); err != nil {
		log.Printf("protocol response decode error: %v", err)
		return
	}

	if data.CommandID != "" {
		p.responseMu.Lock()
		listener, ok := p.pendingResponses[data.CommandID]
		if ok {
			delete(p.pendingResponses, data.CommandID)
		}
		p.responseMu.Unlock()
		if ok {
			listener.ch <- &data
		}
	}
}

func (p *ProcessProtocol) handleProgress(msg *Message) {
	if msg.Type != MsgCommandProgress {
		return
	}

	var data CommandProgressMessage
	if err := msg.ExtractData(&data); err != nil {
		log.Printf("protocol progress decode error: %v", err)
		return
	}

	if strings.TrimSpace(data.CommandID) == "" {
		return
	}

	p.responseMu.Lock()
	listener, ok := p.pendingResponses[data.CommandID]
	p.responseMu.Unlock()

	if ok && listener != nil && listener.progress != nil {
		listener.progress(data)
	}
}

func (p *ProcessProtocol) clearPendingResponse(id string) {
	p.responseMu.Lock()
	if listener, ok := p.pendingResponses[id]; ok {
		delete(p.pendingResponses, id)
		close(listener.ch)
	}
	p.responseMu.Unlock()
}

func (p *ProcessProtocol) drainStderr() {
	defer p.wg.Done()

	reader := bufio.NewReader(p.stderr)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) > 0 {
				p.dispatchRaw(string(trimmed))
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return
			}
			log.Printf("protocol stderr read error: %v", err)
			return
		}
	}
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// DefaultHandlers provides a set of default message handlers
type DefaultHandlers struct {
	OnReady           func(pid int, version string)
	OnLog             func(level LogLevel, message string, fields map[string]interface{})
	OnEvent           func(name string, data map[string]interface{})
	OnLifecycleEvent  func(eventType string, data map[string]interface{})
	OnError           func(err string, code int)
	OnResponse        func(resp *ResponseMessage)
	OnSystemPrompt    func(prompt string, replace bool)
	OnDescription     func(description string)
	OnCommandRegistry       func(commands []CommandDescriptor)
	OnCommandProgress       func(progress CommandProgressMessage)
	OnSidebarSection        func(section SidebarSectionMessage)
	OnSidebarSectionRemoval func(sectionID string)
}

// RegisterDefaults registers the default handlers
func (p *ProcessProtocol) RegisterDefaults(handlers *DefaultHandlers) {
	if handlers.OnReady != nil {
		p.RegisterHandlerFunc(MsgReady, func(msg *Message) error {
			var data ReadyMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnReady(data.PID, data.Version)
			return nil
		})
	}

	if handlers.OnLog != nil {
		p.RegisterHandlerFunc(MsgLog, func(msg *Message) error {
			var data LogMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnLog(data.Level, data.Message, data.Fields)
			return nil
		})
	}

	if handlers.OnEvent != nil {
		p.RegisterHandlerFunc(MsgEvent, func(msg *Message) error {
			var data EventMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnEvent(data.Name, data.Data)
			return nil
		})
	}

	if handlers.OnLifecycleEvent != nil {
		p.RegisterHandlerFunc(MsgLifecycleEvent, func(msg *Message) error {
			var data LifecycleEventMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnLifecycleEvent(data.EventType, data.Data)
			return nil
		})
	}

	if handlers.OnError != nil {
		p.RegisterHandlerFunc(MsgError, func(msg *Message) error {
			var data ErrorMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnError(data.Error, data.Code)
			return nil
		})
	}

	if handlers.OnResponse != nil {
		p.RegisterHandlerFunc(MsgResponse, func(msg *Message) error {
			var data ResponseMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnResponse(&data)
			return nil
		})
	}

	if handlers.OnCommandProgress != nil {
		p.RegisterHandlerFunc(MsgCommandProgress, func(msg *Message) error {
			var data CommandProgressMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnCommandProgress(data)
			return nil
		})
	}

	if handlers.OnSystemPrompt != nil {
		p.RegisterHandlerFunc(MsgSystemPrompt, func(msg *Message) error {
			var data SystemPromptMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnSystemPrompt(data.Prompt, data.Replace)
			return nil
		})
	}

	if handlers.OnDescription != nil {
		p.RegisterHandlerFunc(MsgAgentDescription, func(msg *Message) error {
			var data AgentDescriptionMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnDescription(data.Description)
			return nil
		})
	}

	if handlers.OnSidebarSection != nil {
		p.RegisterHandlerFunc(MsgSidebarSection, func(msg *Message) error {
			var data SidebarSectionMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnSidebarSection(data)
			return nil
		})
	}

	if handlers.OnSidebarSectionRemoval != nil {
		p.RegisterHandlerFunc(MsgSidebarSectionRemoval, func(msg *Message) error {
			var data SidebarSectionRemovalMessage
			if err := msg.ExtractData(&data); err != nil {
				return err
			}
			handlers.OnSidebarSectionRemoval(data.SectionID)
			return nil
		})
	}

	p.RegisterHandlerFunc(MsgCommandRegistry, func(msg *Message) error {
		var data CommandRegistryMessage
		if err := msg.ExtractData(&data); err != nil {
			return err
		}

		defs := NormalizeCommandDescriptors(data.Commands)

		p.registryMu.Lock()
		p.registeredCmds = defs
		p.registryMu.Unlock()
		if handlers != nil && handlers.OnCommandRegistry != nil {
			out := make([]CommandDescriptor, len(defs))
			copy(out, defs)
			handlers.OnCommandRegistry(out)
		}
		return nil
	})
}

func (p *ProcessProtocol) RegisteredCommands() []CommandDescriptor {
	p.registryMu.RLock()
	defer p.registryMu.RUnlock()

	cmds := make([]CommandDescriptor, len(p.registeredCmds))
	copy(cmds, p.registeredCmds)
	return cmds
}
