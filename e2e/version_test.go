package e2e

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func runVersion(t *testing.T, bin string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), bin, args...).Output()
	if err != nil {
		t.Fatalf("%s %v: %v", bin, args, err)
	}
	return string(out)
}

func TestVersion_NoLdflags_PrintsDev(t *testing.T) {
	if got := runVersion(t, tixBin, "--version"); got != "dev\n" {
		t.Errorf("--version without ldflags = %q, want %q", got, "dev\n")
	}
	// The command prints the same and exits 0 (checked by the Output error above).
	if got := runVersion(t, tixBin, "version"); got != "dev\n" {
		t.Errorf("version without ldflags = %q, want %q", got, "dev\n")
	}
}

func TestVersion_Ldflags_PrintsValue(t *testing.T) {
	root, _ := filepath.Abs("..")
	bin := filepath.Join(t.TempDir(), "tix")
	build := exec.CommandContext(context.Background(), "go", "build", "-ldflags", "-X main.version=1.2.3", "-o", bin, "./cmd/tix")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if got := runVersion(t, bin, "--version"); got != "1.2.3\n" {
		t.Errorf("--version with ldflags = %q, want %q", got, "1.2.3\n")
	}
	if got := runVersion(t, bin, "version"); got != "1.2.3\n" {
		t.Errorf("version with ldflags = %q, want %q", got, "1.2.3\n")
	}
}
