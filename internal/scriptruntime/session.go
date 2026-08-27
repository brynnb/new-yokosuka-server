package scriptruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

const (
	maxRuntimeEventBytes = 1024 * 1024
	maxRuntimeStderr     = 64 * 1024
	processStopTimeout   = 2 * time.Second
)

type Bridge struct {
	executable string
	registry   scriptcontent.CommandRegistry
}

func NewBridge(executable string) (*Bridge, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, errors.New("Yarn runtime executable is required")
	}
	registry, err := scriptcontent.Registry()
	if err != nil {
		return nil, err
	}
	return &Bridge{executable: executable, registry: registry}, nil
}

type Session struct {
	command      *exec.Cmd
	stdin        io.WriteCloser
	scanner      *bufio.Scanner
	wait         <-chan error
	stderr       *lockedBuffer
	registry     scriptcontent.CommandRegistry
	sequence     int
	awaiting     string
	queryID      int
	options      map[int]bool
	terminal     bool
	exchangeLock sync.Mutex
	closeOnce    sync.Once
}

func (b *Bridge) Start(request StartRequest) (*Session, error) {
	if len(request.Program) == 0 || strings.TrimSpace(request.StartNode) == "" {
		return nil, errors.New("compiled program and start node are required")
	}
	message := startMessage{
		ProtocolVersion: ProtocolVersion,
		ProgramBase64:   base64.StdEncoding.EncodeToString(request.Program),
		StartNode:       strings.TrimSpace(request.StartNode),
		Variables:       request.Variables,
	}
	for _, entry := range b.registry.Entries {
		if entry.Kind != "function" {
			continue
		}
		definition := functionDefinition{Name: entry.Name, ReturnType: entry.ReturnType}
		for _, parameter := range entry.Parameters {
			definition.ParameterTypes = append(definition.ParameterTypes, parameter.Type)
		}
		message.Functions = append(message.Functions, definition)
	}
	command := exec.Command(b.executable, "--runtime")
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Yarn runtime input: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open Yarn runtime output: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open Yarn runtime diagnostics: %w", err)
	}
	if err := command.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start Yarn runtime: %w", err)
	}
	stderrBuffer := &lockedBuffer{limit: maxRuntimeStderr}
	go func() { _, _ = io.Copy(stderrBuffer, stderr) }()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxRuntimeEventBytes)
	session := &Session{
		command: command, stdin: stdin, scanner: scanner, wait: wait,
		stderr: stderrBuffer, registry: b.registry,
	}
	if err := session.write(message); err != nil {
		session.forceStop()
		return nil, err
	}
	return session, nil
}

func (s *Session) Exchange(ctx context.Context, input *Input) (Event, error) {
	s.exchangeLock.Lock()
	defer s.exchangeLock.Unlock()
	if s.terminal {
		return Event{}, errors.New("Yarn runtime session has ended")
	}
	if err := s.validateInput(input); err != nil {
		return Event{}, err
	}
	if input != nil {
		if err := s.write(input); err != nil {
			s.forceStop()
			return Event{}, err
		}
	}
	type scanResult struct {
		event Event
		err   error
	}
	result := make(chan scanResult, 1)
	go func() {
		if !s.scanner.Scan() {
			err := s.scanner.Err()
			if err == nil {
				err = fmt.Errorf("Yarn runtime stopped: %s", s.stderr.String())
			}
			result <- scanResult{err: err}
			return
		}
		var event Event
		if err := json.Unmarshal(s.scanner.Bytes(), &event); err != nil {
			result <- scanResult{err: fmt.Errorf("decode Yarn runtime event: %w", err)}
			return
		}
		result <- scanResult{event: event}
	}()
	select {
	case <-ctx.Done():
		s.forceStop()
		return Event{}, ctx.Err()
	case scanned := <-result:
		if scanned.err != nil {
			s.forceStop()
			return Event{}, scanned.err
		}
		if err := s.validateEvent(scanned.event); err != nil {
			s.forceStop()
			return Event{}, err
		}
		return scanned.event, nil
	}
}

