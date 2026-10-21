package cli

import (
	"encoding/json"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/task"
)

const (
	inboxLimit   = 8000 // chars of delivery text; the Claude Code limit is 10 000
	inboxReserve = 80   // reserve for the last line "... more"
	inboxMinCut  = 40   // below this remainder a comment is not cut but goes to more
	humanActor   = "human"
	inboxHeader  = "New comments on your tasks (from tix):"

	eventPostTool = "PostToolUse"
	eventPrompt   = "UserPromptSubmit"
	eventStop     = "Stop"
)

// hookInput - the hook input fields that delivery needs.
type hookInput struct {
	SessionID string `json:"session_id"`
	Event     string `json:"hook_event_name"`
	AgentID   string `json:"agent_id"`
	TurnID    string `json:"turn_id"`
}

// readHookInput reads one JSON object from stdin (up to hookStdinLimit) within hookReadTimeout.
// Timeout or non-JSON input - ok=false.
func readHookInput(r io.Reader) (in hookInput, ok bool) {
	done := make(chan hookInput, 1)
	go func() {
		var got hookInput
		if json.NewDecoder(io.LimitReader(r, hookStdinLimit)).Decode(&got) != nil {
			done <- hookInput{}
			return
		}
		done <- got
	}()
	select {
	case got := <-done:
		return got, got.Event != ""
	case <-time.After(hookReadTimeout):
		return hookInput{}, false
	}
}

// newComment - a comment not yet delivered to the agent.
type newComment struct {
	id string
	c  task.Comment
}

func (a *app) cmdInbox(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"hook": false}))
	if err != nil {
		return err
	}
	if len(p.pos) > 0 {
		return inputf("inbox takes no arguments, extra: %s", p.pos[0])
	}
	if !p.set["hook"] {
		return inputf("inbox works only as a hook: tix inbox --hook")
	}
	// The hook does not break the agent's work: any failure is empty output and code 0, the reason goes to .tix/log.
	defer func() {
		if r := recover(); r != nil {
			a.log.Warn("inbox hook panic", "op", "inbox", "err", r)
		}
	}()
	a.deliverInbox()
	return nil
}

// deliverInbox prints new comments on the tasks the hook agent holds.
func (a *app) deliverInbox() {
	if a.env.Stdin == nil {
		return
	}
	in, ok := readHookInput(a.env.Stdin)
	if !ok {
		// No log yet: the store is not open. The reason (cut-off, garbage, over the ceiling) goes to .tix/log.
		if a.open() == nil {
			a.log.Warn("inbox: hook input not read", "op", "inbox", "limit", hookStdinLimit)
		}
		return
	}
	if in.AgentID != "" {
		return
	}
	switch in.Event {
	case eventPostTool, eventPrompt, eventStop:
	default:
		return
	}
	if err := a.open(); err != nil {
		a.log.Warn("inbox: base not opened", "op", "inbox", "err", firstLine(err.Error()))
		return
	}
	who := actor.HookActor(a.env.Getenv, in.SessionID, in.TurnID != "")
	file := actor.FocusFile(who)
	dir := actor.InboxDir(a.env.Getenv)
	if file == "" || dir == "" {
		a.log.Warn("inbox: no actor or marks dir", "op", "inbox", "session", in.SessionID)
		return
	}
	// Empty store: nothing to read.
	if _, statErr := os.Stat(a.root); statErr != nil {
		return
	}
	unlock, err := actor.LockMarks(dir, file)
	if err != nil {
		a.log.Warn("inbox: marks locked", "op", "inbox", "actor", who, "err", err)
		return
	}
	defer unlock()
	tasks, errs := a.store.List()
	for _, e := range errs {
		a.log.Warn("inbox: task not read", "op", "inbox", "err", firstLine(e.Error()))
	}
	marks, _ := actor.ReadMarks(dir, file)
	before := maps.Clone(marks)
	fresh, changed := collectNew(tasks, who, marks)
	// Stop wakes the agent only with a human word: agent comments wait for PostToolUse or UserPromptSubmit,
	// otherwise two agents drive each other in circles.
	if in.Event == eventStop && !hasHuman(fresh) {
		for _, nc := range fresh {
			marks[nc.id] = before[nc.id]
		}
		fresh = nil
		changed = !maps.Equal(marks, before)
	}
	if !changed {
		return
	}
	// The mark moves before printing: delivery happens at most once, Stop does not loop.
	if err := actor.WriteMarks(dir, file, marks); err != nil {
		a.log.Warn("inbox: marks not written", "op", "inbox", "actor", who, "err", err)
		return
	}
	if len(fresh) == 0 {
		return
	}
	a.printInbox(in.Event, inboxText(fresh))
}

