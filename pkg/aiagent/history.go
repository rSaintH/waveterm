// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
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
		out, err := runInDistro(ctx, distro, script, true)
		if err != nil {
			return "", fmt.Errorf("cannot resolve %s in %s: %w", envVar, distro, err)
		}
		dir := strings.TrimSpace(out)
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

// claudeProjectDirMaxLen mirrors the CLI's cap on an encoded directory name: anything longer
// is truncated and suffixed with a hash of the raw path.
const claudeProjectDirMaxLen = 200

// claudeSummaryHeadBytes bounds how much of a remote transcript is fetched per session when
// looking for its title. A titled session has the title within the first few lines.
const claudeSummaryHeadBytes = 262144

// EncodeProjectDir mirrors how Claude Code names a project directory, verified against the
// implementation bundled in claude 2.1.241: every character outside [a-zA-Z0-9] becomes a
// dash — counted per UTF-16 code unit, like the original JavaScript — and a name past 200
// characters is truncated with a hash suffix so distinct long paths stay distinct.
func EncodeProjectDir(cwd string) string {
	var sb strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r > 0xFFFF {
			// An astral character is two UTF-16 code units in JavaScript, so two dashes.
			sb.WriteString("--")
		} else {
			sb.WriteByte('-')
		}
	}
	enc := sb.String()
	if len(enc) <= claudeProjectDirMaxLen {
		return enc
	}
	return enc[:claudeProjectDirMaxLen] + "-" + claudePathHash(cwd)
}

// claudePathHash reproduces the CLI's Math.abs(((h<<5)-h+code)|0).toString(36) rolling hash
// over UTF-16 code units.
func claudePathHash(s string) string {
	var h int32
	for _, u := range utf16.Encode([]rune(s)) {
		h = h*31 + int32(u)
	}
	v := int64(h)
	if v < 0 {
		v = -v
	}
	return strconv.FormatInt(v, 36)
}

// claudeProjectDirCandidates returns the store directories a project may live under. The CLI
// encodes the working directory exactly as the process saw it, so on Windows the same project
// gets one store directory per drive-letter casing it was ever launched with.
func claudeProjectDirCandidates(cwd string) []string {
	rtn := []string{EncodeProjectDir(cwd)}
	if len(cwd) >= 2 && cwd[1] == ':' {
		c := cwd[0]
		var flipped byte
		if c >= 'a' && c <= 'z' {
			flipped = c - 'a' + 'A'
		} else if c >= 'A' && c <= 'Z' {
			flipped = c - 'A' + 'a'
		}
		if flipped != 0 {
			if alt := EncodeProjectDir(string(flipped) + cwd[1:]); alt != rtn[0] {
				rtn = append(rtn, alt)
			}
		}
	}
	return rtn
}

func listClaudeHistory(ctx context.Context, distro string, cwd string) ([]HistorySession, error) {
	base, err := agentHomeDir(ctx, distro, "CLAUDE_CONFIG_DIR", ".claude")
	if err != nil {
		return nil, err
	}
	rtn := []HistorySession{}
	for _, enc := range claudeProjectDirCandidates(cwd) {
		var list []HistorySession
		if distro != "" {
			list, err = listClaudeHistoryRemote(ctx, distro, base+"/projects/"+enc)
		} else {
			list, err = listClaudeHistoryLocal(filepath.Join(base, "projects", enc))
		}
		if err != nil {
			return nil, err
		}
		rtn = append(rtn, list...)
	}
	sortAndCap(&rtn)
	return rtn, nil
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
		rtn = append(rtn, sess)
	}
	// Sort and cap before opening anything: titles are only read for the sessions that stay
	// in the listing. Reading every transcript first meant reading the whole store (tens of
	// megabytes) on each refresh.
	sortAndCap(&rtn)
	for i := range rtn {
		if f, err := os.Open(filepath.Join(dir, rtn[i].SessionId+".jsonl")); err == nil {
			applyClaudeSummary(&rtn[i], f)
			f.Close()
		}
	}
	return rtn, nil
}

