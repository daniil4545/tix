package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	ident "github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/task"
)

// Errors of the agent cycle.
var (
	ErrClaimed    = errors.New("claimed by another actor")
	ErrNoIdentity = errors.New("no identity")
	ErrRule       = errors.New("rule violated")
)

// kindErr - an error with a ready reply text; errors.Is finds kind in it.
type kindErr struct {
	kind error
	msg  string
}

func (e *kindErr) Error() string { return e.msg }
func (e *kindErr) Unwrap() error { return e.kind }

func rule(format string, args ...any) error {
	return &kindErr{kind: ErrRule, msg: fmt.Sprintf(format, args...)}
}

// TakeMode - the mode of Take.
type TakeMode int

// Take modes.
const (
	Take TakeMode = iota
	Steal
	Release
)

// Place - the position in a column: First - to the start, After - after a task; empty - to the end.
type Place struct {
	First bool
	After string
}

// MoveOpts - parameters of Move. Message - the value of -m, Force - close with unchecked criteria.
type MoveOpts struct {
	Place   Place
	Message string
	Force   bool
}

// Field - a name-value pair for Set.
type Field struct{ Name, Value string }

const (
	allowedFields = "title project waiting tags external abstract"
)

func (s *Store) claimed(id string, c task.Claim) error {
	return &kindErr{kind: ErrClaimed, msg: fmt.Sprintf("claimed by %s until %s; steal: tix take %s --steal",
		ident.Short(c.Actor), c.Until.Format("15:04"), id)}
}

// Take takes a task, hands out or steals a claim. actor "" or "unknown": ErrNoIdentity.
func (s *Store) Take(id string, mode TakeMode, actor string) (Result, error) {
	if actor == "" || actor == unknownActor {
		return Result{}, fmt.Errorf("%w; pass --as agent:id", ErrNoIdentity)
	}
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	op := &opInfo{kind: "take"}
	var info string
	res, err := s.modifyTx(id, actor, op, func(_ *txEnv, t *task.Task, _ string) error {
		op.subject = "take"
		var err error
		if mode == Release {
			op.kind, op.subject = "release", "release"
			info, err = s.release(t, actor)
			return err
		}
		info, err = s.take(t, actor, mode == Steal, op)
		return err
	})
	res.Info = info
	return res, err
}

// release drops the claim; an in_progress task goes back to the head of open: in_progress means someone works on it.
func (s *Store) release(t *task.Task, actor string) (string, error) {
	claim, has := t.ClaimInfo()
	if has && claim.Actor != actor && claim.Until.After(s.now()) {
		return "", s.claimed(t.ID, claim)
	}
	if has {
		t.ClearClaim() // own or expired: a live foreign claim was refused above
	}
	if s.kindOf(t.Status) != config.InProgress {
		return "released", nil
	}
	from, to := t.Status, s.conf().DefaultOf(config.Open)
	pl, err := s.place(t, to, Place{First: true})
	if err != nil {
		return "", err
	}
	t.Status, t.Rank = to, pl.rank
	return "released  " + s.transition(from, to), nil
}

