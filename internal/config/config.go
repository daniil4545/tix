// Package config reads and prints .tix/config.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// The five kinds of a status (spec 0.9.0 2.1). Until slice 4 each is also the name of a default status.
const (
	Idea       = "idea"
	Open       = "open"
	InProgress = "in_progress"
	Blocked    = "blocked"
	Closed     = "closed"
)

// Kinds - the five kinds in board order.
var Kinds = []string{Idea, Open, InProgress, Blocked, Closed}

// Statuses - the names of the default statuses in board order. Wrapper until slice 4.
var Statuses = Kinds

// Status - a name and its kind, one [[status]] section.
type Status struct {
	Name string `toml:"name"`
	Kind string `toml:"kind"`
}

var reName = regexp.MustCompile(`^[a-z0-9_]{1,20}$`)

func defaultStatuses() []Status {
	l := make([]Status, len(Kinds))
	for i, k := range Kinds {
		l[i] = Status{k, k}
	}
	return l
}

// legacy - the words of the former columns and roles and the status each one is now.
var legacy = map[string]string{
	"inbox": Idea, "ready": Open, "active": InProgress, "waiting": Blocked,
	"todo": Open, "doing": InProgress, "done": Closed, "closed": Closed,
}

// Check returns nil for one of the five statuses, else an error that names them; an old
// column or role word gets the hint "maybe open?".
func Check(name string) error { return checkIn(Statuses, name) }

func checkIn(names []string, name string) error {
	if slices.Contains(names, name) {
		return nil
	}
	list := strings.Join(names, " ")
	if to, ok := legacy[name]; ok && slices.Contains(names, to) {
		return fmt.Errorf("unknown status %q: maybe %s? statuses: %s", name, to, list)
	}
	return fmt.Errorf("unknown status %q; statuses: %s", name, list)
}

// Config - the content of config.toml: folder=project.
type Config struct {
	Projects map[string]string
	Paths    map[string]string // project name (lower case) = absolute path; "" - a project with no path
	Format   int               // store format; 0 - not set
	statuses []Status          // nil - no [[status]] in the file: the default five, not printed
	extra    map[string]any    // foreign sections, kept as they are on Render
}

var reLine = regexp.MustCompile(`^toml: line (\d+)[^:]*: `)

// file - the shape of config.toml on disk. Foreign sections (adapters and the old [[column]]) are skipped.
type file struct {
	Projects map[string]string `toml:"projects"`
	Paths    map[string]string `toml:"paths,omitempty"`
	Format   int               `toml:"format"`
	Status   []Status          `toml:"status"`
}

// Default returns the config for the first write.
func Default() *Config { return &Config{Projects: map[string]string{}, Paths: map[string]string{}} }

// Load reads the file. No file - Default. The error carries the path and line.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the core sets the path, not input
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	var f file
	if _, err := toml.Decode(string(data), &f); err != nil {
		line, msg := 1, err.Error()
		var perr toml.ParseError
		if errors.As(err, &perr) {
			line, msg = perr.Position.Line, perr.Message
		} else if m := reLine.FindStringSubmatch(msg); m != nil {
			// value type error: "toml: line 2 (last key ...): <reason>"
			line, _ = strconv.Atoi(m[1])
			msg = msg[len(m[0]):]
		}
		return nil, fmt.Errorf("%s:%d: %s", path, line, msg)
	}
	c := build(f)
	if err := c.validate(path, string(data)); err != nil {
		return nil, err
	}
	var all map[string]any
	if _, err := toml.Decode(string(data), &all); err == nil {
		for k, v := range all {
			if k != "projects" && k != "paths" && k != "column" && k != "status" && k != "format" { // [[column]] is the old shape, migrated away
				if c.extra == nil {
					c.extra = map[string]any{}
				}
				c.extra[k] = v
			}
		}
	}
	return c, nil
}

func build(f file) *Config {
	c := &Config{Projects: f.Projects, Paths: f.Paths, Format: f.Format, statuses: f.Status}
	if c.Projects == nil {
		c.Projects = map[string]string{}
	}
	if c.Paths == nil {
		c.Paths = map[string]string{}
	}
	for k, v := range c.Paths { // a hand-edited key keeps working whatever its case
		if l := strings.ToLower(k); l != k {
			delete(c.Paths, k)
			c.Paths[l] = v
		}
	}
	return c
}

