# Code quality audit and implementation record

Completed 2026-09-04 on `main`. **Outcome: SATISFIED after four remediation rounds.** All approved improvements and features are implemented; the final independent audit reported no actionable findings. Changes are uncommitted. The original audit evidence and baseline validation below describe the original checkout; the implementation ledger and final checks are recorded at the end.

The project has a sensible core surrounded by unused infrastructure and inconsistent state handling. Keep Go, Cobra, the working Bubble Tea graph, and analyze → plan → execute. The strongest opportunity is removing code and making repository facts consistent, followed by a small cleanup workflow. A rewrite would add risk without addressing the demonstrated problems.

The code does not establish which model authored a particular design. These recommendations assess present behavior and call sites.

**1. Simplifications worth doing**

| ID / priority | Evidence | Recommended change and acceptance |
| --- | --- | --- |
| S1 / P2 | Eight internal modules have no callers outside their own disconnected implementations; inventory below. | Remove unused modules and newly orphaned helpers. Keep the live implementations. Build, lint, and existing tests must still pass without replacement abstractions. |
| S2 / P2 | `internal/submit/plan.go:169` builds a full submission plan, then filters it into a PR refresh plan. This first performs unrelated orphan discovery and plans pushes, creations, and closures. | Share small PR-discovery and base/comment helpers; construct refresh directly. An instrumented fake must show zero orphan-list/branch-existence calls and zero push/create/close actions during refresh. |
| S3 / P2 | `internal/submit/types.go:136` puts results into `map[string]any`; `internal/submit/execute.go:65` and `:109` inspect concrete action types and patch future actions. | First replace string-keyed results with typed fields such as created PR information and one authoritative error. Keep the existing action interface unless a subsequent change demonstrates that a plain action struct plus executor is clearer. Preserve execution order and failure handling. |
| S4 / P2 | `internal/config/config.go` is never loaded. Repository discovery reads environment variables itself; commands construct additional executors. `cmd/jj-stacked/main.go:79` handles color only in the default command, while debug flags are duplicated in subcommands. | Have a small shared command setup resolve the effective options once and pass the existing jj/client/logger dependencies through. Honor or explicitly retire documented `JJ_PATH` and `GITHUB_API_URL`; do not create a new configuration framework. Test default/subcommand color and debug behavior, executable selection, and remote/base selection. |
| S5 / P2 | `internal/submit/actions.go:178` updates an existing comment unconditionally; `internal/submit/plan.go:417` collects merged history from a map without sorting. | Sort history and skip comment writes when rendered content is unchanged. Repeating an unchanged submission should make no GitHub writes. Reuse discovered comment data when useful; a general cache is unnecessary. |
| S6 / P3 | `REQUIREMENTS.md` is 1,200+ lines, duplicates implementation details, claims no local mutations (`:5`), no API calls in dry run (`:118`), and an interactive remote picker (`:472`) that is not wired in. `README.md:376` links to missing `tasks/README.md`. | Replace the specification with a short behavior contract and architecture note; keep user instructions in the usage guide. Remove dead links and promises for disconnected features. Check examples against command help. |

S1's deletion candidates total **1,297 lines**, approximately **13% of the 10,074 production Go lines**, including comments and blanks:

| File | Lines | Why it is a candidate |
| --- | ---: | --- |
| `internal/config/config.go` | 122 | Unimported configuration package. Resolve documented configuration behavior in S4 before treating removal as the whole fix. |
| `internal/github/remote.go` | 130 | Unused parser/helper family; repository discovery uses its own parser. Retain one tested parser if useful cases are consolidated. |
| `internal/github/ratelimit.go` | 172 | Retry machinery is never invoked by the client. |
| `internal/github/stacksync.go` | 127 | Unused comment synchronizer duplicates the live submission action. |
| `internal/ui/bookmark_select.go` | 163 | Unused bookmark-selection model. |
| `internal/ui/remote_select.go` | 167 | Unused remote-selection model. |
| `internal/ui/spinner.go` | 168 | Unused spinner workflow. |
| `internal/ui/submit_progress.go` | 248 | Unused progress model and callbacks. |

These are candidates established by repository-wide references, not a required line-reduction target. Preserve any behavior intentionally incorporated into the live path. Do not remove Bubble Tea/Bubbles simply because some UI modules are dead: the graph still uses them. Additional small unused helpers can be removed after this pass; avoid spending a separate project on tiny wrappers or comments.

