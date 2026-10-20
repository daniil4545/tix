// Package config читает и печатает .tracker/config.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Column - колонка с ролью.
type Column struct{ Name, Role string }

// Config - содержимое config.toml: колонки и папка=проект.
type Config struct {
	Columns  []Column
	Projects map[string]string
}

// roles - пять ролей колонок (спека 2.5).
var roles = map[string]bool{"inbox": true, "ready": true, "active": true, "waiting": true, "closed": true}

var reLine = regexp.MustCompile(`^toml: line (\d+)[^:]*: `)

// file - вид config.toml на диске. Чужие разделы (adapters и другие) пропускаются.
type file struct {
	Column   []fileColumn      `toml:"column"`
	Projects map[string]string `toml:"projects"`
}

type fileColumn struct {
	Name string `toml:"name"`
	Role string `toml:"role,omitempty"`
}

// Default возвращает конфиг для первой записи.
func Default() *Config {
	return &Config{
		Columns: []Column{
			{"idea", "inbox"}, {"todo", "ready"}, {"doing", "active"},
			{"blocked", "waiting"}, {"done", "closed"},
		},
		Projects: map[string]string{},
	}
}

// Load читает файл. Нет файла - Default. Ошибка несёт путь и строку.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // путь задаёт ядро, не ввод
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
			// ошибка типа значения: "toml: line 2 (last key ...): <причина>"
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

// fail строит ошибку со строкой последнего упоминания needle в тексте.
func fail(path, text, needle, format string, args ...any) error {
	line := 1
	for i, ln := range strings.Split(text, "\n") {
		if strings.Contains(ln, needle) {
			line = i + 1
		}
	}
	return fmt.Errorf("%s:%d: %s", path, line, fmt.Sprintf(format, args...))
}

// Render печатает config.toml.
func (c *Config) Render() []byte {
	f := file{Projects: c.Projects}
	for _, col := range c.Columns {
		f.Column = append(f.Column, fileColumn(col))
	}
	var b bytes.Buffer
	enc := toml.NewEncoder(&b)
	enc.Indent = ""
	if err := enc.Encode(f); err != nil {
		panic(fmt.Sprintf("config: render: %v", err)) // типы файла фиксированы
	}
	return b.Bytes()
}
