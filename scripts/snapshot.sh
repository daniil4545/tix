#!/usr/bin/env bash
# Builds the public snapshot of the working tree from a whitelist.
#
#   snapshot.sh <dir>
#
# The folder must be empty or absent. Only whitelisted files are copied; internal
# docs and service folders stay out. The file list is printed at the end.
set -euo pipefail

[ $# -eq 1 ] || { printf 'usage: %s <dir>\n' "$0" >&2; exit 2; }
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
DEST=$1
mkdir -p "$DEST"
DEST=$(cd "$DEST" && pwd -P)
[ -z "$(ls -A "$DEST")" ] || { printf 'err: %s is not empty\n' "$DEST" >&2; exit 1; }
case $DEST/ in "$ROOT"/*) printf 'err: dir must be outside the repository\n' >&2; exit 1 ;; esac

# Whitelist: whole folders, single files. Everything else stays out:
# .claude, .env*, AGENTS.md, CLAUDE.md, docs/state.md, docs/plans/, docs/research/,
# docs/specs/, docs/spec.md, docs/findings.jsonl, docs/mockup/, docs/api/.
DIRS=(cmd internal e2e connectors scripts .github assets)
FILES=(README.md LICENSE CHANGELOG.md .editorconfig .gitattributes .gitignore
  .golangci.yml .goreleaser.yaml Makefile go.mod go.sum
  docs/architecture.md docs/demo.tape docs/board.tape docs/board-seed.sh)

cd "$ROOT"
for f in "${FILES[@]}"; do
  [ -f "$f" ] || { printf 'warn: %s missing\n' "$f" >&2; continue; }
  mkdir -p "$DEST/$(dirname "$f")"
  cp -p "$f" "$DEST/$f"
done
for d in "${DIRS[@]}"; do
  [ -d "$d" ] || continue
  mkdir -p "$DEST/$d"
  cp -pR "$d/." "$DEST/$d/"
done
find "$DEST" -name .DS_Store -delete

# The snapshot script itself does not go public.
rm -f "$DEST/scripts/snapshot.sh"

(cd "$DEST" && find . -type f | sort)
