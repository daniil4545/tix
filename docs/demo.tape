# Demo for the README. Record: vhs docs/demo.tape
# Needs the tix binary with the board on PATH (go build -o bin/tix ./cmd/tix, then PATH=$PWD/bin:$PATH).
# Everything runs in a throwaway HOME and TIX_HOME, never in the real ~/tasks.
Output assets/demo.gif

Require tix

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 700
Set Padding 16
Set TypingSpeed 40ms

Hide
Type "unset CLAUDE_CODE_SESSION_ID CODEX_THREAD_ID TIX_MODEL && export HOME=$(mktemp -d) && export TIX_HOME=$HOME/tasks && export TIX_ACTOR=claude:demo" Enter
Type "mkdir -p $HOME/shop/.git && cd $HOME/shop && clear" Enter
Show

Type "tix new 'Add retry to the sync job'" Enter
Sleep 600ms
Type "tix new 'Fix the cart total rounding'" Enter
Sleep 600ms
Type "tix new 'Write the release notes'" Enter
Sleep 600ms

Hide
Type "printf '## Details\n- [ ] Failed sync retries 3 times\n' | tix edit 1 --base $(tix show 1 --json | jq -r .rev) && tix move 1 todo && clear" Enter
Show

Type "tix take 1" Enter
Sleep 800ms
Type "tix note 1 'Added backoff helper'" Enter
Sleep 600ms
Type "tix ctx" Enter
Sleep 2s
Type "tix list" Enter
Sleep 2s

Type "tix" Enter
Sleep 2s
Right
Sleep 600ms
Right
Sleep 600ms
Enter
Sleep 3s
Escape
Sleep 800ms
Type "q"
Sleep 500ms
