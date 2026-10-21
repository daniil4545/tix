package cli_test

// Comment delivery from a human to an agent. Written from the spec, without reading the code of zone I.
// The marks dir is outside the store: TIX_INBOX_DIR.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	inboxHeader = "New comments on your tasks (from tix):"
	inboxLimit  = 8000
)

func (b *box) inboxDir() string { return filepath.Join(b.root, "inbox") }

// inboxMark - the marks file of session claude:c1: <agent>-<session id>.
func (b *box) inboxMark() string { return filepath.Join(b.inboxDir(), "claude-c1") }

// newInboxBox - a rig with Claude session "c1" and a marks dir in a temp folder.
func newInboxBox(t *testing.T) *box {
	t.Helper()
	b := newBox(t)
	b.extra = map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "TIX_INBOX_DIR": b.inboxDir()}
	return b
}

// hold creates T-1 and claims it with session c1.
func (b *box) hold() {
	b.t.Helper()
	b.ok("", "new", "Work", "--to", "todo")
	b.ok("", "take", "T-1")
}

func (b *box) comment(id, text, as string) {
	b.t.Helper()
	b.ok("", "comment", id, text, "--as", as)
}

func hookIn(event string, kv ...string) string {
	m := map[string]string{"session_id": "c1", "hook_event_name": event}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	data, _ := json.Marshal(m)
	return string(data)
}

// inbox runs `tix inbox --hook` with the hook input and returns the delivered text ("" - empty).
// The exit code is always 0; the reply format is checked against the event.
func (b *box) inbox(event string, kv ...string) string {
	b.t.Helper()
	return b.inboxRaw(hookIn(event, kv...), event)
}

func (b *box) inboxRaw(stdin, event string) string {
	b.t.Helper()
	r := b.tix(stdin, "inbox", "--hook")
	if r.code != 0 {
		b.t.Fatalf("inbox --hook %q: code %d, stdout %q, stderr %q, want 0", stdin, r.code, r.out, r.err)
	}
	return parseHookOut(b.t, r.out, event)
}

func parseHookOut(t *testing.T, out, event string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("whitespace-only output %q, want empty or JSON", out)
	}
	if event == "Stop" {
		var v struct{ Decision, Reason string }
		if err := json.Unmarshal([]byte(out), &v); err != nil || v.Decision != "block" || v.Reason == "" {
			t.Fatalf("Stop: output %q, want {decision:block, reason:<text>}: %v", out, err)
		}
		return v.Reason
	}
	var v struct {
		H struct {
			Name string `json:"hookEventName"`
			Text string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.H.Name != event || v.H.Text == "" {
		t.Fatalf("%s: output %q, want hookSpecificOutput with hookEventName=%s and additionalContext: %v", event, out, event, err)
	}
	return v.H.Text
}

// wantLine looks for a comment line `T-N author HH:MM: text`.
func wantLine(t *testing.T, text, id, author, body string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(id) + ` ` + regexp.QuoteMeta(author) + ` \d\d:\d\d: ` + regexp.QuoteMeta(body) + `$`)
	if !re.MatchString(text) {
		t.Errorf("the text has no line `%s %s HH:MM: %s`:\n%s", id, author, body, text)
	}
}

// The first call is empty and sets the mark; a new comment is delivered once.
func TestInbox_FirstCallEmptyThenHumanCommentDeliveredOnce(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.comment("T-1", "old remark before first call", "human")

	if got := b.inbox("PostToolUse"); got != "" {
		t.Fatalf("first call: %q, want empty (old comments are read via tix show)", got)
	}
	if _, err := os.Stat(b.inboxMark()); err != nil {
		t.Fatalf("no marks file %s after the first call: %v", b.inboxMark(), err)
	}
	if got := b.inbox("PostToolUse"); got != "" {
		t.Fatalf("an old comment was delivered after the first call: %q", got)
	}

	b.comment("T-1", "Answer: use sqlite", "human")
	got := b.inbox("PostToolUse")
	if !strings.HasPrefix(got, inboxHeader+"\n") {
		t.Errorf("text %q, want header %q as the first line", got, inboxHeader)
	}
	wantLine(t, got, "T-1", "human", "Answer: use sqlite")
	if strings.Contains(got, "old remark") {
		t.Errorf("a comment before the mark was delivered: %q", got)
	}
	if again := b.inbox("PostToolUse"); again != "" {
		t.Errorf("repeat: %q, want empty", again)
	}
}