func (s *Session) validateInput(input *Input) error {
	if s.awaiting == "" {
		if input != nil {
			return errors.New("Yarn runtime did not request controller input")
		}
		return nil
	}
	if input == nil {
		return fmt.Errorf("Yarn runtime is waiting for %s", s.awaiting)
	}
	if input.Type == "cancel" {
		return nil
	}
	switch s.awaiting {
	case "continue":
		if input.Type != "continue" {
			return errors.New("Yarn line or command requires continue")
		}
	case "queryResult":
		if input.Type != "queryResult" || input.QueryID == nil || *input.QueryID != s.queryID || input.Value == nil {
			return fmt.Errorf("Yarn query %d requires its typed result", s.queryID)
		}
	case "select":
		if input.Type != "select" || input.OptionID == nil || !s.options[*input.OptionID] {
			return errors.New("Yarn options require an available option ID")
		}
	default:
		return errors.New("invalid Yarn runtime controller state")
	}
	s.awaiting, s.options = "", nil
	return nil
}

func (s *Session) validateEvent(event Event) error {
	if event.Type == "error" {
		s.terminal = true
		return fmt.Errorf("Yarn runtime error: %s", event.Message)
	}
	if event.Sequence != s.sequence+1 {
		return fmt.Errorf("Yarn runtime sequence %d followed %d", event.Sequence, s.sequence)
	}
	s.sequence = event.Sequence
	s.awaiting, s.queryID, s.options = "", 0, nil
	switch event.Type {
	case "nodeStart", "nodeComplete":
		if strings.TrimSpace(event.Node) == "" {
			return errors.New("Yarn runtime emitted an empty node name")
		}
	case "line":
		if strings.TrimSpace(event.LineID) == "" {
			return errors.New("Yarn runtime emitted an empty line ID")
		}
		s.awaiting = "continue"
	case "command", "query":
		kind := "command"
		if event.Type == "query" {
			kind = "function"
			if event.QueryID == nil || *event.QueryID <= 0 {
				return errors.New("Yarn runtime emitted an invalid query ID")
			}
			s.queryID = *event.QueryID
			s.awaiting = "queryResult"
		} else {
			s.awaiting = "continue"
		}
		_, diagnostics := scriptcontent.AnalyzeCalls([]scriptcontent.CompiledCall{{
			Kind: kind, Name: event.Name, Arguments: event.Arguments,
		}}, s.registry)
		if len(diagnostics) > 0 {
			return fmt.Errorf("invalid Yarn runtime %s: %s", kind, diagnostics[0].Message)
		}
	case "options":
		if len(event.Options) == 0 {
			return errors.New("Yarn runtime emitted no options")
		}
		s.options = make(map[int]bool, len(event.Options))
		for _, option := range event.Options {
			if option.LineID == "" {
				return errors.New("Yarn runtime emitted an option without a line ID")
			}
			if option.IsAvailable {
				s.options[option.ID] = true
			}
		}
		s.awaiting = "select"
	case "complete", "cancelled":
		s.terminal = true
	default:
		return fmt.Errorf("unknown Yarn runtime event %q", event.Type)
	}
	return nil
}

func (s *Session) write(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := s.stdin.Write(encoded); err != nil {
		return fmt.Errorf("write Yarn runtime message: %w", err)
	}
	return nil
}

func (s *Session) Close() error {
	var result error
	s.closeOnce.Do(func() {
		if !s.terminal {
			s.forceStop()
		}
		_ = s.stdin.Close()
		select {
		case err := <-s.wait:
			if err != nil && !s.terminal {
				result = err
			}
		case <-time.After(processStopTimeout):
			s.forceStop()
			result = errors.New("Yarn runtime did not stop")
		}
	})
	return result
}

func (s *Session) forceStop() {
	if s.command.Process != nil {
		_ = s.command.Process.Kill()
	}
	s.terminal = true
}

type lockedBuffer struct {
	mu    sync.Mutex
	value bytes.Buffer
	limit int
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(value)
	remaining := b.limit - b.value.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.value.Write(value)
	}
	return original, nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.value.String())
}
