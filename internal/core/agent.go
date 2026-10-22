package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

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
	ErrNoRemote   = errors.New("no remote")
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
	roleReady   = "ready"
	roleActive  = "active"
	roleWaiting = "waiting"
	roleClosed  = "closed"

	allowedFields = "title waiting due tags ask external abstract summary"
)

// loadConfig reads config.toml again: under the lock the config could have been changed by hand.
func (s *Store) loadConfig() (*config.Config, error) {
	return config.Load(filepath.Join(s.root, tixDir, configFile))
}

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
		cfg, err := s.loadConfig()
		if err != nil {
			return err
		}
		if mode == Release {
			op.kind, op.subject = "release", "release"
			info, err = s.release(t, cfg, actor)
			return err
		}
		info, err = s.take(t, cfg, actor, mode == Steal, op)
		return err
	})
	res.Info = info
	return res, err
}

// release drops the claim; a task in the active column goes back to the head of ready: active means someone works on it.
func (s *Store) release(t *task.Task, cfg *config.Config, actor string) (string, error) {
	claim, has := t.ClaimInfo()
	if has && claim.Actor != actor && claim.Until.After(s.now()) {
		return "", s.claimed(t.ID, claim)
	}
	if has {
		t.ClearClaim() // own or expired: a live foreign claim was refused above
	}
	col, ok := cfg.ByRole(roleReady)
	if cfg.RoleOf(t.Status) != roleActive || !ok {
		return "released", nil
	}
	pl, err := s.place(t, col.Name, Place{First: true})
	if err != nil {
		return "", err
	}
	from := t.Status
	t.Status, t.Rank = col.Name, pl.rank
	return "released  " + transition(from, col.Name), nil
}

