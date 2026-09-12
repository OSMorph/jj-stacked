# Usage guide

`jjk` and `jj-stacked` run the same commands. A local jj bookmark becomes a GitHub PR; its nearest downstack bookmark becomes the base. The bottom PR targets the repository's default branch.

```text
main
  └── user-model       PR targets main
        └── user-api   PR targets user-model
```

## Create and submit a stack

```bash
jj new main -m "Add user model"
# Edit files, then name the segment.
jj bookmark create user-model
jj new -m "Add user API"
# Edit files.
jj bookmark create user-api

jjk submit user-api --dry-run
jjk submit user-api
```

`submit [bookmark]` fetches the selected remote, pushes the selected bookmark and all its dependencies, creates missing PRs, and updates existing bases and navigation comments. It does not close PRs. Unchanged bases and comments are left alone.

Omit the bookmark to infer it from your working change:

```bash
jjk submit
jjk open
```

Inference prefers bookmarks at `@`, then the closest bookmarked descendants when editing inside a segment, then the closest bookmarked ancestors. Ambiguous choices use a terminal picker; scripts must name a bookmark explicitly. `--draft` creates new PRs as drafts; it does not change the draft state of existing PRs. Mark existing drafts ready on GitHub.

Every non-empty change being pushed needs a description. The bookmarked change's first description line supplies its PR title.

## Inspect your work

| Command | Result |
| --- | --- |
| `jjk` or `jjk analyze` | Interactive stack graph; arrows or j/k navigate, Enter prints a submit command, q quits. |
| `jjk analyze --json --no-fetch` | Graph analysis using existing tracking state. |
| `jjk status` | Compact bookmark/base/push/conflict/divergence table, without authentication or network requests. |
| `jjk status --github --fetch` | Fetch the selected remote and include PR state, base, and cleanup evidence. |
| `jjk status --github --json` | Structured report including PR URLs. |
| `jjk doctor` | Effective jj executable/version, repository/remote/trunk, conflicts, divergence, and pending sync. |
| `jjk doctor --github` | Also validate GitHub authentication and repository access. |
| `jjk open user-api` | Open the bookmark's latest PR in the default browser. |
| `jjk open user-api --url` | Print its URL without opening a browser. |

`status` reports whether its remote data was fetched; without `--fetch`, it uses local tracking data. `doctor` returns a nonzero status for errors; warnings are shown for conditions such as a pending sync or divergence. Both support `--json`.

## Merge and sync

Merge PRs from the bottom of the stack upward. After the bottom PR merges:

```bash
jjk sync user-api --dry-run
jjk sync user-api
```

A bookmark selects its whole connected stack, including branches above it. Without a bookmark, `sync` selects all stacks. It fetches the selected remote, identifies contiguous merged segments, cleans up verified merged work, rebases surviving segments onto remote trunk, pushes rewritten bookmarks, and refreshes existing PR bases/comments. It does not create PRs during refresh. `--no-resubmit` skips that final refresh.

Cleanup requires the merged PR's head SHA to match the local bookmark. A bookmark extended or reused since the merge is preserved. Abandoning a rewritten segment also requires evidence that it landed on the selected fetched trunk; a PR merged into some other branch is insufficient. Unsupported descendant paths or protected revisions stop cleanup for review. Out-of-order merges block automatic cleanup rather than skipping a gap.

If conflicts pause sync, resolve the reported changes with jj and continue:

```bash
jj status
jjk sync --continue
```

Sync refuses to start while jj reports conflicted revisions anywhere in the repository (`jj log -r 'conflicts()'`); both the revisions and their descriptions are printed with the refusal. `jj status` only shows working-copy conflicts, so an empty status does not guarantee a clean analysis.

To restore the recorded local state:

```bash
jjk sync --abort
```

Completed steps are checkpointed. Abort restores local jj state; it cannot undo pushes or GitHub edits already completed. Older saved cleanup plans without exact commit identities require aborting and starting a new sync.

## Prune merged bookmarks and stale drafts

Start with a preview:

```bash
jjk prune --merged --dry-run
jjk prune --merged
jjk prune --stale --older-than 30d
```

The terminal lists candidates with reasons, commit IDs, references, and affected descendants. Enter candidate numbers (comma separated), `all`, or an empty selection to skip. After selection, review the final list and confirm; the default is no.

| Scope | Candidate and action |
| --- | --- |
| `--merged` (default) | A merged PR matches the exact local commit: forget the local bookmark, preserve changes. |
| `--closed` | A PR closed without merging: offer local bookmark removal, preserve changes. |
| `--missing-remote` | A prior PR exists but its branch is absent on the selected remote: offer local bookmark removal. |
| `--stale` | Old mutable draft heads outside remote and workspace ancestry: offer abandonment of selected changes. |

