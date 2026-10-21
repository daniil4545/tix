#!/usr/bin/env bash
# Собирает публичный снимок рабочего дерева по whitelist.
#
#   snapshot.sh <каталог>
#
# Каталог должен быть пустым или не существовать. Копируются только файлы из
# whitelist (design.md, раздел 8); внутренние документы и служебные каталоги
# не попадают в снимок. Список файлов печатается в конце.
set -euo pipefail

[ $# -eq 1 ] || { printf 'usage: %s <dir>\n' "$0" >&2; exit 2; }
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
DEST=$1
mkdir -p "$DEST"
DEST=$(cd "$DEST" && pwd -P)
[ -z "$(ls -A "$DEST")" ] || { printf 'err: %s is not empty\n' "$DEST" >&2; exit 1; }
case $DEST/ in "$ROOT"/*) printf 'err: dir must be outside the repository\n' >&2; exit 1 ;; esac

# Белый список: каталоги целиком, файлы по одному. Всё остальное не идёт:
# .claude, .env*, AGENTS.md, CLAUDE.md, docs/state.md, docs/backlog.md, docs/plans/,
# docs/research/, docs/specs/, docs/findings.jsonl, docs/mockup/, docs/api/.
DIRS=(cmd internal e2e connectors scripts .github assets)
FILES=(README.md LICENSE CHANGELOG.md .editorconfig .gitattributes .gitignore
  .golangci.yml .goreleaser.yaml Makefile go.mod go.sum
  docs/architecture.md docs/demo.tape)

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

# Скрипт снимка в публичный репозиторий не идёт.
rm -f "$DEST/scripts/snapshot.sh"

(cd "$DEST" && find . -type f | sort)
