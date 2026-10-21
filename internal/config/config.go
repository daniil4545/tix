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

// Column - a column with a role.
type Column struct{ Name, Role string }

// Config - the content of config.toml: columns and folder=project.
type Config struct {
	Columns  []Column
	Projects map[string]string
}

// roles - the five column roles (spec 2.5).
var roles = map[string]bool{"inbox": true, "ready": true, "active": true, "waiting": true, "closed": true}

var reLine = regexp.MustCompile(`^toml: line (\d+)[^:]*: `)

// file - the shape of config.toml on disk. Foreign sections (adapters and others) are skipped.
type file struct {
	Column   []fileColumn      `toml:"column"`
	Projects map[string]string `toml:"projects"`
}

type fileColumn struct {
	Name string `toml:"name"`
	Role string `toml:"role,omitempty"`
}

// Default returns the config for the first write.
func Default() *Config {
	return &Config{
		Columns: []Column{
			{"idea", "inbox"}, {"todo", "ready"}, {"doing", "active"},
			{"blocked", "waiting"}, {"done", "closed"},
		},
		Projects: map[string]string{},
	}
}

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
	return build(path, string(data), f)
}

func build(path, text string, f file) (*Config, error) {
	c := &Config{Projects: f.Projects}
	if c.Projects == nil {
		c.Projects = map[string]string{}
	}
	if len(f.Column) == 0 {
		c.Columns = Default().Columns
		return c, nil
	}
	names, used := map[string]bool{}, map[string]bool{}
	for _, col := range f.Column {
		switch {
		case col.Name == "":
			return nil, fail(path, text, "[[column]]", "column has no name")
		case names[col.Name]:
			return nil, fail(path, text, col.Name, "column %q is repeated", col.Name)
		case col.Role != "" && !roles[col.Role]:
			return nil, fail(path, text, col.Role, "unknown role %q on column %q", col.Role, col.Name)
		case col.Role != "" && used[col.Role]:
			return nil, fail(path, text, col.Role, "role %q is taken: one role per column", col.Role)
		}
		names[col.Name] = true
		if col.Role != "" {
			used[col.Role] = true
		}
		c.Columns = append(c.Columns, Column(col))
	}
	return c, nil
}

// fail builds an error with the line of the last mention of needle in the text.
func fail(path, text, needle, format string, args ...any) error {
	line := 1
	for i, ln := range strings.Split(text, "\n") {
		if strings.Contains(ln, needle) {
			line = i + 1
		}
	}
	return fmt.Errorf("%s:%d: %s", path, line, fmt.Sprintf(format, args...))
}

// Render prints config.toml.
func (c *Config) Render() []byte {
	f := file{Projects: c.Projects}
	for _, col := range c.Columns {
		f.Column = append(f.Column, fileColumn(col))
	}
	var b bytes.Buffer
	enc := toml.NewEncoder(&b)
	enc.Indent = ""
	if err := enc.Encode(f); err != nil {
		panic(fmt.Sprintf("config: render: %v", err)) // the file types are fixed
	}
	return b.Bytes()
}

// ByRole returns the first column with role role.
func (c *Config) ByRole(role string) (Column, bool) {
	for _, col := range c.Columns {
		if col.Role == role {
			return col, true
		}
	}
	return Column{}, false
}

// Resolve returns a column name by name or by role: the column name wins over the role.
// Neither found - name as is, the column check gives the error.
func (c *Config) Resolve(name string) string {
	if name == "" || slices.ContainsFunc(c.Columns, func(col Column) bool { return col.Name == name }) {
		return name
	}
	if col, ok := c.ByRole(name); ok {
		return col.Name
	}
	return name
}

// Roles prints columns with roles: "idea=inbox todo=ready ...". Columns without a role are skipped.
func (c *Config) Roles() string {
	parts := make([]string, 0, len(c.Columns))
	for _, col := range c.Columns {
		if col.Role != "" {
			parts = append(parts, col.Name+"="+col.Role)
		}
	}
	return strings.Join(parts, " ")
}

// RoleOf returns the column role. Empty: no such column or it has no role.
func (c *Config) RoleOf(column string) string {
	for _, col := range c.Columns {
		if col.Name == column {
			return col.Role
		}
	}
	return ""
}
