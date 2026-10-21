# git: facts for the `tix` store

Date: 2026-10-20. Git 2.54.0 (Apple Git-157), macOS, M4, APFS. Measurements: 1000 files of 5 KB, 3000 commits, direct path `xcrun --find git`, n=25-30, median. The measurement scripts were not kept (session scratchpad), the facts are in this file. Sources: git-scm.com/docs, Documentation/ in github.com/git/git (master), go-git COMPATIBILITY.md.

## 1. Structure

- A commit of an edit writes 3 loose objects: blob, root tree (1000 names, `cat-file -s HEAD^{tree}` = 52 KB), commit. The index `.git/index` is read and written whole (`commit --only` touches 3 entries, trace2).
- A ref is a file `.git/refs/heads/main`; `pack-refs` (called by gc) removes it and moves it into `packed-refs` (measured). A ref write is atomic and checks the old value. Reflog: one line per commit, 90 days; turning it off does not change speed (10.84 vs 10.62 ms).

## 2. Locks (measured: an empty lock created beforehand, command run; git 2.54.0)

| Command | index.lock | HEAD.lock | refs/heads/main.lock | packed-refs.lock |
|---|---|---|---|---|
| `status` (including `--no-optional-locks`) | ok (optional) | ok | ok | ok |
| `add` | REFUSED (128) | ok | ok | ok |
| `commit --only` | REFUSED | REFUSED | REFUSED | ok |
| `write-tree` | REFUSED | ok | ok | ok |
| `hash-object -w`, `merge-file`, `log`, `ls-files`, `rev-parse`, `diff-files` | ok | ok | ok | ok |

- `hash-object -w` writes through `objects/xx/tmp_obj_*`, with no lock. `push` was not measured.
- `--no-optional-locks`: status does not take index.lock to refresh the stat cache. Trap: without an index write, every such status hashes the files with a changed stat again; a fresh copy of the repository: `status` 28.9 ms vs 5.9 after one normal one. `commit` refreshes the index.
- Removing a foreign lock (measured): A holds index.lock (pre-commit hook sleep 2), the lock is removed, B ran `add` and `commit`. A failed: `cannot lock ref 'HEAD': is at X but expected Y`, code 128; `fsck` is clean, the commit of A dangles, the edit is in the file. Without the commit of B, process A would have overwritten the index of B. The ref is protected by the old-value check, the index is not.

## 3. Failure

- `kill -9` at a random moment of `commit` (150 attempts): `fsck` 0 errors. Leftovers: `index.lock` 39 times, `next-index-<pid>.lock` 12 (both caught by the glob `.git/*.lock` in `DropGitLocks`), `tmp_obj_*` 17 files (cleaned by gc), a dangling blob. The edit stayed in the file.
- Power loss (git-config, core.fsync): the default is `committed,-loose-object`; loose objects, refs and the index are not synced; on macOS `core.fsyncMethod` defaults to `writeout-only` (the disk cache is not flushed). The last commits may be lost, and a ref may point to an empty loose object. Not reproduced.
- Cost: `core.fsync=all` 11.4 ms (+1.4); with `core.fsyncMethod=fsync` 27.8 (+17.7); `batch` 28.0. Do not enable for the MVP.
- Safe to repair automatically under flock: old lock files, `tmp_obj_*`, `rm .git/index` + `git reset -q` (the index is derived). Manually: a broken object, a ref rollback from the reflog, `gc --prune=now`, `reset --hard`.

## 4. Write speed (ms)

| Variant | Median | Conclusion |
|---|---|---|
| `commit --only -- file` (current) | 10.6 (p95 11.5) | baseline |
| the same without `maintenance.auto=false` | 15.6 | `commit` starts `git maintenance run --auto --quiet --detach` and waits for it (5.0 ms in trace2) |
| the same with `gc.auto=0` without `maintenance.auto=false` | 15.9 | `gc.auto=0` does not turn this off |
| `add` + `commit --only` | 16.4 | |
| plumbing: `update-index`, `write-tree`, `rev-parse`, `commit-tree`, `update-ref` (5 processes) | 24.6 | |
| plumbing, HEAD read from a file (4 processes) | 21.2 | each git process costs 3.0 ms (`--version`) |
| `index v4`, `feature.manyFiles`, `core.untrackedCache` | 11.0 / 10.4 / 11.3 | no gain on 1000 files |
| `core.logAllRefUpdates=false` | 10.8 | no gain |

