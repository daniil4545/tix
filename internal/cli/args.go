package cli

import (
	"errors"
	"fmt"
	"strings"
)

// flagSpec: имя флага без дефисов, true - флаг берёт значение.
type flagSpec map[string]bool

// parsed - позиционные аргументы и флаги команды.
type parsed struct {
	pos  []string
	vals map[string]string
	set  map[string]bool
}

// commonFlags есть у каждой команды: -v и --as.
var commonFlags = flagSpec{"v": false, "as": true}

func withCommon(extra flagSpec) flagSpec {
	spec := flagSpec{}
	for k, v := range commonFlags {
		spec[k] = v
	}
	for k, v := range extra {
		spec[k] = v
	}
	return spec
}

// parseArgs принимает флаги в любом месте; "--" заканчивает флаги,
// так текст может начинаться с дефиса.
func parseArgs(args []string, spec flagSpec) (*parsed, error) {
	p := &parsed{vals: map[string]string{}, set: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			p.pos = append(p.pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			p.pos = append(p.pos, arg)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		takes, known := spec[name]
		if !known {
			return nil, fmt.Errorf("unknown flag %s", arg)
		}
		if !takes {
			if hasVal {
				return nil, fmt.Errorf("flag %s takes no value", arg)
			}
			p.set[name] = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag %s needs a value", arg)
			}
			i++
			val = args[i]
		}
		p.vals[name] = val
		p.set[name] = true
	}
	return p, nil
}

// actor возвращает автора записи; без --as - unknown.
// Запрещённое ломает маркер комментария и трейлер коммита.
func (p *parsed) actor() (string, error) {
	if !p.set["as"] {
		return "unknown", nil
	}
	a := p.vals["as"]
	if strings.TrimSpace(a) == "" || strings.ContainsAny(a, "|\r\n") || strings.Contains(a, "-->") {
		return "", errors.New("bad --as: empty, or contains |, --> or a line break")
	}
	return a, nil
}