// validate checks the status list (spec 2.1). The line is that of the [[status]] section at fault.
func (c *Config) validate(path, text string) error {
	var lines []int // line of each [[status]] section
	for i, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "[[status]]" {
			lines = append(lines, i+1)
		}
	}
	fail := func(i int, format string, a ...any) error {
		line := 1
		if i < len(lines) {
			line = lines[i]
		}
		return fmt.Errorf("%s:%d: %s", path, line, fmt.Sprintf(format, a...))
	}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	for i, s := range c.statuses {
		switch {
		case !reName.MatchString(s.Name):
			return fail(i, "status name %q: use a-z, 0-9, _, up to 20 characters", s.Name)
		case seen[s.Name]:
			return fail(i, "status %q is listed twice", s.Name)
		case !slices.Contains(Kinds, s.Kind):
			return fail(i, "status %q: unknown kind %q; kinds: %s", s.Name, s.Kind, strings.Join(Kinds, " "))
		case slices.Contains(Kinds, s.Name) && s.Name != s.Kind:
			return fail(i, "status %q is a kind word, it can only have the kind %s", s.Name, s.Name)
		}
		seen[s.Name], kinds[s.Kind] = true, true
	}
	if len(c.statuses) == 0 {
		return nil
	}
	for _, k := range Kinds {
		if !kinds[k] {
			return fail(0, "no status of the kind %s", k)
		}
	}
	return nil
}

// StatusList returns the statuses in config order: the default five when the file has none.
func (c *Config) StatusList() []Status {
	if len(c.statuses) == 0 {
		return defaultStatuses()
	}
	return slices.Clone(c.statuses)
}

// KindOf returns the kind of a status; false if there is no such status.
func (c *Config) KindOf(name string) (string, bool) {
	for _, s := range c.StatusList() {
		if s.Name == name {
			return s.Kind, true
		}
	}
	return "", false
}

// DefaultOf returns the first status of a kind; "" if the kind is unknown.
func (c *Config) DefaultOf(kind string) string {
	for _, s := range c.StatusList() {
		if s.Kind == kind {
			return s.Name
		}
	}
	return ""
}

// CheckStatus returns nil for a configured status name, else an error that lists them.
func (c *Config) CheckStatus(name string) error {
	l := c.StatusList()
	names := make([]string, len(l))
	for i, s := range l {
		names[i] = s.Name
	}
	return checkIn(names, name)
}

// Render prints config.toml.
func (c *Config) Render() []byte {
	var b bytes.Buffer
	enc := toml.NewEncoder(&b)
	enc.Indent = ""
	// one map, one Encode: a top-level scalar from a foreign section must not land under [paths]
	all := map[string]any{"projects": c.Projects}
	if len(c.Paths) > 0 {
		all["paths"] = c.Paths
	}
	if c.Format != 0 {
		all["format"] = c.Format
	}
	if len(c.statuses) > 0 {
		all["status"] = c.statuses
	}
	for k, v := range c.extra {
		all[k] = v
	}
	if err := enc.Encode(all); err != nil {
		panic(fmt.Sprintf("config: render: %v", err)) // decoded from toml, so it encodes back
	}
	return b.Bytes()
}

// LegacyMap reads the old [[column]] sections and maps old status to new by column role.
// has: sections found. A column with no known role is absent; no sections - the default names.
func LegacyMap(text string) (m map[string]string, has bool, err error) {
	var f struct {
		Column []struct{ Name, Role string } `toml:"column"`
	}
	if _, err := toml.Decode(text, &f); err != nil {
		return nil, false, err
	}
	m = map[string]string{"idea": Idea, "todo": Open, "doing": InProgress, "blocked": Blocked, "done": Closed}
	if len(f.Column) == 0 {
		return m, false, nil
	}
	m = map[string]string{}
	for _, c := range f.Column {
		if to, ok := legacy[c.Role]; ok {
			m[c.Name] = to
		}
	}
	return m, true, nil
}
