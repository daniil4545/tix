#!/usr/bin/env bash
# Seeds a throwaway store for the README board picture (docs/board.tape). Never run it on a real store.
set -euo pipefail
unset CLAUDE_CODE_SESSION_ID CODEX_THREAD_ID TIX_MODEL
export TIX_ACTOR=human
crit() { # id, criteria lines
  local id=$1; shift
  printf '## Details\n\n' >"$TIX_HOME/.body"
  for c in "$@"; do printf -- '- [ ] %s\n' "$c" >>"$TIX_HOME/.body"; done
  tix edit "$id" --base "$(tix show "$id" --json | jq -r .rev)" <"$TIX_HOME/.body" >/dev/null
  rm -f "$TIX_HOME/.body"
}
tix new "Retry the sync job" --project shop >/dev/null
tix new "Cart total rounding" --project shop >/dev/null
tix new "Orders schema migration" --project api >/dev/null
tix new "Pick a payment provider" --project shop >/dev/null
tix new "Release notes for 1.2" --project api >/dev/null
tix new "Dark mode in settings" --project shop >/dev/null
tix new "Rate limit the API" --project api >/dev/null
tix new "CI on macOS and Linux" --project api >/dev/null
crit 1 "Failed sync retries 3 times" "Backoff doubles up to 1 minute" "Alert after the last retry"
crit 2 "Totals match the invoice to the cent" "Test for 3 discounts in a row"
crit 3 "Orders table has the new columns" "Backfill runs in batches"
crit 4 "Fees compared for 3 providers"
crit 5 "Notes cover every merged change"
crit 8 "Both runners green on main"
for id in 1 2 3 4 5 8; do tix move "$id" todo >/dev/null; done
tix take 1 --as claude:7f3a9c21 >/dev/null
tix note 1 "Added backoff helper" --as claude:7f3a9c21 >/dev/null
tix check 1 1 --as claude:7f3a9c21 >/dev/null
tix take 3 --as codex:0b5e2d44 >/dev/null
tix note 3 "Columns added" --as codex:0b5e2d44 >/dev/null
tix check 3 1 --as codex:0b5e2d44 >/dev/null
tix take 4 --as claude:c19d0e85 >/dev/null
tix set 4 ask="Stripe or Adyen?" --as claude:c19d0e85 >/dev/null
tix take 8 --as codex:0b5e2d44 >/dev/null
tix check 8 1 --as codex:0b5e2d44 >/dev/null
tix "done" 8 -m "Both runners green" --as codex:0b5e2d44 >/dev/null
tix set 3 ask="review: backfill plan in PR 41" --as codex:0b5e2d44 >/dev/null
