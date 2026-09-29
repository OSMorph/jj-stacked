# Reordered published stacks

Status: approved by the user on 2026-09-28; implementation and validation complete on 2026-09-29.

## Evidence

Submission discovers open PRs, pushes every changed bookmark, then updates PR
bases. During a reorder, an old PR base can become a descendant of its new head.
GitHub can mark that PR indirectly merged and, with automatic branch deletion,
remove its head branch before the base updates execute. This explains the shape
of the reported #663 closed-PR and #662 missing-base failures. The user confirmed
that #663's timeline shows it was marked merged automatically after the pushes.
No authenticated GitHub inspection has been performed.

The implementation points are `internal/submit/plan.go`,
`internal/submit/actions.go`, and `internal/submit/execute.go`.

## Proposed behavior

1. Discover existing PRs before scheduling pushes. For a published stack whose
   dependency order changes, protect affected existing PRs by temporarily moving
   their bases to the repository default branch before any push. Determine the
   affected set from actual old and desired bases; do not add writes to unchanged
   submissions. Do not simply apply final bases before pushing, because an old
   descendant can already contain the old head.
2. Treat protective base changes as prerequisites. If one fails, abort before
   pushing. Keep temporary and final changes visible in the plan and dry run.
3. Push the selected stack, apply final bases, then sync navigation comments.
   Base failures must prevent dependent work and misleading successful comment
   updates. Surface actionable recovery guidance for partial execution.
4. Preserve rerun behavior after a partial temporary retarget or push. Do not
   automatically reopen or replace historical PRs as an error fallback. Document
   recovery for already affected PRs, distinguishing closed from merged state.
5. Preserve the refresh-only contract: no pushes, creations, or closures.

## Acceptance evidence

- A stateful GitHub fixture reproduces indirect closure and head deletion for
  the original push-first order. Reordering A -> B into B -> A with the fix
  preserves existing PR numbers, open state, branches, and final bases.
- Cover moving across multiple children, unchanged submissions, new branches,
  temporary-retarget failure, push failure, final-base failure, reruns after
  partial execution, dry-run ordering, and refresh-only behavior.
- Use disposable real-jj repositories and local bare remotes for ancestry and
  push behavior. No live GitHub mutations are required.
- Full race-enabled tests on supported jj 0.27.0 and 0.44.0, formatting, lint,
  security checks, CLI build and final artifact inspection pass. No silently
  skipped integration coverage.
- Independent correctness, security, and conciseness review is clean, followed
  by a separate final-suite and artifact-validation pass.

## Execution and ownership

After approval, create or adopt an implementation goal and use the
coordinate-punch-list skill with xhigh coordination for this data-integrity bug.
One executor owns submit code, directly affected callers, regression tests, and
user documentation. A separate environment executor, if needed, owns only
pinned Nix development and worker-image configuration. Complete environment
setup before worker test execution. Use rootless Podman, disabled worker network,
read-only source mounts, and narrowly scoped writable artifact mounts. An
independent validator reviews without editing; corrections return to the
executor. A third agent performs final suite and artifact validation.

## Scope limits

No authenticated GitHub operation until the user explicitly selects an identity.
No live PR repair, source push, commit, release, installation, or deployment.
No broad sync redesign, unrelated cleanup, new submission framework, host-package
installation, or changes to merge-cleanup safety rules.

## Implementation review record

- The initial review found that a historical-PR guard based on pending pushes
  would miss the reported rerun, because all pushes had already succeeded.
  The executor changed this to compare the historical PR's exact head commit
  with the current bookmark. A different commit may reuse the name; missing
  historical identity stops replacement with recovery guidance.
- Rerun coverage includes a later push failure after an earlier push succeeded,
  with refreshed push state, and interruption after a partial final retarget.
- Code and regression tests passed independent review and worker execution,
  followed by the separate final acceptance pass recorded below.

## Validation status