func listClaudeHistoryRemote(ctx context.Context, distro string, dir string) ([]HistorySession, error) {
	script := fmt.Sprintf(`cd %q 2>/dev/null || exit 0; for f in *.jsonl; do [ -e "$f" ] || continue; printf '%%s\t%%s\n' "$f" "$(stat -c %%Y "$f" 2>/dev/null)"; done`, dir)
	out, err := runInDistro(ctx, distro, script, false)
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
	if len(rtn) == 0 {
		return rtn, nil
	}
	// One exec fetches a bounded head of every capped session instead of catting whole
	// transcripts one exec at a time, which blew well past the rpc timeout on real stores.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("cd %q 2>/dev/null || exit 0; ", dir))
	for _, s := range rtn {
		sb.WriteString(fmt.Sprintf(`printf '\n===WAVEFILE:%%s\n' %q; head -c %d -- %q; `,
			s.SessionId, claudeSummaryHeadBytes, s.SessionId+".jsonl"))
	}
	out, err = runInDistro(ctx, distro, sb.String(), false)
	if err != nil {
		// A listing without titles still beats an error.
		return rtn, nil
	}
	byId := map[string]*HistorySession{}
	for i := range rtn {
		byId[rtn[i].SessionId] = &rtn[i]
	}
	sections := strings.Split(out, "\n===WAVEFILE:")
	for _, sec := range sections[1:] {
		nl := strings.IndexByte(sec, '\n')
		if nl < 0 {
			continue
		}
		if sess := byId[sec[:nl]]; sess != nil {
			applyClaudeSummary(sess, strings.NewReader(sec[nl+1:]))
		}
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

// applyClaudeSummary streams transcript lines looking for the generated title, the recorded
// working directory and a first-prompt fallback. It stops reading at the first generated
// title, which for a titled session sits within the first few lines — the transcript itself
// can be tens of megabytes.
func applyClaudeSummary(sess *HistorySession, r io.Reader) {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, readErr := br.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" && claudeLineOfInterest(sess, line) && applyClaudeLine(sess, line) {
			return
		}
		if readErr != nil {
			return
		}
	}
}

// claudeLineOfInterest is a cheap substring check so multi-megabyte tool-result lines are not
// JSON-parsed just to be discarded.
func claudeLineOfInterest(sess *HistorySession, line string) bool {
	if strings.Contains(line, `"ai-title"`) {
		return true
	}
	if sess.Cwd == "" && strings.Contains(line, `"cwd"`) {
		return true
	}
	return sess.Title == "" && strings.Contains(line, `"type":"user"`)
}

// applyClaudeLine folds one transcript line into the summary and reports whether nothing
// further can improve it.
func applyClaudeLine(sess *HistorySession, line string) bool {
	var cl claudeLine
	if err := json.Unmarshal([]byte(line), &cl); err != nil {
		return false
	}
	if sess.Cwd == "" && cl.Cwd != "" {
		sess.Cwd = cl.Cwd
	}
	if cl.Type == "ai-title" && cl.AiTitle != "" {
		sess.Title = cl.AiTitle
		return sess.Cwd != ""
	}
	// The first user prompt is the fallback: a session can be stored before a title is
	// generated.
	if sess.Title == "" && cl.Type == "user" && cl.Message != nil {
		sess.Title = firstUserText(cl.Message.Content)
	}
	return false
}

// --- Codex -----------------------------------------------------------------------------
//
// Sessions are rollout files under <home>/sessions/<yyyy>/<mm>/<dd>/rollout-<ts>-<id>.jsonl.
// The working directory is only in the first line of each rollout (a "session_meta" record),
// so the listing reads that line; titles come from the separate session_index.jsonl.

