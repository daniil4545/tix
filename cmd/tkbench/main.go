// Команда tkbench - замер скорости tk.
// Вызов из make bench: go run ./cmd/tkbench bin/tk (первый аргумент - путь к собранному tk).
// Код выхода 0, если цели раздела 7 плана выполнены.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"tracker/internal/gitx"
)

const (
	runTimeout = 30 * time.Second
	benchActor = "bench:1"
	// Задачи, на которых идёт замер: show и comment читают одну, правка мимо ядра - другую.
	targetTask   = 500
	externalTask = 600
	// moveTask лежит в колонке todo; move гоняет её между todo и doing, каждый вызов пишет коммит.
	moveTask = 701
	// takeFirst - первая задача todo; take берёт по одной новой на прогон (каждая пятая).
	takeFirst   = 6
	takeStride  = 5
	columnTodo  = "todo"
	columnDoing = "doing"
	// maxRuns держит take между первой todo-задачей и moveTask (701 = 6 + 5*139).
	maxRuns = 130
)

// bench - один замеряемый вызов tk и цель по медиане.
type bench struct {
	name     string
	targetMS float64
	args     []string
	env      []string             // добавка к окружению запуска
	argsOf   func(n int) []string // аргументы прогона n; вместо args, если задана
	before   func(n int) error    // правка перед каждым прогоном, не входит в время
	check    func(n int, out string) error
}

type sample struct {
	name        string
	median, p95 float64
	targetMS    float64
}

func main() {
	runs := flag.Int("n", 30, "runs per operation after warm-up")
	warm := flag.Int("warm", 3, "warm-up runs")
	tasks := flag.Int("tasks", 1000, "tasks in the store")
	keep := flag.Bool("keep", false, "keep the temporary TRACKER_HOME")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: tkbench [flags] <path to tk>")
		os.Exit(2)
	}
	tk, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "tkbench:", err)
		os.Exit(2)
	}
	ok, err := run(tk, *tasks, *runs, *warm, *keep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tkbench:", err)
		os.Exit(1)
	}
	if !ok {
		os.Exit(1)
	}
}

func run(tk string, tasks, runs, warm int, keep bool) (bool, error) {
	if tasks <= moveTask {
		return false, fmt.Errorf("need at least %d tasks", moveTask+1)
	}
	if warm+runs > maxRuns {
		return false, fmt.Errorf("at most %d runs per operation: take needs a new task each run", maxRuns)
	}
	home, err := os.MkdirTemp("", "tkbench-")
	if err != nil {
		return false, err
	}
	if keep {
		fmt.Fprintln(os.Stderr, "TRACKER_HOME:", home)
	} else {
		defer func() { _ = os.RemoveAll(home) }()
	}
	t0 := time.Now()
	if err := seed(tk, home, tasks); err != nil {
		return false, fmt.Errorf("store setup: %w", err)
	}
	fmt.Fprintf(os.Stderr, "store: %d tasks in %s\n", tasks, time.Since(t0).Round(time.Millisecond))

	var samples []sample
	pass := true
	for _, b := range append(benches(home, tasks), tuiBenches()...) {
		s, err := measure(tk, home, b, runs, warm)
		if err != nil {
			return false, fmt.Errorf("%s: %w", b.name, err)
		}
		samples = append(samples, s)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "operation\tmedian, ms\tp95, ms\ttarget, ms\tverdict\n")
	for _, s := range samples {
		verdict := "ok"
		if s.median > s.targetMS {
			verdict, pass = "FAIL", false
		}
		_, _ = fmt.Fprintf(tw, "%s\t%.1f\t%.1f\t%.0f\t%s\n", s.name, s.median, s.p95, s.targetMS, verdict)
	}
	return pass, tw.Flush()
}