The production code, regression tests, and recovery documentation have received
an independent static review with no outstanding findings. Formatting and diff
checks pass. The Nix configuration, pinned tool-version checks, and worker shell
checks pass. The runner's artifact and runtime-directory guards have been
corrected and smoke-tested.

The pinned worker image has built, but the host's container trust policy rejects
Docker archives. On 2026-09-29 the user explicitly approved task-scoped loading
of the locally built Nix-pinned validation image. The host policy must remain
unchanged, and workers must remain rootless, offline, with read-only source and
narrowly scoped writable artifacts. The approved one-shot load succeeded after
archive hash verification. No image had been loaded before this approval.

- Image: `/nix/store/fsbc6z19cqqdvbzzf16rgbmfqy84szch-jj-stacked-validation-worker.tar.gz`
- Archive SHA-256: `a3c43ab5cfe1a750ec7430f37ffeb70bb04337079c5a60df931d293fee9502db`
- Archive size: 320,089,562 bytes.
- Loaded immutable image ID: `3df3056ad28bd874ea3e88fd758c6f635e6c747d979d86c4450d3634b14b1665`.
- Vulnerability snapshot: `golang/vulndb@5775cfa9da25ff717723a7bb7ea716c86f056335`,
  dated 2026-09-28T16:43:40Z by the flake lock.

The actual worker smoke passed on both supported jj versions. It verified
non-root UID/GID, zero effective capabilities, no-new-privileges, network
isolation, read-only source and root filesystem, writable artifacts, offline
module resolution, and a CLI build. The host policy content remained unchanged.

Focused race-enabled tests passed on jj 0.27.0 and 0.44.0 without skips. The
independent validator also checked the exact pre-fix commit
`bbfc2fe87606a4477357d66350d4decef1c2a688` with only the new regression test added;
the protected-plan assertion failed because PR b was closed and marked merged.
This was a behavioral failure, not a compile or environment failure.

The independent review returned CLEAN for correctness, regression coverage,
documentation, security, dependencies, and conciseness. An independent offline
positive control also detected the expected known vulnerability, verifying that
the pinned database was loaded. The preliminary project scan found zero
reachable vulnerabilities and one module-only advisory.

The final pass initially found incomplete action switches, a lint simplification,
and formatting targets scanning generated Go files under `.artifacts`. The
executor corrected these without suppressions or test removal. The independent
validator reread and retested the corrections and returned CLEAN.

The separate final validator returned CLEAN on the corrected tree:

- Full `go test -race -count=1 -v ./...` passed on jj 0.27.0 and 0.44.0,
  using Go 1.26.6. Each run had 112 passing test records across 25 packages,
  with no skipped tests, failures, races, or panics.
- Both real-jj regression scenarios ran on both versions: the original
  push-first failure and the protected submission preserving PRs and branches.
- `make fmt-check`, `git diff --check`, and golangci-lint 2.12.2 passed
  with zero lint issues.
- Offline govulncheck found zero reachable or imported-package vulnerabilities.
  One module-only Windows advisory remains: GO-2026-5024 in
  `golang.org/x/sys@v0.36.0`, fixed in v0.44.0; the project does not call
  the affected code according to the scan.
- CLI build and artifact inspection passed. `jjk` resolves to `jj-stacked`;
  both have matching version and help output after normalizing the command name.
  Binary SHA-256: `c340534d00b37bedaddfd42b3e914ce629ed0aef3393f06fff6220c00e905a7b`.
- Source manifests were unchanged during the final run. Worker isolation and
  the approved archive hash were verified; host container policy was unchanged.

Final evidence is in `.artifacts/final-validation/VERDICT.md`, with individual
logs and a verified `SHA256SUMS` manifest in the same directory. The root agent
inspected the verdict, test audit, gate logs, and artifact hashes before closing
acceptance. Only this plan's completion record changed afterward. Changes remain
local; no commit, push, installation, deployment, or live PR repair was performed.