func (s *Store) take(t *task.Task, actor string, steal bool, op *opInfo) (string, error) {
	kind := s.kindOf(t.Status)
	canTake := slices.Contains([]string{config.Open, config.Blocked, config.InProgress}, kind)
	if kind == config.Idea {
		canTake = len(t.Criteria()) > 0
	}
	if !canTake {
		if len(t.Criteria()) == 0 {
			return "", rule("%s is in %s and has no criteria; add `- [ ]` lines under ## Details with tix edit, "+
				"then run: tix move %s %s, then tix take %s", t.ID, t.Status, t.ID, s.conf().DefaultOf(config.Open), t.ID)
		}
		return "", rule("%s is in %s; run: tix move %s %s", t.ID, t.Status, t.ID, s.conf().DefaultOf(config.Open))
	}
	claim, has := t.ClaimInfo()
	live := has && claim.Until.After(s.now())
	foreign := live && claim.Actor != actor
	if foreign && !steal {
		return "", s.claimed(t.ID, claim)
	}
	if foreign {
		op.subject = "steal " + ident.Short(claim.Actor)
	}
	from := t.Status
	if kind == config.Blocked {
		if err := t.SetField("waiting", ""); err != nil {
			return "", err
		}
	}
	if kind != config.InProgress {
		to := s.conf().DefaultOf(config.InProgress)
		pl, err := s.place(t, to, Place{})
		if err != nil {
			return "", err
		}
		t.Status, t.Rank = to, pl.rank
	}
	if !live || foreign {
		t.SetClaim(task.Claim{Actor: actor, Until: s.now().Add(claimTTL)})
	}
	c, _ := t.ClaimInfo()
	return fmt.Sprintf("%s  @%s until %s", s.transition(from, t.Status), ident.Short(c.Actor), c.Until.Format("15:04")), nil
}

// transition prints "from > to"; a move into the same column is just its name.
func (s *Store) transition(from, to string) string {
	if from == to {
		return s.label(to)
	}
	return s.label(from) + " > " + s.label(to)
}

// label prints a status as its name, or "name [kind]" when the name is not the kind word.
func (s *Store) label(name string) string {
	if k := s.kindOf(name); k != "" && k != name {
		return name + " [" + k + "]"
	}
	return name
}

// Move moves a task into a status by the entry rules.
func (s *Store) Move(id, column string, o MoveOpts, actor string) (Result, error) {
	return s.moveTo(id, func(*task.Task) string { return column }, o, actor)
}

// Close moves a task to the default status of the kind closed; a task already of that kind stays
// where it is (ok, no commit). This is "done"; Move to a name would send a task of another closed status away.
func (s *Store) Close(id string, o MoveOpts, actor string) (Result, error) {
	return s.moveTo(id, func(t *task.Task) string {
		if s.kindOf(t.Status) == config.Closed {
			return t.Status
		}
		return s.conf().DefaultOf(config.Closed)
	}, o, actor)
}

// moveTo is Move with the target status chosen under the lock, after the config is read.
func (s *Store) moveTo(id string, target func(*task.Task) string, o MoveOpts, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	op := &opInfo{kind: "move"}
	var info string
	var ready []string
	res, err := s.modifyTx(id, actor, op, func(tx *txEnv, t *task.Task, _ string) error {
		column := target(t)
		if err := s.conf().CheckStatus(column); err != nil {
			return rule("%v", err)
		}
		wasClosed := s.kindOf(t.Status) == config.Closed
		nowClosed := s.kindOf(column) == config.Closed
		if nowClosed && !wasClosed && t.IsEpic() {
			if err := s.settleLines(t); err != nil {
				return err
			}
		}
		var err error
		info, err = s.move(tx, t, column, o, actor, op)
		if err == nil && wasClosed != nowClosed {
			ready = s.syncEpics(tx, t.ID, nowClosed)
		}
		return err
	})
	res.Info = info
	res.Ready = ready
	return res, err
}

func (s *Store) move(tx *txEnv, t *task.Task, column string, o MoveOpts,
	actor string, op *opInfo) (string, error) {
	from := t.Status
	msg := strings.TrimSpace(o.Message)
	fromKind, toKind := s.kindOf(from), s.kindOf(column)
	if msg != "" && toKind != config.Blocked && toKind != config.Closed {
		return "", rule("-m is only for moves to %s and %s", config.Blocked, config.Closed)
	}
	op.subject = "move " + from + " > " + column
	same := from == column
	switch {
	case fromKind != toKind: // an unknown status has the kind "": it gets the checks of the target
		if err := s.leave(t, fromKind, toKind, column, msg, o, actor, op); err != nil {
			return "", err
		}
	case toKind == config.Blocked && msg != "":
		if err := t.SetField("waiting", msg); err != nil {
			return "", err
		}
	}
	if !same || o.Place != (Place{}) {
		pl, err := s.place(t, column, o.Place)
		if err != nil {
			return "", err
		}
		if !pl.keep {
			t.Rank = pl.rank
			tx.pending = pl.neighbours
		}
	}
	t.Status = column
	return s.transition(from, column), nil
}

