package cli

import (
	"strings"
)

// flagSpec: flag name without dashes, true - the flag takes a value.
type flagSpec map[string]bool

// parsed - positional arguments and flags of a command.
type parsed struct {
	pos  []string
	vals map[string]string
	set  map[string]bool
}

// commonFlags exist on every command: -v and --as.
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

// parseArgs accepts flags anywhere; "--" ends the flags,
// so text may start with a dash.
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
			return nil, inputf("unknown flag %s", arg)
		}
		if !takes {
			if hasVal {
				return nil, inputf("flag %s takes no value", arg)
			}
			p.set[name] = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, inputf("flag %s needs a value", arg)
			}
			i++
			val = args[i]
		}
		p.vals[name] = val
		p.set[name] = true
	}
	return p, nil
}