func benches(home string, tasks int) []bench {
	id := fmt.Sprintf("T-%d", targetTask)
	ext := filepath.Join(home, taskFile(externalTask))
	return []bench{
		{
			name: "list", targetMS: 15, args: []string{"list"},
			check: func(_ int, out string) error {
				if n := strings.Count(out, "\n"); n < tasks {
					return fmt.Errorf("%d lines in reply, expected at least %d", n, tasks)
				}
				return nil
			},
		},
		{
			name: "show", targetMS: 15, args: []string{"show", id},
			check: func(_ int, out string) error {
				if !strings.HasPrefix(out, "rev ") {
					return fmt.Errorf("reply does not start with rev: %.40q", out)
				}
				return nil
			},
		},
		{
			name: "ctx", targetMS: 15, args: []string{"ctx"},
			check: func(_ int, out string) error {
				if !strings.HasPrefix(out, "project ") {
					return fmt.Errorf("reply does not start with project: %.40q", out)
				}
				return nil
			},
		},
		{
			name: "take", targetMS: 50,
			argsOf: func(n int) []string { return []string{"take", taskID(takeFirst + takeStride*n)} },
			check: func(n int, out string) error {
				return expectMove(out, taskID(takeFirst+takeStride*n), columnDoing)
			},
		},
		{
			name: "move", targetMS: 50,
			argsOf: func(n int) []string { return []string{"move", taskID(moveTask), moveTarget(n)} },
			check:  func(n int, out string) error { return expectMove(out, taskID(moveTask), moveTarget(n)) },
		},
		{
			name: "comment", targetMS: 50, args: []string{"comment", id, "speed measurement"},
			check: expectOK,
		},
		{
			name: "comment after outside edit", targetMS: 70, args: []string{"comment", id, "speed measurement"},
			before: func(n int) error { return editOutside(ext, n) },
			check:  expectOK,
		},
	}
}

func expectOK(_ int, out string) error {
	if !strings.HasPrefix(out, "ok ") {
		return fmt.Errorf("reply does not start with ok: %.60q", out)
	}
	return nil
}

func taskID(n int) string { return fmt.Sprintf("T-%d", n) }

// moveTarget чередует колонки: чётный прогон - в doing, нечётный - обратно в todo.
func moveTarget(n int) string {
	if n%2 == 0 {
		return columnDoing
	}
	return columnTodo
}

// expectMove требует ответ о настоящем переносе: повтор без изменения коммита не пишет.
func expectMove(out, id, column string) error {
	if !strings.HasPrefix(out, "ok "+id+" ") || !strings.Contains(out, "> "+column) {
		return fmt.Errorf("reply is not a move of %s to %s: %.60q", id, column, out)
	}
	return nil
}