func (s *Store) take(t *task.Task, cfg *config.Config, actor string, steal bool, op *opInfo) (string, error) {
	role := cfg.RoleOf(t.Status)
	if role != roleReady && role != roleWaiting && role != roleActive {
		hint := "<ready column>"
		if c, ok := cfg.ByRole(roleReady); ok {
			hint = c.Name
		}
		if len(t.Criteria()) == 0 {
			return "", rule("%s is in %s and has no criteria; add `- [ ]` lines under ## Details with tix edit, "+
				"then run: tix move %s %s, then tix take %s", t.ID, t.Status, t.ID, hint, t.ID)
		}
		return "", rule("%s is in %s; run: tix move %s %s", t.ID, t.Status, t.ID, hint)
	}
	active, ok := cfg.ByRole(roleActive)
	if !ok {
		return "", rule("config has no column with role %s", roleActive)
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
	if role == roleWaiting {
		if err := t.SetField("waiting", ""); err != nil {
			return "", err
		}
	}
	if from != active.Name {
		pl, err := s.place(t, active.Name, Place{})
		if err != nil {
			return "", err
		}
		t.Status, t.Rank = active.Name, pl.rank
	}
	if !live || foreign {
		t.SetClaim(task.Claim{Actor: actor, Until: s.now().Add(claimTTL)})
	}
	c, _ := t.ClaimInfo()
	return fmt.Sprintf("%s  @%s until %s", transition(from, active.Name), ident.Short(c.Actor), c.Until.Format("15:04")), nil
}

// transition prints "from > to"; a move into the same column is just its name.
func transition(from, to string) string {
	if from == to {
		return to
	}
	return from + " > " + to
}

// Move moves a task into a column by the role rules.
func (s *Store) Move(id, column string, o MoveOpts, actor string) (Result, error) {
	actor, err := checkActor(actor)
	if err != nil {
		return Result{}, err
	}
	op := &opInfo{kind: "move"}
	var info string
	res, err := s.modifyTx(id, actor, op, func(tx *txEnv, t *task.Task, _ string) error {
		cfg, err := s.loadConfig()
		if err != nil {
			return err
		}
		info, err = s.move(tx, t, cfg, cfg.Resolve(column), o, actor, op)
		return err
	})
	res.Info = info
	return res, err
}

func (s *Store) move(tx *txEnv, t *task.Task, cfg *config.Config, column string, o MoveOpts,
	actor string, op *opInfo) (string, error) {
	from := t.Status
	msg := strings.TrimSpace(o.Message)
	fromRole, toRole, err := moveRoles(cfg, from, column, msg)
	if err != nil {
		return "", err
	}
	op.subject = "move " + from + " > " + column
	same := from == column
	switch {
	case !same:
		if err := s.leave(t, fromRole, toRole, msg, o, actor, op); err != nil {
			return "", err
		}
	case toRole == roleWaiting && msg != "":
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
	return transition(from, column), nil
}

// moveRoles checks that both columns exist and have a role, and that -m is appropriate.
func moveRoles(cfg *config.Config, from, column, msg string) (fromRole, toRole string, err error) {
	if !slices.ContainsFunc(cfg.Columns, func(c config.Column) bool { return c.Name == column }) {
		return "", "", rule("unknown column %q; columns: %s", column, cfg.Roles())
	}
	for _, c := range []string{from, column} {
		if cfg.RoleOf(c) == "" {
			return "", "", rule("column %s has no role", c)
		}
	}
	toRole = cfg.RoleOf(column)
	if msg != "" && toRole != roleWaiting && toRole != roleClosed {
		return "", "", rule("-m is only for moves to %s and %s", roleWaiting, roleClosed)
	}
	return cfg.RoleOf(from), toRole, nil
}

// leave applies a transition between different columns: entry checks, fields and claim.
func (s *Store) leave(t *task.Task, fromRole, toRole, msg string, o MoveOpts, actor string, op *opInfo) error {
	forced, err := s.enter(t, toRole, msg, o, actor)
	if err != nil {
		return err
	}
	if toRole == roleClosed {
		op.kind, op.subject = "done", "done"
		if forced {
			op.subject = "done force"
		}
	}
	if fromRole == roleWaiting {
		if err := t.SetField("waiting", ""); err != nil {
			return err
		}
	}
	if fromRole == roleActive || toRole == roleClosed {
		t.ClearClaim()
	}
	if toRole == roleClosed {
		return t.SetField("ask", "")
	}
	return nil
}

// enter checks the entry into a column of role toRole and writes its fields (waiting, the Result comment).
// forced: the closing bypassed unchecked criteria.
func (s *Store) enter(t *task.Task, toRole, msg string, o MoveOpts, actor string) (bool, error) {
	switch toRole {
	case roleReady:
		if len(t.Criteria()) == 0 {
			return false, rule("ready needs a criterion")
		}
	case roleWaiting:
		if msg == "" && strings.TrimSpace(t.Waiting) == "" {
			return false, rule("waiting needs a reason: use -m")
		}
		if msg != "" {
			return false, t.SetField("waiting", msg)
		}
	case roleClosed:
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
		cfg, err := s.loadConfig()
		if err != nil {
			return err
		}
		for _, f := range fields {
			if err := setField(t, cfg.RoleOf(t.Status), f); err != nil {
				return err
			}
		}
		return nil
	})
	res.Info = list
	return res, err
}

func setField(t *task.Task, role string, f Field) error {
	switch f.Name {
	case "status", "claim", "rank":
		return rule("%s is changed by tix move", f.Name)
	case "id", "created":
		return rule("%s cannot be changed", f.Name)
	case "abstract":
		return t.SetSection("Abstract", f.Value)
	case "summary":
		return t.SetSection("Summary", f.Value)
	case "title":
		if strings.TrimSpace(f.Value) == "" {
			return rule("title is empty")
		}
		return t.SetField("title", strings.TrimSpace(f.Value))
	case "waiting":
		if strings.TrimSpace(f.Value) == "" && role == roleWaiting {
			return rule("waiting needs a reason: use -m, or leave with tix move")
		}
	case "due":
		if _, err := time.Parse("2006-01-02", f.Value); err != nil && f.Value != "" {
			return rule("due must be 2006-01-02")
		}
	case "ask":
		if _, _, known := task.ParseAsk(f.Value); !known {
			return rule("unknown ask kind; a prefix must be one of: %s", strings.Join(task.AskKinds, ", "))
		}
	case "tags", "external":
	default:
		return rule("field %s cannot be set; allowed: %s", f.Name, allowedFields)
	}
	return t.SetField(f.Name, f.Value)
}

// Check marks criteria with numbers nums.
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
		if _, err := t.Check(nums...); err != nil {
			return err
		}
		crit := t.Criteria()
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

// Backup commits foreign edits under the lock and sends HEAD to origin without the lock.
func (s *Store) Backup(ctx context.Context) (Result, error) {
	err := s.withLock(false, func(tx *txEnv) error {
		if err := s.syncExternal(tx); err != nil {
			s.log.Warn("external edits not committed before backup", "op", "backup_sync", "err", err)
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return Result{}, fmt.Errorf("no tasks folder %s: %w", s.root, ErrNotFound)
	}
	if err != nil {
		return Result{}, err
	}
	repo, err := s.git()
	if err != nil {
		return Result{}, err
	}
	remotes, err := repo.Remotes()
	if err != nil {
		return Result{}, fmt.Errorf("list remotes: %w", err)
	}
	if !slices.Contains(remotes, "origin") {
		return Result{}, &kindErr{kind: ErrNoRemote, msg: "no remote origin; run: git remote add origin <url>"}
	}
	if err := repo.Push(ctx, "origin"); err != nil {
		s.log.Warn("push failed", "op", "push", "remote", "origin", "err", gitx.Detail(err))
		return Result{}, fmt.Errorf("backup to origin: %w", err)
	}
	return Result{Info: "origin"}, nil
}