- Breakdown of 10.6 ms: startup 3.0, the rest 7.6 (index, cache-tree, objects, ref). `fast-import` writes a pack on every run, not suitable.
- `status --porcelain -z --untracked-files=all` 5.9 (index refreshed); `ls-files -m -d -o --exclude-standard` 5.4 (does not see staged); the built-in `core.fsmonitor` 17.5 (worse).
- `log --grep '^T-12:'`: 17 ms (3000 commits, pack), 13 ms with commit-graph. `log -- file`: 37.5 ms without the graph, 28.5 with the graph, 8.4 with the `--changed-paths` graph (Bloom). `log -1 --grep` (the last commit of a task) 8 ms.

## 5. Growth (measured)

- Loose: 3000 commits = 9000 loose objects = 105 MB (35 KiB per commit, because of the root tree). 100k commits loose = about 3.5 GB and 300k files.
- After `git gc` (4.1 s for 6000 commits): `.git` 109 MB -> 5.8 MB. 100k commits, 1000 files in a pack (fast-import): 90 MB pack, `.git` 97-104 MB, refs in one pack.
- Reads before gc (6000 commits, 9000 loose): `log --grep` 84 ms, `log -- file` 641 ms, `rev-list --count` 106 ms. After gc: 29, 54, 6 ms.
- 100k commits in a pack: `log --grep` 0.9 s, `log -- file` 4.2 s without the graph, 0.43 s with `--changed-paths`, 0.66 s with the graph from `gc`; `log -1 --grep` 8 ms. A full graph rebuild takes 4.7 s. Without `-1`/`-n` the search is slow.
- `gc.auto=0 maintenance.auto=false` means: loose objects pile up forever, reads degrade 10 times. The default threshold `gc.auto`=6700 loose (documentation), about 2200 commits. gc in parallel with writes (measured: 1500 loose commits, gc, 33 commits during it): 0 failures, commit delay 27 ms (median), `fsck` clean. gc takes `gc.pid` itself; `pruneExpire` of 2 weeks protects the objects of parallel processes.
- What to do instead: our own counter in `.tix/` (every 200-500 writes) and, after the flock is released, a separate detached process `git gc --quiet`, then `git commit-graph write --reachable --changed-paths --split` (gc builds the graph without Bloom filters, `commitGraph.changedPaths=true` in config did not fix this, measured: 43 ms vs 15 ms after an explicit write).

## 6. go-git (v5.19.3, COMPATIBILITY.md, 2026-10-20)

- Present: commit, status, log, add (without flags), repack. Partial: merge (fast-forward only), worktree. Absent: gc, index v1/v3 (v2 only). `merge-file`, reftable, lock compatibility, commit-graph are not mentioned in the document.
- Measurement (same 1000/3000): open + `status` 22.1 ms; + add + commit 57.8 ms vs 16-17 ms for git. `fsck` clean.
- Conclusion: not worth it. 3 times slower, no merge-file and no gc, our own index and lock code next to the system git.

## 7. Reading HEAD without git

1. `<dir>/.git` may be a file `gitdir: <path>` (worktree, submodule).
2. `HEAD`: `ref: refs/heads/x` or hex (detached; sha256 gives 64 chars, measured).
3. Linked worktree: `.git/worktrees/<id>/HEAD`, the file `commondir` (`../..`) points to the common git dir with refs.
4. Ref: `<common>/refs/heads/x`; no file - a line `<sha> <name>` in `<common>/packed-refs` (after gc the file is absent, measured).
5. Reftable (`extensions.refStorage = reftable`): `HEAD` = `ref: refs/heads/.invalid`, refs in `.git/reftable/*.ref`, read only with `git rev-parse HEAD` (3.9 ms). The future default of Git 3.0 (BreakingChanges.adoc).
6. A ref changes by a whole rename; read once, and when in doubt use `git rev-parse`.
7. The current `looseHead` (`internal/gitx/gitx.go`) reads only items 2 and 4 (loose); `.git` as a file, packed-refs and reftable go to `rev-parse`. Speed is lost only in these cases.

