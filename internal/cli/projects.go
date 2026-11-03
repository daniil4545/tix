package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"
)

// cmdProjects: `tix projects` prints the catalog, `tix projects add NAME [PATH]` registers a project.
func (a *app) cmdProjects(args []string) error {
	p, err := parseArgs(args, withCommon(nil))
	if err != nil {
		return err
	}
	switch {
	case len(p.pos) == 0:
		return a.listProjects()
	case p.pos[0] != "add":
		return inputf("unknown subcommand %q; usage: %s", p.pos[0], commandHelp["projects"])
	case len(p.pos) < 2 || len(p.pos) > 3:
		return inputf("usage: %s", commandHelp["projects"])
	}
	name, path := p.pos[1], ""
	if len(p.pos) == 3 {
		path = p.pos[2]
		if !filepath.IsAbs(path) {
			return inputf("project path %q is not absolute", path)
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return inputf("project path %q is not an existing folder", path)
		}
	}
	who, err := a.actorOf(p)
	if err != nil {
		return err
	}
	if err := a.open(); err != nil {
		return err
	}
	res, err := a.store.SetProjectPath(name, path, who)
	if err != nil {
		return err
	}
	line := "ok project " + name
	if p.set["v"] && res.Commit != "" {
		line += " " + res.Commit
	}
	a.out("%s", line)
	return nil
}

func (a *app) listProjects() error {
	if err := a.open(); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(a.env.Stdout, 0, 0, 2, ' ', 0)
	for _, e := range a.store.Catalog() {
		path, note := e.Path, ""
		if path == "" {
			path = "-"
		}
		if !e.Known {
			note = "not in catalog"
		}
		_, _ = tw.Write([]byte(e.Name + "\t" + path + "\t" + strconv.Itoa(e.Tasks) + "\t" + note + "\n"))
	}
	return tw.Flush()
}
