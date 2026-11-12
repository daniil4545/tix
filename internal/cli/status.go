package cli

import (
	"strconv"
	"text/tabwriter"
)

// cmdStatus: `tix status` prints the statuses, add/rename/rm change the list (spec 0.9.0 2.3).
func (a *app) cmdStatus(args []string) error {
	p, err := parseArgs(args, withCommon(flagSpec{"kind": true}))
	if err != nil {
		return err
	}
	if len(p.pos) == 0 {
		return a.listStatuses()
	}
	want := map[string]int{"add": 2, "rename": 3, "rm": 2}[p.pos[0]]
	if want == 0 {
		return inputf("unknown subcommand %q; usage: %s", p.pos[0], commandHelp["status"])
	}
	if len(p.pos) != want || (p.pos[0] == "add") != p.set["kind"] {
		return inputf("usage: %s", commandHelp["status"])
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	var line, commit string
	switch p.pos[0] {
	case "add":
		r, err := a.store.AddStatus(p.pos[1], p.vals["kind"], who)
		if err != nil {
			return err
		}
		line, commit = "ok status add "+p.pos[1]+" "+p.vals["kind"], r.Commit
	case "rename":
		r, err := a.store.RenameStatus(p.pos[1], p.pos[2], who)
		if err != nil {
			return err
		}
		line, commit = "ok status rename "+p.pos[1]+" "+p.pos[2], r.Commit
	default:
		r, err := a.store.RemoveStatus(p.pos[1], who)
		if err != nil {
			return err
		}
		line, commit = "ok status rm "+p.pos[1], r.Commit
	}
	if p.set["v"] && commit != "" {
		line += " " + commit
	}
	a.out("%s", line)
	return nil
}

func (a *app) listStatuses() error {
	if err := a.open(); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 0, 2, ' ', 0)
	for _, s := range a.store.Statuses() {
		_, _ = tw.Write([]byte(s.Name + "\t" + s.Kind + "\t" + strconv.Itoa(s.Tasks) + "\n"))
	}
	return tw.Flush()
}
