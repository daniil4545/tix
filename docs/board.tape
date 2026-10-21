# Board picture for the README. Record: vhs docs/board.tape
# Needs the tix binary on PATH (make build, then PATH=$PWD/bin:$PATH) and jq.
# Everything runs in a throwaway HOME and TIX_HOME, never in the real ~/tasks.

Require tix
Require jq

Set Shell "bash"
Set FontSize 16
Set Width 1400
Set Height 380
Set Padding 16

Hide
Type "export SEED=$PWD/docs/board-seed.sh && export HOME=$(mktemp -d) && export TIX_HOME=$HOME/tasks && cd $HOME" Enter
Type "bash $SEED && export TIX_ACTOR=human && clear" Enter
Sleep 3s
Type "tix" Enter
Sleep 2s
Right
Right
Sleep 500ms
Show
Sleep 500ms
Screenshot assets/board.png
Hide
Type "q"