// collectNew compares the tasks that who holds with the marks: moves marks and returns others'
// comments numbered above the mark. changed - marks changed. A task without a mark gets the
// current count and delivers nothing; a mark above the current count resets to it.
func collectNew(tasks []*task.Task, who string, marks actor.Marks) (fresh []newComment, changed bool) {
	for _, t := range tasks {
		if claim, ok := t.ClaimInfo(); !ok || claim.Actor != who {
			continue
		}
		cur := len(t.Comments)
		seen, has := marks[t.ID]
		if has && seen == cur {
			continue
		}
		marks[t.ID] = cur
		changed = true
		if !has || seen < 0 || seen > cur {
			continue
		}
		for _, c := range t.Comments[seen:] {
			if c.Actor != who {
				fresh = append(fresh, newComment{id: t.ID, c: c})
			}
		}
	}
	slices.SortStableFunc(fresh, func(x, y newComment) int { return taskNumber(x.id) - taskNumber(y.id) })
	return fresh, changed
}

func hasHuman(fresh []newComment) bool {
	return slices.ContainsFunc(fresh, func(nc newComment) bool { return nc.c.Actor == humanActor })
}

func taskNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "T-"))
	return n
}

// inboxText builds the delivery text up to inboxLimit chars. A long comment is cut;
// what did not fit is named by the last line "... more: tix show <id>".
func inboxText(fresh []newComment) string {
	var b strings.Builder
	b.WriteString(inboxHeader)
	used := runeLen(inboxHeader)
	var left []string
	for i, nc := range fresh {
		prefix := "\n" + nc.id + " " + nc.c.Actor + " " + nc.c.At.Format("15:04") + ": "
		text := nc.c.Text
		room := inboxLimit - inboxReserve - used - runeLen(prefix)
		if runeLen(text) > room {
			if room < inboxMinCut {
				left = missing(left, fresh[i:])
				break
			}
			text = string([]rune(text)[:room-3]) + "..."
			left = missing(left, fresh[i:i+1])
			b.WriteString(prefix + text)
			left = missing(left, fresh[i+1:])
			break
		}
		b.WriteString(prefix + text)
		used += runeLen(prefix) + runeLen(text)
	}
	if len(left) > 0 {
		b.WriteString("\n... more: tix show " + strings.Join(left, ", "))
	}
	return b.String()
}

// missing appends to left the task ids from rest that are not there yet.
func missing(left []string, rest []newComment) []string {
	for _, nc := range rest {
		if !slices.Contains(left, nc.id) {
			left = append(left, nc.id)
		}
	}
	return left
}

func runeLen(s string) int { return len([]rune(s)) }

// printInbox prints the hook reply in the event format: Stop returns the agent to work
// through decision=block, other events add text to the context.
func (a *app) printInbox(event, text string) {
	var out any
	if event == eventStop {
		out = map[string]string{"decision": "block", "reason": text}
	} else {
		out = map[string]any{"hookSpecificOutput": map[string]string{
			"hookEventName": event, "additionalContext": text,
		}}
	}
	enc := json.NewEncoder(a.env.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		a.log.Warn("inbox: output failed", "op", "inbox", "err", err)
	}
}
