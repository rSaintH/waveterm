// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Every agent CLI already stores its own sessions, so history is read from those stores
// rather than kept a second time in Wave, which would drift from what the CLI itself
// resumes. Each one does it differently, so there is a reader per agent.

// maxHistorySessions caps a listing. Reading a remote store is not free and a history panel
// does not need the whole archive.
const maxHistorySessions = 20

// HistorySession is one past conversation.
type HistorySession struct {
	SessionId string `json:"sessionid"`
	Title     string `json:"title,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	// Unix seconds of the last write, for ordering.
	ModTime int64 `json:"modtime,omitempty"`
}

// ListHistory returns past sessions of an agent for a working directory, newest first.
func ListHistory(ctx context.Context, connName string, agentId string, cwd string) ([]HistorySession, error) {
	if cwd == "" {
		return nil, fmt.Errorf("a working directory is required to find its sessions")
	}
	def, err := lookupAgentDef(agentId)
	if err != nil {
		return nil, err
	}
	if !def.HistorySupported {
		return []HistorySession{}, nil
	}
	distro := WslDistroFromConn(connName)
	if distro == "" && !IsLocalConn(connName) {
		return nil, fmt.Errorf("history is not implemented for connection %q", connName)
	}
	switch agentId {
	case "claude":
		return listClaudeHistory(ctx, distro, cwd)
	case "codex":
		return listCodexHistory(ctx, distro, cwd)
	}
	return []HistorySession{}, nil
}

// agentHomeDir resolves an agent's store directory. The CLIs let you move it with an
// environment variable, so that wins over the default under the home directory; hardcoding
// the default would quietly read the wrong store for anyone who moved it.
func agentHomeDir(ctx context.Context, distro string, envVar string, defaultName string) (string, error) {
	if distro != "" {
		// Resolved inside the distro through a login shell, since the variable is usually
		// exported from a shell profile.
		script := fmt.Sprintf(`if [ -n "$%s" ]; then printf '%%s' "$%s"; else printf '%%s' "$HOME/%s"; fi`, envVar, envVar, defaultName)
		out, err := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "-e", "bash", "-lc", script).Output()
		if err != nil {
			return "", fmt.Errorf("cannot resolve %s in %s: %w", envVar, distro, err)
		}
		dir := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
		if dir == "" {
			return "", fmt.Errorf("empty %s in %s", envVar, distro)
		}
		return dir, nil
	}
	if dir := os.Getenv(envVar); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultName), nil
}

// --- Claude Code -----------------------------------------------------------------------
//
// One file per session, <session-id>.jsonl, under <config>/projects/<encoded-cwd>/, with a
// generated title on an "ai-title" line.

// EncodeProjectDir mirrors how Claude Code names a project directory: every path separator,
// drive colon and dot becomes a dash. Verified against the directories on disk.
func EncodeProjectDir(cwd string) string {
	repl := strings.NewReplacer(`\`, "-", "/", "-", ":", "-", ".", "-")
	return repl.Replace(cwd)
}

func listClaudeHistory(ctx context.Context, distro string, cwd string) ([]HistorySession, error) {
	base, err := agentHomeDir(ctx, distro, "CLAUDE_CONFIG_DIR", ".claude")
	if err != nil {
		return nil, err
	}
	sub := "projects/" + EncodeProjectDir(cwd)
	if distro != "" {
		return listClaudeHistoryRemote(ctx, distro, base+"/"+sub)
	}
	return listClaudeHistoryLocal(filepath.Join(base, filepath.FromSlash(sub)))
}

func listClaudeHistoryLocal(dir string) ([]HistorySession, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// No sessions yet is not an error: it is what a new project looks like.
			return []HistorySession{}, nil
		}
		return nil, err
	}
	rtn := []HistorySession{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sess := HistorySession{SessionId: strings.TrimSuffix(e.Name(), ".jsonl")}
		if info, err := e.Info(); err == nil {
			sess.ModTime = info.ModTime().Unix()
		}
		if barr, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
			applyClaudeSummary(&sess, barr)
		}
		rtn = append(rtn, sess)
	}
	sortAndCap(&rtn)
	return rtn, nil
}

func listClaudeHistoryRemote(ctx context.Context, distro string, dir string) ([]HistorySession, error) {
	script := fmt.Sprintf(`cd %q 2>/dev/null || exit 0; for f in *.jsonl; do [ -e "$f" ] || continue; printf '%%s\t%%s\n' "$f" "$(stat -c %%Y "$f" 2>/dev/null)"; done`, dir)
	out, err := runInDistro(ctx, distro, script)
	if err != nil {
		return nil, err
	}
	rtn := []HistorySession{}
	for _, line := range splitLines(out) {
		fields := strings.Split(line, "\t")
		if fields[0] == "" {
			continue
		}
		sess := HistorySession{SessionId: strings.TrimSuffix(fields[0], ".jsonl")}
		if len(fields) > 1 {
			fmt.Sscanf(fields[1], "%d", &sess.ModTime)
		}
		rtn = append(rtn, sess)
	}
	sortAndCap(&rtn)
	for i := range rtn {
		out, err := runInDistro(ctx, distro, fmt.Sprintf("cat %q", dir+"/"+rtn[i].SessionId+".jsonl"))
		if err != nil {
			continue
		}
		applyClaudeSummary(&rtn[i], []byte(out))
	}
	return rtn, nil
}

type claudeLine struct {
	Type string `json:"type"`
	Cwd  string `json:"cwd"`
	// Written as "aiTitle"; verified against a stored transcript.
	AiTitle string `json:"aiTitle"`
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func applyClaudeSummary(sess *HistorySession, barr []byte) {
	for _, line := range splitLines(string(barr)) {
		var cl claudeLine
		if err := json.Unmarshal([]byte(line), &cl); err != nil {
			continue
		}
		if sess.Cwd == "" && cl.Cwd != "" {
			sess.Cwd = cl.Cwd
		}
		if cl.Type == "ai-title" && cl.AiTitle != "" {
			sess.Title = cl.AiTitle
			if sess.Cwd != "" {
				return
			}
		}
		// The first user prompt is the fallback: a session can be stored before a title is
		// generated.
		if sess.Title == "" && cl.Type == "user" && cl.Message != nil {
			sess.Title = firstUserText(cl.Message.Content)
		}
	}
}

// --- Codex -----------------------------------------------------------------------------
//
// Sessions are rollout files under <home>/sessions/<yyyy>/<mm>/<dd>/rollout-<ts>-<id>.jsonl.
// The working directory is only in the first line of each rollout (a "session_meta" record),
// so the listing reads that line; titles come from the separate session_index.jsonl.

type codexMeta struct {
	Type    string `json:"type"`
	Payload *struct {
		SessionId string `json:"session_id"`
		Cwd       string `json:"cwd"`
	} `json:"payload"`
}

type codexIndexLine struct {
	Id         string `json:"id"`
	ThreadName string `json:"thread_name"`
	UpdatedAt  string `json:"updated_at"`
}

func listCodexHistory(ctx context.Context, distro string, cwd string) ([]HistorySession, error) {
	base, err := agentHomeDir(ctx, distro, "CODEX_HOME", ".codex")
	if err != nil {
		return nil, err
	}
	var rtn []HistorySession
	if distro != "" {
		rtn, err = listCodexHistoryRemote(ctx, distro, base, cwd)
	} else {
		rtn, err = listCodexHistoryLocal(base, cwd)
	}
	if err != nil {
		return nil, err
	}
	sortAndCap(&rtn)
	applyCodexTitles(ctx, distro, base, rtn)
	return rtn, nil
}

func listCodexHistoryLocal(base string, cwd string) ([]HistorySession, error) {
	root := filepath.Join(base, "sessions")
	rtn := []HistorySession{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			// A missing or unreadable store is empty history, not a failure.
			return nil
		}
		if !strings.HasPrefix(info.Name(), "rollout-") || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		sess, ok := readCodexMeta(readFirstLineOfFile(path), cwd)
		if !ok {
			return nil
		}
		sess.ModTime = info.ModTime().Unix()
		sess.Title = codexFallbackTitle(info.Name())
		rtn = append(rtn, sess)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return rtn, nil
}

func listCodexHistoryRemote(ctx context.Context, distro string, base string, cwd string) ([]HistorySession, error) {
	// One call for the whole store: a rollout per exec would be far too slow.
	script := fmt.Sprintf(
		`find %q -name 'rollout-*.jsonl' -type f 2>/dev/null | while read -r f; do printf '%%s\t%%s\t%%s\n' "$(stat -c %%Y "$f")" "$f" "$(head -n1 "$f")"; done`,
		base+"/sessions")
	out, err := runInDistro(ctx, distro, script)
	if err != nil {
		return nil, err
	}
	rtn := []HistorySession{}
	for _, line := range splitLines(out) {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		sess, ok := readCodexMeta(fields[2], cwd)
		if !ok {
			continue
		}
		fmt.Sscanf(fields[0], "%d", &sess.ModTime)
		sess.Title = codexFallbackTitle(fields[1])
		rtn = append(rtn, sess)
	}
	return rtn, nil
}

// readCodexMeta accepts a rollout's first line and keeps it only when it belongs to cwd.
func readCodexMeta(firstLine string, cwd string) (HistorySession, bool) {
	if firstLine == "" {
		return HistorySession{}, false
	}
	var meta codexMeta
	if err := json.Unmarshal([]byte(firstLine), &meta); err != nil {
		return HistorySession{}, false
	}
	if meta.Type != "session_meta" || meta.Payload == nil {
		return HistorySession{}, false
	}
	if !samePath(meta.Payload.Cwd, cwd) {
		return HistorySession{}, false
	}
	return HistorySession{SessionId: meta.Payload.SessionId, Cwd: meta.Payload.Cwd}, true
}

// codexFallbackTitle reads the timestamp out of a rollout filename
// (rollout-2026-08-06T14-43-17-<id>.jsonl). Only some sessions have an entry in the index,
// and a bare uuid is a useless label in a list.
func codexFallbackTitle(fileName string) string {
	name := filepath.Base(fileName)
	name = strings.TrimPrefix(name, "rollout-")
	if len(name) < 19 {
		return ""
	}
	stamp := name[:19]
	// 2026-08-06T14-43-17 -> 2026-08-06 14:43:17. Seconds are kept because several
	// sessions in the same minute are common, and the label has to tell them apart.
	if len(stamp) == 19 && stamp[10] == 0x54 {
		return stamp[:10] + " " + strings.ReplaceAll(stamp[11:19], "-", ":")
	}
	return ""
}

// applyCodexTitles fills titles from session_index.jsonl, which maps a session id to the
// name Codex generated for it.
func applyCodexTitles(ctx context.Context, distro string, base string, sessions []HistorySession) {
	if len(sessions) == 0 {
		return
	}
	var content string
	if distro != "" {
		out, err := runInDistro(ctx, distro, fmt.Sprintf("cat %q 2>/dev/null", base+"/session_index.jsonl"))
		if err != nil {
			return
		}
		content = out
	} else {
		barr, err := os.ReadFile(filepath.Join(base, "session_index.jsonl"))
		if err != nil {
			return
		}
		content = string(barr)
	}
	titles := map[string]string{}
	for _, line := range splitLines(content) {
		var il codexIndexLine
		if err := json.Unmarshal([]byte(line), &il); err != nil {
			continue
		}
		if il.Id != "" && il.ThreadName != "" {
			titles[il.Id] = il.ThreadName
		}
	}
	for i := range sessions {
		if t, ok := titles[sessions[i].SessionId]; ok && t != "" {
			// A real name beats the timestamp fallback.
			sessions[i].Title = t
		}
	}
}

// --- shared ------------------------------------------------------------------------------

func runInDistro(ctx context.Context, distro string, script string) (string, error) {
	out, err := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "-e", "bash", "-lc", script).Output()
	if err != nil {
		return "", fmt.Errorf("command failed in %s: %w", distro, err)
	}
	return strings.ReplaceAll(string(out), "\r", ""), nil
}

func splitLines(s string) []string {
	rtn := []string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			rtn = append(rtn, line)
		}
	}
	return rtn
}

func readFirstLineOfFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	// A rollout's metadata line carries the whole base prompt, so it is large; reading the
	// whole file would be much worse.
	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	if n <= 0 {
		return ""
	}
	if idx := strings.IndexByte(string(buf[:n]), '\n'); idx >= 0 {
		return string(buf[:idx])
	}
	return string(buf[:n])
}

// samePath compares working directories the way a user would: separators and case do not
// distinguish a project on Windows.
func samePath(a string, b string) bool {
	norm := func(p string) string {
		p = strings.ReplaceAll(p, `\`, "/")
		p = strings.TrimRight(p, "/")
		return strings.ToLower(p)
	}
	return norm(a) == norm(b)
}

// sortAndCap orders newest first and collapses duplicates. A session can have more than one
// rollout file, which would otherwise list the same conversation several times.
func sortAndCap(list *[]HistorySession) {
	sort.Slice(*list, func(i, j int) bool { return (*list)[i].ModTime > (*list)[j].ModTime })
	seen := map[string]bool{}
	deduped := make([]HistorySession, 0, len(*list))
	for _, s := range *list {
		if s.SessionId == "" || seen[s.SessionId] {
			continue
		}
		seen[s.SessionId] = true
		deduped = append(deduped, s)
	}
	if len(deduped) > maxHistorySessions {
		deduped = deduped[:maxHistorySessions]
	}
	*list = deduped
}

// firstUserText reads a user message body, which is a plain string in most lines and an
// array of blocks in others.
func firstUserText(content json.RawMessage) string {
	var asString string
	if err := json.Unmarshal(content, &asString); err == nil {
		return truncateTitle(asString)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return truncateTitle(b.Text)
			}
		}
	}
	return ""
}

func truncateTitle(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