## 8. Obsidian Git (out of scope)

The plugin calls the system git through simple-git: `status`, `add -A`, `commit` without `--no-optional-locks`, and takes index.lock (src/gitManager/simpleGit.ts, 2026-10-20).

## 9. Ready-made git features (second recon)

Date: 2026-10-20, git 2.54.0. Checked by running in temporary repositories, no speed measurements.
The versions of introduction come from the git-scm.com documentation from memory, not checked against the page (except 2.54).

| Feature | What it gives tix | Git | Checked |
|---|---|---|---|
| `git commit --trailer "Actor: x"` | trailers without hand-gluing the message (D56, D58) | 2.32 | yes |
| `%(trailers:key=Actor,valueonly)` in `log --format` | `Actor:` and `Code:` are read without parsing text | 2.22 | yes |
| `shortlog -s --group=trailer:Actor` | the number of edits of a session | 2.29 | yes |
| `log --grep A --grep B` (OR), `--all-match` (AND) | history of several ids, a filter by id and `Actor` | long ago | yes |
| `log -L ':## Comments:T-1.md'` + `diff.tix.xfuncname '^(## .*)$'` | history of one section of a card; attributes in `.git/info/attributes` and `.git/config`, not in the tree. The form `/re/,$` does not work, `:funcname:` is needed; the output is a patch, not a commit list | 1.8.4 | yes |
| `git last-modified` | the change date of all files in one call instead of `log -1` per file; a fallback path (mtime) is needed for old git | 2.52 | yes |
| `cat-file --batch` / `--batch-command` | one process for many blob reads; a missing object gives a `missing` line | 2.36 | yes |
| `merge-tree --write-tree` | merge of two commits without a working tree (`tix sync`, D57); a conflict gives code 1 and a list | 2.38 | yes |
| `.gitattributes *.md merge=union`, `merge-file --union` | appended comments merge without a conflict; union glues the body too, the line order is not guaranteed, a repeat gives duplicates | long ago | yes |
| `git bundle create/verify` | an offline copy and a transfer to a second machine without a remote | 1.5.1 | yes |
| plumbing `mktree`, `commit-tree`, `update-ref --stdin` | a commit without `index.lock`. Trap: the index goes stale, `status` shows `D T-3.md` as staged; `read-tree` or `update-index` is needed | long ago | yes, trap found |

Do not take: `rerere` (conflicts are rare and different); our own merge driver (an external binary breaks
the user's manual git); `git notes` for `Code:` (a trailer is simpler, notes do not travel on push without a refspec);
SSH commit signing (one machine, a key per agent is costly); author and committer instead of `Actor:` (a duplicate).

## 10. Attribution: the window between rename and commit

- Risk (risk check of S1, 2026-10-20): `commit --only -- <file>` takes the file from the working tree.
  A write that bypasses the core after rename and before commit lands in the commit under the agent's `Actor:`, not as an
  external edit. The data is intact, the author is replaced (violates D56).
- Fix (S1, `f9e88de`): commit exactly the bytes of the core. `hash-object -w` on the file,
  `update-index -z --index-info` (mode 0 removes a path), commit the index. It avoids the plumbing trap from
  section 9: the index is up to date, `git status` is clean. Code: `gitx.CommitFiles`
  (`internal/gitx/gitx.go`), test `TestStore_ExternalWriteBetweenRenameAndCommit_KeepsAuthor`,
  checked by the test and a live run. The variant `update-index --cacheinfo` was not chosen.
- Residual risk S1: a foreign `git add` between `update-index` and `commit` lands under the agent's `Actor:`.
  The decision on S1 belongs to the user.

## What this means for tix

- `commit --only` in one process is the best; plumbing does not speed it up.
- `maintenance.auto=false` is needed for speed, but our own maintenance is needed: gc and the `--changed-paths` graph.
- Auto-cleaning of lock files and `tmp_obj` is safe; do not enable fsync; do not take go-git.
- Trailers: `commit --trailer` and `%(trailers)` (S2). Dates: `last-modified`, section history:
  `log -L` (S4). After the MVP for `tix sync`: `merge-tree --write-tree`, `merge=union`, `bundle`.