// leave applies a transition between different columns: entry checks, fields and claim.
// from and to are kinds; name is the target status.
func (s *Store) leave(t *task.Task, from, to, name, msg string, o MoveOpts, actor string, op *opInfo) error {
	forced, err := s.enter(t, name, msg, o, actor)
	if err != nil {
		return err
	}
	if to == config.Closed {
		op.kind, op.subject = "done", "done"
		if forced {
			op.subject = "done force"
		}
	}
	if from == config.Blocked {
		if err := t.SetField("waiting", ""); err != nil {
			return err
		}
	}
	if from == config.InProgress || from == "" || to == config.Closed {
		t.ClearClaim()
	}
	return nil
}

// enter checks the entry into the status to and writes its fields (waiting, the Result comment).
// forced: the closing bypassed unchecked criteria.
func (s *Store) enter(t *task.Task, to, msg string, o MoveOpts, actor string) (bool, error) {
	switch s.kindOf(to) {
	case config.Open:
		if len(t.Criteria()) == 0 {
			return false, rule("open needs a criterion")
		}
	case config.Blocked:
		if msg == "" && strings.TrimSpace(t.Waiting) == "" {
			return false, rule("blocked needs a reason: use -m")
		}
		if msg != "" {
			return false, t.SetField("waiting", msg)
		}
	case config.Closed:
		forced, err := closeChecks(t, msg, o.Force)
		if err != nil {
			return false, err
		}
		if msg != "" {
			t.AddComment(s.now(), actor, "Result: "+msg)
		}
		return forced, nil
	}
	return false, nil
}

// closeChecks - closing checks. forced: unchecked criteria bypassed through --force.
func closeChecks(t *task.Task, msg string, force bool) (bool, error) {
	if force && msg == "" {
		return false, rule("--force needs -m result")
	}
	crit := t.Criteria()
	if len(crit) == 0 {
		if msg == "" {
			return false, rule("no criteria; use -m result")
		}
		return false, nil
	}
	var open []string
	for _, c := range crit {
		if !c.Done {
			open = append(open, strconv.Itoa(c.N))
		}
	}
	if len(open) == 0 {
		return false, nil
	}
	if !force {
		return false, rule("%d/%d criteria checked; run: tix check %s %s",
			len(crit)-len(open), len(crit), t.ID, strings.Join(open, " "))
	}
	return true, nil
}

// Set changes task fields.
func (s *Store) Set(id string, fields []Field, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	if len(fields) == 0 {
		return Result{}, errors.New("nothing to set: pass field=value")
	}
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	list := strings.Join(names, " ")
	op := &opInfo{kind: "set", subject: "set " + list}
	res, err := s.modifyTx(id, actor, op, func(_ *txEnv, t *task.Task, _ string) error {
		old := t.Project
		for _, f := range fields {
			if err := s.setField(t, f); err != nil {
				return err
			}
		}
		if t.Project != old {
			return s.checkKnownProject(t.Project)
		}
		return nil
	})
	res.Info = list
	return res, err
}