// Two comments in a row (within one minute) are both delivered, in order.
func TestInbox_TwoCommentsSameMinute_BothDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", "first remark", "human")
	b.comment("T-1", "second remark", "human")

	got := b.inbox("PostToolUse")
	wantLine(t, got, "T-1", "human", "first remark")
	wantLine(t, got, "T-1", "human", "second remark")
	if strings.Index(got, "first remark") > strings.Index(got, "second remark") {
		t.Errorf("order broken:\n%s", got)
	}
	if again := b.inbox("PostToolUse"); again != "" {
		t.Errorf("repeat: %q, want empty", again)
	}
}

// PostToolUse and UserPromptSubmit - additionalContext with the same hookEventName.
func TestInbox_PostToolUseAndUserPromptSubmit_AdditionalContext(t *testing.T) {
	for _, event := range []string{"PostToolUse", "UserPromptSubmit"} {
		t.Run(event, func(t *testing.T) {
			defer guard(t)
			b := newInboxBox(t)
			b.hold()
			b.inbox(event)
			b.comment("T-1", "do it", "human")
			wantLine(t, b.inbox(event), "T-1", "human", "do it")
		})
	}
}

// The agent's own comment is not delivered.
func TestInbox_OwnComment_NotDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", "my own remark", "claude:c1")
	if got := b.inbox("PostToolUse"); got != "" {
		t.Errorf("own comment delivered: %q", got)
	}
}

// A comment on a task without a claim by this identity and on a task claimed by another is not delivered;
// a comment on its own task is delivered (control: empty is not due to a breakage).
func TestInbox_UnclaimedAndForeignClaim_NotDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.ok("", "new", "Free", "--to", "todo")
	b.ok("", "new", "Foreign", "--to", "todo")
	b.ok("", "take", "T-3", "--as", "claude:other")
	b.inbox("PostToolUse")

	b.comment("T-1", "for mine", "human")
	b.comment("T-2", "for free", "human")
	b.comment("T-3", "for foreign", "human")
	got := b.inbox("PostToolUse")
	wantLine(t, got, "T-1", "human", "for mine")
	for _, bad := range []string{"for free", "for foreign", "T-2", "T-3"} {
		if strings.Contains(got, bad) {
			t.Errorf("extra delivered %q:\n%s", bad, got)
		}
	}
}

// Stop with a new comment - decision block and the text in reason; without a new one - empty; a repeat is empty.
func TestInbox_Stop_BlocksWithTextOnlyWhenNew(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	if got := b.inbox("Stop"); got != "" {
		t.Fatalf("Stop without comments: %q, want empty", got)
	}
	b.comment("T-1", "one more thing", "human")
	got := b.inbox("Stop")
	if !strings.HasPrefix(got, inboxHeader+"\n") {
		t.Errorf("reason %q, want header %q", got, inboxHeader)
	}
	wantLine(t, got, "T-1", "human", "one more thing")
	if again := b.inbox("Stop"); again != "" {
		t.Errorf("repeated Stop: %q, want empty (Stop must not loop)", again)
	}
}

// Many comments - text up to 8000 chars, tail `more`, a repeated Stop is empty.
func TestInbox_ManyComments_TruncatedWithMoreTail(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("Stop")
	for i := 1; i <= 6; i++ {
		b.comment("T-1", fmt.Sprintf("n%d %s", i, strings.Repeat("a", 2000)), "human")
	}
	got := b.inbox("Stop")
	if n := utf8.RuneCountInString(got); n > inboxLimit {
		t.Errorf("text %d chars, limit %d", n, inboxLimit)
	}
	if !strings.Contains(got, "n1 ") {
		t.Errorf("the first comment was not delivered")
	}
	if last := got[strings.LastIndex(strings.TrimRight(got, "\n"), "\n")+1:]; !strings.HasPrefix(strings.TrimRight(last, "\n"), "... more: tix show T-1") {
		t.Errorf("last line %q, want `... more: tix show T-1`", last)
	}
	if again := b.inbox("Stop"); again != "" {
		t.Errorf("repeated Stop: %q, want empty", again)
	}
}

// One comment longer than 8000 chars is cut to the limit.
func TestInbox_SingleLongComment_Truncated(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", strings.Repeat("x", 12000), "human")
	got := b.inbox("PostToolUse")
	if got == "" {
		t.Fatal("the long comment was not delivered at all")
	}
	if n := utf8.RuneCountInString(got); n > inboxLimit {
		t.Errorf("text %d chars, limit %d", n, inboxLimit)
	}
	if again := b.inbox("PostToolUse"); again != "" {
		t.Errorf("repeat: %q, want empty", again)
	}
}