Bookmark removal uses `jj bookmark forget`; it does not queue a remote branch deletion. No cleanup command closes PRs or deletes remote branches. Remote scopes fetch the selected remote and stop if that fetch fails.

Staleness is based on the committer timestamp. It is an approximate inactivity filter, not proof that work is unwanted. The default is 30 days; `--older-than` also accepts durations such as `48h`. Stale cleanup is local and needs no GitHub authentication.

By default, only eligible heads are offered. To inspect a whole draft range, supply a jj revset:

```bash
jjk prune --stale --revision 'old-feature::' --older-than 30d --dry-run
```

Each revision still must pass the age and protection checks. Abandonment removes the selected changes and reparents surviving descendants onto their parents. Review changed files and affected descendants before confirming.

For scripts, an explicit scope and `--yes` are required:

```bash
jjk prune --merged --yes
jjk prune --stale --older-than 90d --json
```

`--json` always reports without applying cleanup, even alongside `--yes`.

## Abandon divergent versions

When a change has multiple visible versions, choose the one you intend to keep:

```bash
jjk abandon --diverged
```

The picker shows commit IDs, parents, descriptions, local/remote references, and content differences. Select the version to **keep**, or skip that group. The final preview identifies the other versions and descendants affected by abandonment.

To act on one change group explicitly:

```bash
jjk abandon --diverged --keep <commit-id> --dry-run
jjk abandon --diverged --keep <commit-id> --yes
```

Use a commit ID, not the ambiguous change ID. A unique commit prefix is accepted. There is no automatic “keep newest” policy. `--dry-run` or `--json` without `--keep` reports the groups without choosing a version.

Descendants of an abandoned version are reparented onto that version's parents. They are not automatically transplanted onto the retained sibling. Cleanup blocks changes affecting immutable history, remote history, any workspace, or the retained version. Move or resolve those references deliberately in jj before retrying.

Both cleanup commands print a recovery operation before applying changes:

```text
To restore local state: jj op restore <operation-id>
```

Restoration returns the repository to that local operation, including other later local work. Inspect `jj op log` before restoring after additional work. Cleanup stops if the repository changed after preview, and it requires finishing or aborting any pending sync first.

## Dry runs

`submit`, `sync`, `prune`, and `abandon` support `--dry-run`. They omit the planned history rewrites, cleanup, pushes, and GitHub writes. Remote-aware commands can still fetch and query GitHub; jj may snapshot working-copy edits. Use `status` or `analyze --no-fetch` for local inspection.

## Remotes and configuration

Commands that work with a selected remote accept `--remote NAME`. Selection defaults to `origin`, then the sole remote; otherwise specify it explicitly. `analyze` retains its all-remotes fetch behavior; `--no-fetch` skips it.

```bash
jjk submit user-api --remote upstream
jjk sync user-api --remote upstream
jjk status --remote upstream --fetch
```

Authentication can use GitHub CLI credentials or environment tokens. See [installation](installation.md) and [troubleshooting](troubleshooting.md).

| Variable | Purpose |
| --- | --- |
| `JJ_PATH` | jj executable path; default `jj` on PATH. |
| `GITHUB_TOKEN`, `GH_TOKEN` | GitHub authentication token. |
| `GHE_TOKEN` | Token preferred for GitHub Enterprise. |
| `GITHUB_HOST` | Override host detected from the selected remote. |
| `GITHUB_API_URL` | Override the GitHub API base URL, including for GitHub.com. |
| `GITHUB_OWNER`, `GITHUB_REPO` | Override the repository inferred from the remote. |
| `TRUNK_BRANCH` | Override the default branch name. Authenticated commands otherwise query GitHub; local commands use jj configuration/bookmarks. |
| `JJ_STACK_DEBUG` | Enable debug logging when nonempty; explicit `--debug=false` overrides it. |
| `JJ_STACK_LOG_FORMAT` | Logging format (`text` or `json`). |
| `NO_COLOR` | Disable colored output. |

Global flags `--debug` and `--no-color` work before or after subcommands. Debug output can contain repository information; review it before sharing.

## Completion and updates

Generate shell completion with `jjk completion bash`, `zsh`, `fish`, or `powershell`. Bookmark arguments complete dynamically. Generate a separate script using `jj-stacked` if you use the long name.

```bash
jjk update --check
jjk update
```

Updates run only when requested. Package-manager installations print the appropriate update command. Run `jjk COMMAND --help` for the full current flag reference.