**2. Correctness prerequisites for cleanup features**

P1 means fix before expanding automatic cleanup; P2 means a concrete normal-priority defect. All findings below are high confidence. Static findings were traced through their consumers; only C4 and C5 were additionally reproduced through the built CLI in temporary repositories.

| ID / priority | Finding and evidence | Smallest fix and acceptance |
| --- | --- | --- |
| C1 / P1 | Merged-PR detection can miss list results. `internal/github/client.go:245` uses the list endpoint; `:393` converts `Merged` only from `GetMerged()`. `internal/sync/detect.go:50` then requires both the boolean and timestamp. The documented list representation supplies `merged_at` without the separate boolean. | Normalize merged state from `MergedAt`, with an HTTP fixture passing through the real adapter. Cover merged, closed-unmerged, and open responses. Ship with C2, so repairing detection does not expose unsafe cleanup. |
| C2 / P1 | A bookmark name is treated as proof of merged content. `internal/sync/detect.go:38` associates the latest PR for a name with the bookmark's current commit; the PR model discards the head SHA. `internal/sync/execute.go:121` later abandons by bookmark name. Extending/reusing an already merged bookmark can therefore classify new work as merged once C1 is fixed. | Keep the PR head commit identity, require positive correspondence with the local candidate, and carry exact reviewed commit IDs into mutation/recovery. A reused or moved bookmark must retain new work. Test multi-change segments, squash/rebase merges, and bookmark movement between planning and execution. Uncertain equivalence should produce a review candidate. |
| C3 / P1 | `submit` can plan unrelated PR closures. `internal/commands/submit/submit.go:135` treats an identity lookup failure as an empty username; `internal/submit/plan.go:275` then omits author filtering. Including the default branch in candidate bases also broadens the scope beyond the selected stack. | Immediately disable closure on unknown identity and require actual stack membership. Prefer moving orphan closure to explicit cleanup as a deliberate behavior change. Tests must show that submitting A cannot close unrelated B and identity failure produces no close actions. |
| C4 / P2 | Remote status drops lower stack bookmarks. `internal/jjutils/bookmarks.go:223` queries `heads(remote_bookmarks())`, which selects graph heads, not the target of every bookmark. | Query all relevant remote bookmark targets and retain the selected remote's identity. With `main → A → B` fully pushed, both A and B must be synced; another remote or `@git` must not supply the selected remote's status. |
| C5 / P2 | Graph construction silently truncates ancestry. `internal/jjutils/graph.go:104` and `:194` impose a 100-ancestor/entry limit without reporting truncation. | Traverse the required range to its actual boundary. Do not introduce a page/cache framework just to remove a magic cutoff. A segment with 105 changes must keep its true parent and complete count; submission must retain its downstack bookmarks. |
| C6 / P2 | Scoped sync can lose its refresh targets. `internal/commands/sync/sync.go:283` refreshes using the original selected bookmark; `:425` rebuilds the graph after cleanup. If selected A was merged and removed, its component is empty and surviving B is not refreshed. | Persist the selected surviving bookmarks in plan/state and use those for refresh. `sync A` on merged A with child B must update B on initial execution and `--continue`, without touching unrelated stacks. |

