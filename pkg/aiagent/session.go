// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"

	"github.com/wavetermdev/waveterm/pkg/panichandler"
)

// Stdout lines can be large: the init message alone carries the whole tool and command list.
const maxLineBytes = 4 * 1024 * 1024

// eventBufSize is generous on purpose. A slow consumer must not stall the agent's stdout,
// which would deadlock the process behind a full pipe.
const eventBufSize = 256

// Session is one running agent process.
type Session struct {
	Id         string
	AgentId    string
	Connection string
	Cwd        string

	lock     sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	events   chan AgentEvent
	done     chan struct{}
	closed   bool
	exitErr  error
	lastCost float64
}

// BuildExecCommand turns session options into the process to run. A wsl:// connection is
// wrapped by wsl.exe with --cd, because the cwd is a path inside the distro and setting it on
// the Windows-side process would be meaningless.
func BuildExecCommand(ctx context.Context, opts SessionOpts) (*exec.Cmd, error) {
	args, err := BuildArgs(opts)
	if err != nil {
		return nil, err
	}
	def, err := lookupAgentDef(opts.AgentId)
	if err != nil {
		return nil, err
	}
	distro := WslDistroFromConn(opts.Connection)
	if distro != "" {
		wslArgs := []string{}
		if opts.Cwd != "" {
			wslArgs = append(wslArgs, "--cd", opts.Cwd)
		}
		wslArgs = append(wslArgs, "-d", distro, "-e", def.Bin)
		wslArgs = append(wslArgs, args...)
		return exec.CommandContext(ctx, "wsl.exe", wslArgs...), nil
	}
	if !IsLocalConn(opts.Connection) {
		return nil, fmt.Errorf("agent sessions are not implemented for connection %q", opts.Connection)
	}
	cmd := exec.CommandContext(ctx, def.Bin, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	return cmd, nil
}

// StartSession spawns the agent and streams its protocol output. The returned Session owns
// the process; call Close to stop it.
func StartSession(ctx context.Context, id string, opts SessionOpts) (*Session, error) {
	cmd, err := BuildExecCommand(ctx, opts)
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot pipe agent stdout: %w", err)
	}
	// Kept separate from the protocol stream: a crash or a usage message shows up here and
	// would otherwise be invisible.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot pipe agent stderr: %w", err)
	}
	var stdin io.WriteCloser
	if opts.Interactive {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("cannot pipe agent stdin: %w", err)
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start %s: %w", opts.AgentId, err)
	}
	sess := &Session{
		Id:         id,
		AgentId:    opts.AgentId,
		Connection: opts.Connection,
		Cwd:        opts.Cwd,
		cmd:        cmd,
		stdin:      stdin,
		events:     make(chan AgentEvent, eventBufSize),
		done:       make(chan struct{}),
	}
	go sess.readStdout(stdout)
	go sess.drainStderr(stderr)
	return sess, nil
}

// Events is the protocol stream. It closes when the process exits.
func (s *Session) Events() <-chan AgentEvent {
	return s.events
}

// Done closes once the process has exited and all events have been delivered.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// ExitErr is the process result, valid after Done is closed.
func (s *Session) ExitErr() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.exitErr
}

// TotalCostUSD is the cost reported by the last result line. Worth surfacing: an agent
// session can get expensive without anything looking wrong.
func (s *Session) TotalCostUSD() float64 {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.lastCost
}

func (s *Session) readStdout(stdout io.ReadCloser) {
	defer func() {
		panichandler.PanicHandler("aiagent:readStdout", recover())
	}()
	defer close(s.done)
	defer close(s.events)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		ev, err := ParseStreamJSONLine(line)
		if err != nil {
			// Not fatal: the CLI can print a non-protocol line. Keep Raw as well as the
			// text: dropping it made an unparseable line invisible while debugging.
			ev = AgentEvent{Kind: EventKind_Other, Text: string(line), Raw: append([]byte(nil), line...)}
		}
		if ev.CostUSD > 0 {
			s.lock.Lock()
			s.lastCost = ev.CostUSD
			s.lock.Unlock()
		}
		s.events <- ev
	}
	if err := scanner.Err(); err != nil {
		log.Printf("aiagent session %s stdout error: %v\n", s.Id, err)
	}
	err := s.cmd.Wait()
	s.lock.Lock()
	s.exitErr = err
	s.lock.Unlock()
}