// measure гонит прогрев, затем runs замеров; возвращает медиану и p95.
func measure(tk, home string, b bench, runs, warm int) (sample, error) {
	times := make([]float64, 0, runs)
	for i := 0; i < warm+runs; i++ {
		if b.before != nil {
			if err := b.before(i); err != nil {
				return sample{}, err
			}
		}
		args := b.args
		if b.argsOf != nil {
			args = b.argsOf(i)
		}
		d, out, err := timeRun(tk, home, args, b.env)
		if err != nil {
			return sample{}, err
		}
		if err := b.check(i, out); err != nil {
			return sample{}, err
		}
		if i >= warm {
			times = append(times, float64(d.Microseconds())/1000)
		}
	}
	sort.Float64s(times)
	return sample{
		name:     b.name,
		median:   percentile(times, 0.5),
		p95:      percentile(times, 0.95),
		targetMS: b.targetMS,
	}, nil
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if q == 0.5 && len(sorted)%2 == 0 {
		return (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	i := int(q*float64(len(sorted))+0.5) - 1
	return sorted[min(max(i, 0), len(sorted)-1)]
}

// tkCmd готовит запуск tk в базе home; cwd = home, чтобы Detect не сузил list до проекта.
func tkCmd(ctx context.Context, tk, home string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, tk, args...) //nolint:gosec // tk - аргумент командной строки bench
	cmd.Dir = home
	cmd.Env = append(cleanEnv(), "TRACKER_HOME="+home, "TRACKER_ACTOR="+benchActor)
	return cmd
}

// cleanEnv убирает переменные сессии агента: личность замера не зависит от окна запуска.
func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=") || strings.HasPrefix(kv, "CODEX_THREAD_ID=") ||
			strings.HasPrefix(kv, "TRACKER_ACTOR=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func timeRun(tk, home string, args, env []string) (time.Duration, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := tkCmd(ctx, tk, home, args)
	cmd.Env = append(cmd.Env, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	start := time.Now()
	err := cmd.Run()
	d := time.Since(start)
	if err != nil {
		return 0, "", fmt.Errorf("tk %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return d, out.String(), nil
}

// editOutside правит файл в обход ядра (как Obsidian): строка в теле перед комментариями.
func editOutside(path string, n int) error {
	data, err := os.ReadFile(path) //nolint:gosec // путь внутри временной базы
	if err != nil {
		return err
	}
	marker := "\n## Comments\n"
	line := fmt.Sprintf("\noutside edit %d\n", n)
	i := bytes.Index(data, []byte(marker))
	if i < 0 {
		return fmt.Errorf("%s: no comments section", path)
	}
	out := append(append(append([]byte{}, data[:i]...), line...), data[i:]...)
	return os.WriteFile(path, out, 0o600) //nolint:gosec // путь внутри временной базы
}

// seed строит базу: первую задачу создаёт сам tk (папка, git, config, .gitignore),
// остальные пишутся файлами и уходят одним коммитом; затем история из комментариев.
func seed(tk, home string, tasks int) error {
	if _, _, err := timeRun(tk, home, []string{"new", "bench seed"}, nil); err != nil {
		return err
	}
	bin, err := gitx.Resolve(os.Getenv("TRACKER_GIT"))
	if err != nil {
		return err
	}
	repo := gitx.Repo{Dir: home, Bin: bin}
	rnd := rand.New(rand.NewSource(1)) //nolint:gosec // детерминированные данные замера
	contents := make(map[int]string, tasks)
	paths := make([]string, 0, tasks)
	for id := 2; id <= tasks; id++ {
		contents[id] = taskContent(rnd, id)
		p := taskFile(id)
		if err := os.WriteFile(filepath.Join(home, p), []byte(contents[id]), 0o600); err != nil {
			return err
		}
		paths = append(paths, p)
	}
	if _, err := repo.Commit(paths, "bench: seed", benchActor); err != nil {
		return err
	}
	for i := 0; i < tasks/5; i++ {
		id := 2 + rnd.Intn(tasks-1)
		contents[id] += fmt.Sprintf("\n<!-- c: 2026-10-20T10:%02d | %s -->\nhistory %d\n", i%60, benchActor, i)
		p := taskFile(id)
		if err := os.WriteFile(filepath.Join(home, p), []byte(contents[id]), 0o600); err != nil {
			return err
		}
		if _, err := repo.Commit([]string{p}, fmt.Sprintf("T-%d: comment", id), benchActor); err != nil {
			return err
		}
	}
	changes, err := repo.Status()
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		return fmt.Errorf("foreign edits in the store after setup: %s", changes[0].Path)
	}
	return nil
}

func taskFile(id int) string { return fmt.Sprintf("T-%d bench-%d.md", id, id) }

var words = strings.Fields("core write commit lock task agent column board card check merge file git frontmatter comment criterion result summary stream")

func para(r *rand.Rand, n int) string {
	p := make([]string, n)
	for i := range p {
		p[i] = words[r.Intn(len(words))]
	}
	return strings.Join(p, " ")
}

// taskContent строит файл около 4 КБ по разделу 5 спеки: frontmatter, тело, комментарии.
func taskContent(r *rand.Rand, id int) string {
	cols := []string{"idea", "todo", "doing", "blocked", "done"}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: T-%d\ntitle: Task number %d about the core\nproject: proj%d\nstatus: %s\nrank: %d\ncreated: 2026-10-01T09:00:00\n",
		id, id, id%12, cols[id%5], id)
	fmt.Fprintf(&b, "external: \"\"\nwaiting: \"\"\nclaim: \"\"\ndue: \"\"\ntags: [a, b]\nask: \"\"\n---\n")
	fmt.Fprintf(&b, "## Abstract\n\n%s.\n\n## Summary\n\n%s.\n\n## Details\n\n", para(r, 30), para(r, 12))
	for i := 0; i < 4+r.Intn(4); i++ {
		fmt.Fprintf(&b, "- [ ] %s\n", para(r, 10))
	}
	fmt.Fprintf(&b, "\n%s.\n\n%s.\n\n## Comments\n\n", para(r, 60), para(r, 40))
	for i := 0; i < 2+r.Intn(5); i++ {
		fmt.Fprintf(&b, "<!-- c: 2026-10-%02dT10:%02d | claude -->\n%s.\n\n", 1+r.Intn(20), r.Intn(60), para(r, 25))
	}
	return b.String()
}