func (s *Store) setField(t *task.Task, f Field) error {
	switch f.Name {
	case "status", "claim", "rank":
		return rule("%s is changed by tix move", f.Name)
	case "id", "created":
		return rule("%s cannot be changed", f.Name)
	case "abstract":
		return t.SetSection("Abstract", f.Value)
	case "title":
		if strings.TrimSpace(f.Value) == "" {
			return rule("title is empty")
		}
		return t.SetField("title", strings.TrimSpace(f.Value))
	case "project":
		project := strings.ToLower(strings.TrimSpace(f.Value))
		if project == "" || strings.ContainsAny(project, "\r\n") {
			return rule("project must be one non-empty line")
		}
		return t.SetField("project", project)
	case "waiting":
		if strings.TrimSpace(f.Value) == "" && s.kindOf(t.Status) == config.Blocked {
			return rule("blocked needs a reason: use -m, or leave with tix move")
		}
	case "tags", "external":
	default:
		return rule("field %s cannot be set; allowed: %s", f.Name, allowedFields)
	}
	return t.SetField(f.Name, f.Value)
}

// Check marks criteria with numbers nums. A task line of an epic follows the status of its task and is refused.
func (s *Store) Check(id string, nums []int, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	if len(nums) == 0 {
		return Result{}, errors.New("check needs criterion numbers")
	}
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	op := &opInfo{kind: "check", subject: "check " + strings.Join(parts, " ")}
	var info string
	res, err := s.modifyTx(id, actor, op, func(_ *txEnv, t *task.Task, _ string) error {
		crit := t.Criteria()
		if t.IsEpic() {
			for _, n := range nums {
				if n >= 1 && n <= len(crit) && task.LineTask(crit[n-1].Text) != "" {
					return rule("line %d follows the status of %s", n, task.LineTask(crit[n-1].Text))
				}
			}
		}
		if _, err := t.Check(nums...); err != nil {
			return err
		}
		crit = t.Criteria()
		done := 0
		for _, c := range crit {
			if c.Done {
				done++
			}
		}
		info = fmt.Sprintf("%d/%d", done, len(crit))
		return nil
	})
	res.Info = info
	return res, err
}

// ReleaseAll drops every live claim of the actor in one commit: an in_progress task goes back to the head
// of open, as on a single release. Foreign and expired claims stay. The result lists the released ids.
func (s *Store) ReleaseAll(actor string) ([]string, Result, error) {
	if actor == "" || actor == unknownActor {
		return nil, Result{}, fmt.Errorf("%w; pass --as agent:id", ErrNoIdentity)
	}
	actor, err := checkActor(actor)
	if err != nil {
		return nil, Result{}, err
	}
	var ids []string
	var res Result
	err = s.withLock(false, func(tx *txEnv) error {
		if err := s.syncExternal(tx); err != nil {
			return err
		}
		var files []gitx.File
		for _, e := range s.rankIndex() {
			if s.kindOf(e.status) != config.InProgress {
				continue // only in_progress carries a claim: no parse of the rest
			}
			f, ok, err := s.releaseOwn(e.path, actor)
			if err != nil {
				return err
			}
			if ok {
				ids = append(ids, e.id)
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			return nil
		}
		sort.Slice(ids, func(i, j int) bool { return IDNum(ids[i]) < IDNum(ids[j]) })
		files = append(files, tx.extra...)
		s.commit(tx, &res, files, "release "+strings.Join(ids, " "), actor)
		return nil
	})
	return ids, res, err
}

// releaseOwn releases the live claim of actor in the file path; ok=false - the task is not held by actor.
func (s *Store) releaseOwn(path, actor string) (gitx.File, bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is built by the core
	if err != nil {
		return gitx.File{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	t, err := task.Parse(path, data)
	if err != nil {
		return gitx.File{}, false, nil // a broken card is not ours to fix
	}
	if c, has := t.ClaimInfo(); !has || c.Actor != actor || !c.Until.After(s.now()) {
		return gitx.File{}, false, nil
	}
	if _, err := s.release(t, actor); err != nil {
		return gitx.File{}, false, err
	}
	out := t.Render()
	ok, err := s.replace(path, data, out)
	if err != nil || !ok {
		return gitx.File{}, false, err
	}
	return gitx.File{Path: filepath.Base(path), Data: out}, true, nil
}