func (s *Session) drainStderr(stderr io.ReadCloser) {
	defer func() {
		panichandler.PanicHandler("aiagent:drainStderr", recover())
	}()
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 8*1024), maxLineBytes)
	for scanner.Scan() {
		log.Printf("aiagent session %s stderr: %s\n", s.Id, scanner.Text())
	}
}

// Send writes a prompt to a running interactive session.
func (s *Session) Send(text string) error {
	s.lock.Lock()
	stdin := s.stdin
	closed := s.closed
	s.lock.Unlock()
	if closed {
		return fmt.Errorf("session %s is closed", s.Id)
	}
	if stdin == nil {
		return fmt.Errorf("session %s is not interactive", s.Id)
	}
	line, err := EncodeUserPrompt(text)
	if err != nil {
		return err
	}
	if _, err := stdin.Write(line); err != nil {
		return fmt.Errorf("cannot write to agent stdin: %w", err)
	}
	return nil
}

// writeControl sends a control line to a running session.
func (s *Session) writeControl(line []byte) error {
	s.lock.Lock()
	stdin := s.stdin
	closed := s.closed
	s.lock.Unlock()
	if closed {
		return fmt.Errorf("session %s is closed", s.Id)
	}
	if stdin == nil {
		return fmt.Errorf("session %s is not interactive", s.Id)
	}
	if _, err := stdin.Write(line); err != nil {
		return fmt.Errorf("cannot write to agent stdin: %w", err)
	}
	return nil
}

// Interrupt cancels the current turn without ending the session, so the conversation and
// its cost so far are kept. Killing the process would lose the ability to continue.
func (s *Session) Interrupt(requestId string) error {
	line, err := EncodeInterrupt(requestId)
	if err != nil {
		return err
	}
	return s.writeControl(line)
}

// SetPermissionMode changes the mode of the running session, so going from asking to
// automatic does not require a restart.
func (s *Session) SetPermissionMode(requestId string, mode string) error {
	line, err := EncodeSetPermissionMode(requestId, mode)
	if err != nil {
		return err
	}
	return s.writeControl(line)
}

// RespondToTool answers a can_use_tool request. Without an answer the agent treats the tool
// as denied, so leaving this unwired silently blocks work.
func (s *Session) RespondToTool(requestId string, allow bool, denyMessage string) error {
	line, err := EncodeToolDecision(requestId, allow, denyMessage)
	if err != nil {
		return err
	}
	return s.writeControl(line)
}

// Close stops the process. Safe to call more than once.
func (s *Session) Close() {
	s.lock.Lock()
	if s.closed {
		s.lock.Unlock()
		return
	}
	s.closed = true
	stdin := s.stdin
	cmd := s.cmd
	s.lock.Unlock()
	if stdin != nil {
		// Closing stdin lets the agent finish on its own before we resort to killing it.
		stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}

// SessionManager keeps the running sessions for the app.
type SessionManager struct {
	lock     sync.Mutex
	sessions map[string]*Session
}

func MakeSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

func (m *SessionManager) Add(sess *Session) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.sessions[sess.Id] = sess
}

func (m *SessionManager) Get(id string) *Session {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.sessions[id]
}

func (m *SessionManager) Remove(id string) {
	m.lock.Lock()
	sess := m.sessions[id]
	delete(m.sessions, id)
	m.lock.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// Ids lists the running sessions, for cleanup and diagnostics.
func (m *SessionManager) Ids() []string {
	m.lock.Lock()
	defer m.lock.Unlock()
	rtn := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		rtn = append(rtn, id)
	}
	return rtn
}