type codexMetaPayload struct {
	SessionId string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

type codexMeta struct {
	Type    string            `json:"type"`
	Payload *codexMetaPayload `json:"payload"`
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

// codexHeadBytes bounds how much of a rollout is read to find its metadata. The metadata
// line carries the whole base prompt, but the identifying fields sit in front of it.
const codexHeadBytes = 8192

func listCodexHistoryLocal(base string, cwd string) ([]HistorySession, error) {
	root := filepath.Join(base, "sessions")
	type rolloutFile struct {
		path    string
		name    string
		modTime int64
	}
	files := []rolloutFile{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			// A missing or unreadable store is empty history, not a failure.
			return nil
		}
		if !strings.HasPrefix(info.Name(), "rollout-") || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}
		files = append(files, rolloutFile{path: path, name: info.Name(), modTime: info.ModTime().Unix()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// The store holds every project's rollouts and each metadata read costs a file open, so
	// scan newest first and stop at the cap instead of opening the whole archive.
	sort.Slice(files, func(i, j int) bool { return files[i].modTime > files[j].modTime })
	rtn := []HistorySession{}
	seen := map[string]bool{}
	for _, rf := range files {
		if len(rtn) >= maxHistorySessions {
			break
		}
		sess, ok := readCodexMeta(readFileHead(rf.path, codexHeadBytes), cwd)
		if !ok || sess.SessionId == "" || seen[sess.SessionId] {
			continue
		}
		seen[sess.SessionId] = true
		sess.ModTime = rf.modTime
		sess.Title = codexFallbackTitle(rf.name)
		rtn = append(rtn, sess)
	}
	return rtn, nil
}

func listCodexHistoryRemote(ctx context.Context, distro string, base string, cwd string) ([]HistorySession, error) {
	// One call for the whole store: a rollout per exec would be far too slow.
	script := fmt.Sprintf(
		`find %q -name 'rollout-*.jsonl' -type f 2>/dev/null | while read -r f; do printf '%%s\t%%s\t%%s\n' "$(stat -c %%Y "$f")" "$f" "$(head -n1 "$f")"; done`,
		base+"/sessions")
	out, err := runInDistro(ctx, distro, script, false)
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

var codexSessionIdRe = regexp.MustCompile(`"session_id":("(?:[^"\\]|\\.)*")`)
var codexCwdRe = regexp.MustCompile(`"cwd":("(?:[^"\\]|\\.)*")`)

// readCodexMeta accepts a rollout's first line — possibly truncated to a head, since the full
// line carries the whole base prompt — and keeps it only when it belongs to cwd. The
// identifying fields sit in front of the prompt, so a truncated line falls back to targeted
// extraction.
func readCodexMeta(firstLine string, cwd string) (HistorySession, bool) {
	if firstLine == "" {
		return HistorySession{}, false
	}
	var meta codexMeta
	if err := json.Unmarshal([]byte(firstLine), &meta); err != nil {
		if !strings.Contains(firstLine, `"session_meta"`) {
			return HistorySession{}, false
		}
		meta.Type = "session_meta"
		meta.Payload = &codexMetaPayload{
			SessionId: extractJsonString(codexSessionIdRe, firstLine),
			Cwd:       extractJsonString(codexCwdRe, firstLine),
		}
	}
	if meta.Type != "session_meta" || meta.Payload == nil {
		return HistorySession{}, false
	}
	if !samePath(meta.Payload.Cwd, cwd) {
		return HistorySession{}, false
	}
	return HistorySession{SessionId: meta.Payload.SessionId, Cwd: meta.Payload.Cwd}, true
}

// extractJsonString pulls one string field out of a truncated JSON line, decoding escapes
// through the json package so a Windows cwd keeps its backslashes.
func extractJsonString(re *regexp.Regexp, line string) string {
	m := re.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(m[1]), &s); err != nil {
		return ""
	}
	return s
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
		out, err := runInDistro(ctx, distro, fmt.Sprintf("cat %q 2>/dev/null", base+"/session_index.jsonl"), false)
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

// runInDistro executes a script inside a distro. A login shell sources the whole profile and
// costs seconds per invocation, so it is opt-in: only scripts that depend on profile
// environment (PATH additions, exported config vars) need it; plain file access does not.
func runInDistro(ctx context.Context, distro string, script string, loginShell bool) (string, error) {
	flag := "-c"
	if loginShell {
		flag = "-lc"
	}
	out, err := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "-e", "bash", flag, script).Output()
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

// readFileHead returns up to n bytes from the start of a file, cut at the first newline when
// one fits inside the head.
func readFileHead(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, n)
	got, _ := io.ReadFull(f, buf)
	head := buf[:got]
	if idx := bytes.IndexByte(head, '\n'); idx >= 0 {
		head = head[:idx]
	}
	return string(head)
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
	if len(s) <= 80 {
		return s
	}
	// Cut on a rune boundary: a byte cut through an accented character leaves mojibake.
	cut := 80
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