// Input with agent_id (a call from a subagent) - empty, and the reply stays for the main agent.
func TestInbox_SubagentCall_EmptyAndKeepsCommentForMainAgent(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", "for the main agent", "human")
	if got := b.inbox("PostToolUse", "agent_id", "sub1"); got != "" {
		t.Fatalf("call with agent_id: %q, want empty", got)
	}
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "for the main agent")
}

// An expired claim of its own identity does not block delivery (the reply to an ask arrives after 3 h).
func TestInbox_ExpiredOwnClaim_StillDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.rewrite("T-1", func(s string) string {
		return regexp.MustCompile(`(?m)^claim: (\S+) until .*$`).ReplaceAllString(s, "claim: $1 until 2000-01-01T00:00")
	})
	if c := b.claimOf("T-1"); !strings.HasSuffix(c, "until 2000-01-01T00:00") {
		t.Fatalf("claim did not expire in setup: %q", c)
	}
	b.inbox("PostToolUse")
	b.comment("T-1", "late answer", "human")
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "late answer")
}

// An unreadable marks file = no mark: empty, nothing old delivered, then it works.
func TestInbox_CorruptMarkFile_EmptyThenWorks(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	if err := os.WriteFile(b.inboxMark(), []byte("not json{"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.comment("T-1", "written while mark broken", "human")
	if got := b.inbox("PostToolUse"); got != "" {
		t.Fatalf("with a corrupt marks file: %q, want empty", got)
	}
	b.comment("T-1", "after repair", "human")
	got := b.inbox("PostToolUse")
	wantLine(t, got, "T-1", "human", "after repair")
	if strings.Contains(got, "written while mark broken") {
		t.Errorf("a comment counted at the mark reset was delivered:\n%s", got)
	}
}

// A number in the file above the current one (external edit) - reset to the current, a new one is delivered after.
func TestInbox_MarkAboveCurrent_ResetsAndDeliversNew(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	data, err := os.ReadFile(b.inboxMark())
	if err != nil {
		t.Fatal(err)
	}
	var marks map[string]int
	if err := json.Unmarshal(data, &marks); err != nil || len(marks) != 1 {
		t.Fatalf("marks file %q: want a JSON object with one task: %v", data, err)
	}
	for k := range marks {
		marks[k] = 99
	}
	data, _ = json.Marshal(marks)
	if err := os.WriteFile(b.inboxMark(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	b.comment("T-1", "first after reset", "human")
	b.inbox("PostToolUse")
	b.comment("T-1", "second after reset", "human")
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "second after reset")
}

// No stdin, garbage, another event, no store, no identity - empty, code 0.
func TestInbox_BadInputOrEnvironment_EmptyExitZero(t *testing.T) {
	cases := []struct {
		name  string
		stdin string
		setup func(b *box)
		env   map[string]string // nil - the environment of session c1
	}{
		{"no stdin", "", nil, nil},
		{"garbage", "this is not json", nil, nil},
		{"json array", `[1,2]`, nil, nil},
		{"other event", hookIn("SessionStart"), nil, nil},
		{"no event", `{"session_id":"c1"}`, nil, nil},
		{"no identity", `{"hook_event_name":"PostToolUse"}`, nil, map[string]string{}},
		{"no base", hookIn("PostToolUse"), func(b *box) { b.home = filepath.Join(b.root, "absent", "tasks") }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newInboxBox(t)
			if c.setup == nil {
				b.hold()
				b.inbox("PostToolUse")
				b.comment("T-1", "pending", "human")
			} else {
				c.setup(b)
			}
			if c.env != nil {
				b.extra = map[string]string{"TIX_INBOX_DIR": b.inboxDir()}
			}
			r := b.tix(c.stdin, "inbox", "--hook")
			if r.code != 0 || r.out != "" {
				t.Fatalf("stdin %q: code %d, stdout %q, stderr %q, want code 0 and empty stdout", c.stdin, r.code, r.out, r.err)
			}
		})
	}
}

// The hook environment gives no identity - the session_id of the input is taken with the prefix claude:.
func TestInbox_NoEnvIdentity_UsesSessionIDFromInput(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.extra = map[string]string{"TIX_INBOX_DIR": b.inboxDir()}
	b.inbox("PostToolUse")
	b.comment("T-1", "found by session id", "human")
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "found by session id")
}