C1 is supported by the [official GitHub list-pull-requests representation](https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28#list-pull-requests). No live authenticated GitHub mutations were performed.

Observed C4 result: after pushing both `feature-a` and `feature-b`, `analyze --json --no-fetch` reported `feature-a.is_synced=false` and `needs_push=true`, while B was synced. The raw jj query returned only B for `heads(remote_bookmarks())` although the unfiltered query showed A and B.

Observed C5 result: adding a 105-change segment above B produced a separate `long-tip` stack with 100 changes and no parent, instead of a segment attached to B.

**3. Features that fit this CLI**

The following feature proposals were accepted and implemented.

`sync` already attempts cleanup of contiguous merged bookmarks as part of rebasing and pushing. A dedicated `prune` command would provide focused cleanup without requiring the full sync workflow.

| Order | Feature | Useful first version |
| --- | --- | --- |
| 1 | `jjk prune` | Review cleanup candidates with explicit reasons. Separate merged bookmark cleanup from abandoning work. |
| 2 | `jjk abandon --diverged` | Group divergent variants, show their differences and references, and let the user choose which version to retain. |
| 3 | `jjk status` | A concise noninteractive stack table: bookmark, PR URL/state, base, push state, conflicts/divergence, cleanup reason. Reuse `analyze` data and keep JSON available. |
| 4 | `jjk doctor` | Explain effective jj version, remote/trunk, authentication, hidden/conflicted bookmarks, and pending sync recovery. Report actionable causes using existing adapters. |
| 5 | Infer bookmark from `@` | Allow `submit` without a name when the intended bookmarked stack is unambiguous; otherwise use a short picker. Add `jjk open [bookmark]` for its PR as a small follow-up. |

Pruning should distinguish several different situations:

| Candidate | Evidence | Default treatment |
| --- | --- | --- |
| Merged bookmark | Matching merged PR and verified local target; ancestry may provide additional evidence. | Offer removal of the local bookmark. Treat abandonment of a whole segment as a separate operation requiring complete range/descendant analysis. |
| Closed, unmerged PR | Closed PR plus local target/stack relationship. | Offer review; closure does not establish that its local work is disposable. |
| Missing remote branch | Selected remote no longer has the branch after a successful fetch. | Show a reason for review; never equate this with merged or unwanted work. |
| Old draft or unbookmarked head | Mutable local changes with an old committer timestamp and no active reference requiring them. | Present candidates with descriptions, diffs, and descendants. Age is a filter, not a deletion verdict; timestamps are only a rough inactivity signal. |
| Divergent change | Several visible commits share a change ID. | Route to explicit variant selection. Different content or parentage can be intentional. |

Proposed examples:

```text
jjk prune --merged --dry-run
jjk prune --merged
jjk prune --stale --older-than 30d
jjk abandon --diverged
jjk abandon --diverged --keep <commit-id> --dry-run
```

For `prune`, start with a report and interactive selection. Noninteractive use must supply explicit scope and confirmation; `--yes` must not choose among ambiguous divergent versions. Keep remote PR closure or remote branch deletion as separately selected actions. Fetch only the chosen remote when remote facts are needed and stop claiming those facts are current if fetch fails.

For local-only bookmark removal, use `jj bookmark forget` deliberately: `bookmark delete` schedules deletion for a future push, and neither command abandons the target changes. The distinction should be visible in previews. [Official jj CLI reference](https://docs.jj-vcs.dev/latest/cli-reference/#jj-bookmark-forget).

For the divergent picker, show full commit identities internally and short IDs in the UI, bookmarks/remotes, parent, description, content differences, and affected descendants. Choose a survivor explicitly; avoid a “keep newest” default. jj supports abandoning a particular divergent version by commit ID because its change ID alone is ambiguous. [Official divergence guide](https://www.jj-vcs.dev/latest/guides/divergence/).

`jj abandon` reparents descendants onto the abandoned revision's parents; it does not automatically transplant them onto whichever divergent sibling was retained. The preview must explain that consequence. Protect immutable revisions and working copies in every workspace; handle any desired descendant transplant as an explicit, previewed operation. Record the starting jj operation and provide recovery instructions. Local operation restoration cannot undo remote pushes or PR edits. [Official jj abandon reference](https://docs.jj-vcs.dev/latest/cli-reference/#jj-abandon).

Implement cleanup with a small candidate/plan model containing exact IDs, reason, action, and affected references/descendants. Use existing jj execution and operation-recovery primitives. Discover unbookmarked draft changes directly through jj queries: the current bookmark-only `ChangeGraph` cannot represent them all. A generic rules engine, background janitor, score-based staleness system, or new persistence database is unnecessary for this feature.

**4. Suggested implementation sequence**

1. **Make the important checks real.** Provision an explicit jj version in CI and verify it before running integration tests. `.github/workflows/ci.yml` currently never installs/checks jj, while `internal/integration_test.go:22` and `internal/jjutils/remote_integration_test.go:45` skip without it. Add GitHub HTTP adapter fixtures and command-level sync coverage; avoid a blanket coverage target. Test the minimum advertised version and a current pinned version, or deliberately narrow the support contract. Keep tooling project-local when practical.
2. **Repair state facts and cleanup scope.** Address C1+C2 together, C3, C4, C5, and C6 with the focused scenarios above. Complete this before expanding automatic cleanup. Prefer small cohesive fixes over a graph rewrite.
3. **Delete unused code and align documentation.** Apply S1/S6. Consolidate only useful cases from the unused parser/configuration code, then delete the alternate paths. Keep this separate from behavior changes so review is straightforward.
4. **Simplify live planning and command setup.** Apply S2/S4, make unchanged comments a no-op (S5), and type the action results (S3). Preserve the durable sync state format or provide an explicit compatibility path for pending operations.
5. **Ship prune incrementally.** First add a read-only candidate report and merged local-bookmark cleanup. Next add explicit selection of stale draft heads/ranges. Automatic abandonment of merged segments should wait for multi-change, squash-merge, and active-descendant cases to pass. Share the eligibility logic with sync so the two commands agree.
6. **Ship the divergent picker.** Reuse the candidate preview and recovery plumbing, with explicit keeper selection and commit-ID targeting. Test different content, different parents, descendants, multiple bookmarks/remotes, immutable versions, and working copies in other workspaces.
7. **Add status/doctor and small navigation conveniences.** Reuse the same facts and adapters; avoid maintaining another independent graph model.

Each step is independently reviewable. New mutation tests should prove preservation of unrelated work, exact scope, dry-run behavior, and recovery after failure. Removing dead code does not require tests that merely assert implementation structure.

**5. Designs to preserve**

- Analyze → plan → execute: useful for dry runs, reviewable mutations, and tests.
- Sync's saved operation ID, atomic state writes, checkpoints, and continue/abort support: these address real interrupted-operation behavior.
- Test seams around jj and GitHub: narrow them where convenient, but retain the ability to exercise plans without live services.
- Updater checksum validation and staged replacement/rollback: the length reflects real installation cases, not obvious speculative architecture.
- The working graph UI and GitHub Enterprise support: no evidence from this review justifies removing either.

Deferred: wholesale action-framework replacement, graph caches/concurrency, new storage, multi-forge abstraction, and broad logger/error-wrapper rewrites. The review found stronger, smaller changes first.

**6. Validation and limits**

- Environment: Go 1.26.6, jj 0.44.0, macOS arm64.
- Passed: `go test -short -race -cover ./...`.
- Passed: `go test -race ./internal/jjutils`, including real jj push acceptance/rejection against temporary local bare remotes.
- Passed: `golangci-lint run ./...` (zero issues), `make fmt-check`, and build of `./cmd/jj-stacked` to a temporary binary.
- Reproduced C4/C5 through that binary in disposable jj repositories; no project history or hosted repository state was changed.
- The main `internal` integration suite was not executed outside short mode. Its setup performs Git commits with a hard-coded `Test User`, conflicting with this repository's OSMorph commit-identity instruction. No Git commits or `gh` actions were used in this audit.
- No live GitHub end-to-end test or interactive TUI smoke test was performed. GitHub-state findings are adapter/data-flow review backed by API documentation, not claims about a particular hosted repository.
- Short-mode statement coverage was 31.1% for submit, 24.9% for sync, and 0% for the GitHub adapter and sync command packages. These figures describe the executed suite, not a target or complete integration coverage measurement.
- At the end of the original audit, application code was unchanged. The implementation record below supersedes that baseline.


## Implementation record

The user approved all changes and requested implementation. Remediation round 1 is complete. Independent verification accepted five remaining findings; remediation round 2 is complete; its verification found one remaining recurrence, addressed in round 3 and awaiting final verification. The original independent auditor findings and the main audit were consolidated into the S/C acceptance IDs above.

| IDs / fingerprint | Round 1 changes | Validation / disposition |
| --- | --- | --- |
| S1: dead-code / eight disconnected modules | Removed the eight modules; retained the live graph UI. | Build and focused tests pass; awaiting final audit. |
| S2: submit/refresh / full-plan filtering | Build PR refresh actions directly. | Instrumented adapter tests verify no unrelated discovery or mutation actions. |
| S3: submit/results / untyped result mutation | Typed created-PR result and one error; executor resolves references without mutating future actions. | Execution and planning tests pass. |
| S4: config / duplicated command setup | Shared debug/logging/prompt setup, unauthenticated local discovery, one jj adapter per context, effective JJ_PATH/API URL/remote settings, inherited color. | Flag, repository, adapter, and command tests. |
| S5: submit/comments / redundant writes and map order | Stable merged history ordering and unchanged-comment no-op. | Repeat execution and ordering tests. |
| S6: docs / oversized inconsistent specification | Replaced REQUIREMENTS with a behavior/architecture contract and one useful flow diagram; updated README, usage, and recovery guidance. | Help and link checks pending. |
| C1: GitHub/list / missing merged normalization | Preserve head SHA and normalize merged_at list results. | Real HTTP adapter fixtures for merged, closed, and open PRs. |
| C2: sync/cleanup / bookmark-name identity | Match exact merged heads, carry exact segment IDs, revalidate targets, preserve surviving full segments, guard descendant coverage, avoid queued remote deletions. | Real squash/rebase merge and movement tests; final descendant/remote cases in progress. |
| C3: submit/orphans / unrelated closure | Removed orphan closure from submit planning and action types. | Refresh and submit tests; no automatic close path remains. |
| C4: jj/remote-status / heads-only query | Query every remote bookmark target; select remote deterministically and exclude @git. | Real two-level pushed-stack and other-remote tests. |
| C5: jj/graph / 100-change cutoff | Traverse full required ancestry. | Real 105-change segment regression. |
| C6: sync/refresh / deleted selection | Persist surviving scoped bookmark names across state serialization and continue. | Command refresh scope regression. |
| F1: prune / explicit local cleanup | Merged/closed/missing-remote bookmark forget; age-filtered draft heads or explicit ranges; text/JSON preview, selection, exact operation checks and recovery. | Real local cleanup, remote retention, range, unrelated-work, dry-run, and workspace tests on jj 0.27 and 0.44. |
| F2: divergence / explicit keeper | Version grouping, diff/parent/reference preview, explicit keeper, descendant protection and recovery. | Different-content/different-parent variants and preserved child/keeper tests on both jj versions. |
| F3–F5: CLI inspection/navigation | status, doctor, optional submit inference, and open. | Focused command and inference tests; final review pending. |
| CI / skipped integration | Install and verify pinned jj binaries on Linux and macOS; fixture setup uses jj rather than Git commits. | Local version-matrix suite pending; no hosted CI run claimed. |

No project commits or hosted repository writes have been performed; integration-test pushes use disposable local bare remotes. Round 1 verification: the full race-enabled suite passed with jj 0.44. The jj 0.27 suite failed one existing unsupported fixture query, and lint reported one import-order issue. Both were accepted for remediation; no test was skipped to hide either failure.

### Round 2 accepted findings

| ID / fingerprint | Classification and evidence | Required resolution |
| --- | --- | --- |
| R1 / sync/execute/intermediate-abandon-conflict | P2; existing sequencing defect exposed by C2 coverage. Real jj reproduction: B edits a file introduced by merged A; abandoning A creates a transient conflict that the planned trunk rebase resolves. | Complete the planned rebase through intermediate conflicts, including serialized resume. Preserve final-conflict/no-push guards. |
| R2 / sync/analysis/in-trunk-scope-anchor | P2; **RECURRENT C6 / AUD-04**. An incorporated anchor is absent from the feature graph before analysis, losing surviving B from the initial plan. | Derive scoped survivors from the exact anchor and preserve initial/resume selection without touching unrelated stacks. |
| R3 / sync/cleanup/merge-destination-proof | P1; **FIX_INDUCED C2 follow-on**. Full current segment cleanup can include an unmerged dependency when the PR merged into another branch. | Require verified landing on selected trunk or positive dependency correspondence. Unknown destinations preserve/block; name/timestamp chains alone are insufficient. |
| V1 / validation/integration/divergence-revset-jj027 | P2; existing fixture uses unsupported `change_id(...)`. | Use a compatible query and pass the full jj 0.27 and 0.44 suites. |
| V2 / validation/sync-detect-test/import-grouping | P2; goimports failure in sync detection test. | Correct import grouping and pass global lint. |

All five findings were accepted. No material findings were dismissed. Timeline: R1 C6 refresh preservation implemented → verification found recurrent initial-scope loss (R2); R1 C2 expanded exact segment cleanup → verification found destination-proof gap (R3). These are addressed through shared scope/landing invariants, not alternating local fixes. No oscillation has been observed. A new read-only verification follows remediation round 2.


Round 2 implementation: phase-aware conflict checks allow only conflicts inside pending rebase subtrees to proceed; actual final conflicts still stop pushes. Scope recovery uses the exact incorporated anchor and preserves its original surviving component. Destructive cleanup now retains PR base/merge SHA evidence, verifies the landing commit in fetched trunk, supports positive local ancestry into a separately verified landed head, and rechecks persisted proof. Legacy destructive plans without this proof stop with abort-and-replan guidance.

Thirteen real-jj round 2 scenarios passed on the current pinned version, covering same-file dependencies for squash/rebase merges, serialized interruption after abandonment, final conflicts with no push, normal/fast-forward merged anchors with unrelated work, four insufficient-destination cases, and a positive dependency proof. Focused race tests and all new scenarios pass on jj 0.27 and 0.44; global lint reports zero issues. Final complete matrix validation and the read-only verification audit are in progress. Actual terminal smokes passed for prune selection/default-no/confirmation and the divergent keeper flow, with retained content verified afterward. Local Markdown file links and section anchors have been checked.


### Round 3 accepted recurrence

Round 2 final validation passed: full `go test -race ./...` suites on jj 0.27 and 0.44, global lint (zero issues), formatting, local build, and diff checks. The independent auditor confirmed R1, R3, V1, and V2 resolved, but found another case of **R2 / sync/analysis/in-trunk-scope-anchor**.

With A → B → C and both A and B incorporated into trunk, `sync A` still missed C because its oldest parent's commit is B rather than A. Merged detection also omitted B. Round 3 must recover lineage through consecutive incorporated stack bookmarks, not only the immediate selected anchor, while retaining unrelated-stack exclusion. Acceptance includes two or more incorporated anchors, surviving work, serialized continuation, and untouched unrelated branches on both supported jj versions.

Timeline: R1 C6 post-cleanup refresh repaired → R2 initial incorporated-anchor scope repaired → R3 consecutive incorporated-anchor scope remains **RECURRENT**. This is incomplete traversal of the same lineage invariant, not oscillation. The finding was accepted; no material finding was dismissed.


Round 3 implementation is complete. A shared exact incorporated-lineage set now drives both surviving-root recovery and merged detection; arbitrary draft descendants are excluded. Only sync scope analysis and its integration tests changed. All 12 scope cases passed with race detection on jj 0.27 and 0.44, covering the original single-anchor cases and two/three incorporated anchors under fast-forward/normal merges, initial/serialized continuation, two survivors, retained remote references, and old/new unrelated stacks. Global lint reports zero issues. The subsequent complete-suite/build checks and independent verification passed, as recorded below.


### Round 4 terminal cancellation correction

The round 3 independent audit returned **NO_ACTIONABLE_FINDINGS**, and both updated full race-enabled version suites, lint, formatting, build, and diff checks passed. A final main-agent PTY check then reproduced **M1 / cli/main/interrupt-swallowed-at-text-prompt (P2, FIX_INDUCED)**: the signal handler introduced in round 1 cancelled a context but consumed Ctrl+C while a synchronous text prompt waited for input. The command stayed alive until another answer arrived.

Round 4 removes that unnecessary custom signal handler and restores the operating system's normal interrupt behavior. No asynchronous prompt framework is added. Acceptance is a built-CLI PTY check showing Ctrl+C exits at selection and confirmation without cleanup. Domain code is unchanged from the passing round 3 full matrix.


## Final outcome

**SATISFIED — four remediation rounds.** The latest independent verification returned **NO_ACTIONABLE_FINDINGS**. All accepted S1–S6, C1–C6, F1–F5, R1–R3, V1/V2, and M1 items are resolved. The recurring incorporated-scope finding was closed in round 3; the signal-handler regression was closed in round 4. No outstanding recurrence, oscillation, material dismissal, or validation failure remains.

- Full `go test -race ./...` passed with pinned jj 0.27.0 and 0.44.0, using `JJ_CONFIG=/dev/null`. The final domain change was covered by both complete runs.
- After the final entrypoint-only signal-handler removal, global lint again reported zero issues and `make build-all` passed. Formatting and `git diff --check` passed.
- Built executables are available as `bin/jj-stacked` and `bin/jjk`; no global installation was performed.
- Actual PTY smokes verified prune selection, default-no cancellation, confirmed cleanup, divergent keeper selection, descendant/keeper content preservation, and immediate Ctrl+C exit at both selection and confirmation. The interruption checks left the jj operation ID and file contents unchanged.
- Command help, local documentation links/anchors, status/doctor JSON, and the job-local jj installer were checked.
- Validation ran locally on macOS arm64. GitHub behavior used HTTP fixtures and injected clients; repository mutation tests used disposable jj repositories and local bare Git remotes. No live GitHub end-to-end session or hosted CI run is claimed.
- No project commits or hosted repository writes were performed. The new CI matrix is configured for Linux and macOS with both pinned jj versions.

See [the usage guide](usage.md) for the new commands and their recovery behavior. Unknown merge destinations and unproven dependency equivalence deliberately preserve/block destructive sync cleanup; no automatic squash-content guesswork was added.
