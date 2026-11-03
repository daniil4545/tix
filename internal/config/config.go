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

// The five fixed statuses of a task (spec 2.5): no roles, no columns in config.toml.
const (
	Idea       = "idea"
	Open       = "open"
	InProgress = "in_progress"
	Blocked    = "blocked"
	Closed     = "closed"
)

// Statuses - the statuses in board order.
var Statuses = []string{Idea, Open, InProgress, Blocked, Closed}

// legacy - the words of the former columns and roles and the status each one is now.
var legacy = map[string]string{
	"inbox": Idea, "ready": Open, "active": InProgress, "waiting": Blocked,
	"todo": Open, "doing": InProgress, "done": Closed, "closed": Closed,
}

// Check returns nil for one of the five statuses, else an error that names them; an old
// column or role word gets the hint "maybe open?".
func Check(name string) error {
	if slices.Contains(Statuses, name) {
		return nil
	}
	list := strings.Join(Statuses, " ")
	if to, ok := legacy[name]; ok {
		return fmt.Errorf("unknown status %q: maybe %s? statuses: %s", name, to, list)
	}
	return fmt.Errorf("unknown status %q; statuses: %s", name, list)
}

// Config - the content of config.toml: folder=project.
type Config struct {
	Projects map[string]string
	Paths    map[string]string // project name (lower case) = absolute path; "" - a project with no path
	extra    map[string]any    // foreign sections, kept as they are on Render
}

var reLine = regexp.MustCompile(`^toml: line (\d+)[^:]*: `)

// file - the shape of config.toml on disk. Foreign sections (adapters and the old [[column]]) are skipped.
type file struct {
	Projects map[string]string `toml:"projects"`
	Paths    map[string]string `toml:"paths,omitempty"`
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
	var all map[string]any
	if _, err := toml.Decode(string(data), &all); err == nil {
		for k, v := range all {
			if k != "projects" && k != "paths" && k != "column" { // [[column]] is the old shape, migrated away
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
	c := &Config{Projects: f.Projects, Paths: f.Paths}
	if c.Projects == nil {
		c.Projects = map[string]string{}
	}
	if c.Paths == nil {
		c.Paths = map[string]string{}
	}
	return c
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
