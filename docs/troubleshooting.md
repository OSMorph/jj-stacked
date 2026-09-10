# Troubleshooting

This guide covers common issues and their solutions.

## Authentication Issues

### "authentication failed"

**Symptom:** Error when running `jj-stacked submit` or `jj-stacked auth test`.

**Solutions:**

1. Test your authentication:
   ```bash
   jj-stacked auth test
   ```

2. If using GitHub CLI, re-authenticate:
   ```bash
   gh auth login
   gh auth status  # Verify it worked
   ```

3. If using environment variables, check they're set:
   ```bash
   test -n "$GITHUB_TOKEN" && echo "GITHUB_TOKEN is set"
   # Check presence without printing the credential.
   ```

4. Verify your token has `repo` scope:
   - Go to https://github.com/settings/tokens
   - Check the token's scopes
   - Ensure `repo` is checked

### "token expired"

**Symptom:** Authentication worked before but now fails.

**Solution:**

Regenerate your token:

1. Go to https://github.com/settings/tokens
2. Delete the expired token
3. Create a new one with `repo` scope
4. Update your authentication:
   ```bash
   # GitHub CLI
   gh auth login

   # Or environment variable
   export GITHUB_TOKEN=ghp_new_token_here
   ```

### GitHub Enterprise authentication fails

**Symptom:** Can't authenticate to GHE instance.

**Solutions:**

1. Ensure you're using the correct hostname:
   ```bash
   jj-stacked auth test --host git.mycompany.com
   ```

2. For GitHub CLI:
   ```bash
   gh auth login --hostname git.mycompany.com
   gh auth status --hostname git.mycompany.com
   ```

3. For environment variables, set both:
   ```bash
   export GHE_TOKEN=ghp_your_ghe_token
   export GITHUB_HOST=git.mycompany.com
   ```

4. Ensure the token was created on the GHE instance, not github.com

## Bookmark Issues

### "bookmark not found"

**Symptom:** `jj-stacked submit my-bookmark` says bookmark doesn't exist.

**Solutions:**

1. List your bookmarks:
   ```bash
   jj bookmark list
   ```

2. Check for typos in the bookmark name

3. If the bookmark was deleted, recreate it:
   ```bash
   jj bookmark create my-bookmark
   ```

### No bookmarks shown in graph

**Symptom:** `jj-stacked` shows empty or minimal output.

**Possible causes:**

1. **No bookmarks created** - Create bookmarks for your changes:
   ```bash
   jj bookmark create my-feature
   ```

2. **Bookmarks on trunk** - jj-stacked only shows bookmarks with changes from trunk:
   ```bash
   jj new main -m "My change"
   jj bookmark create my-feature
   ```

3. **Bookmarks excluded due to merges** - Check for merge commits:
   ```bash
   jj log
   ```
   Rebase to linear history if needed.

### "bookmark excluded due to merge commits"

**Symptom:** Some bookmarks don't appear in the graph.

**Explanation:** jj-stacked only supports linear stacks. Bookmarks containing or descended from merge commits are excluded.

**Solution:**

Rebase to create linear history:

```bash
# Identify the merge
jj log

# Rebase your changes linearly
jj rebase -d main@origin
```

## Repository Issues

### "no GitHub remote found"

**Symptom:** jj-stacked can't find a GitHub remote.

**Solutions:**

1. List your remotes:
   ```bash
   jj git remote list
   ```

2. Add a GitHub remote:
   ```bash
   jj git remote add origin https://github.com/user/repo.git
   # or
   jj git remote add origin git@github.com:user/repo.git
   ```

3. If you have multiple remotes, specify which to use:
   ```bash
   jj-stacked submit my-feature --remote origin
   ```

### "not a jj repository"

**Symptom:** jj-stacked fails to find repository.

**Solution:**

Initialize a Jujutsu repository:

```bash
# New repository
git init my-repo
cd my-repo
jj git init --colocate

# Existing Git repo
cd existing-repo
jj git init --colocate
```

### "failed to fetch from remotes"

**Symptom:** Warning about fetch failure when running `jj-stacked`.

For `jj-stacked analyze`, this is usually non-fatal and analysis can continue with local state. For `submit`, `sync`, and remote-aware `prune`, fetch failure is intentionally blocking because planning against stale remote state could rewrite or push the wrong stack.

**Possible causes:**
- Network connectivity issues
- Remote authentication problems
- Remote doesn't exist

**To skip fetching:**
```bash
jj-stacked analyze --no-fetch
```

## PR Creation Issues

### "base branch mismatch"

**Symptom:** PR has wrong base branch.

**Solution:**

Re-submit the stack to update base branches:
```bash
jj-stacked submit top-bookmark
```

jj-stacked will detect the mismatch and update the PR.

### "PR already exists"

**This is expected behavior.** jj-stacked detects existing PRs and:
- Updates the branch (pushes new commits)
- Updates the base branch if needed
- Updates the stack navigation comment

### "push rejected"

**Symptom:** Can't push bookmark to remote.

**Possible causes:**

1. **Empty change description** - Every non-empty change in the selected stack must have a description. Describe the reported change, then submit again:
   ```bash
   jj describe -r my-feature -m "Describe my feature"
   jj-stacked submit my-feature
   ```

2. **Branch protection rules** - Check repository settings on GitHub

3. **Stale remote state after rewriting history** - Fetch before submitting so jj can perform its force-with-lease safety check:
   ```bash
   jj git fetch --remote origin
   ```
   Then re-submit:
   ```bash
   jj-stacked submit my-feature
   ```

4. **No push access** - Verify you have write access to the repository

### Draft PRs not created

**Symptom:** PRs created as ready for review despite `--draft` flag.

**Verify you're using the flag:**
```bash
jj-stacked submit my-feature --draft
```

Note: Existing PRs aren't converted to drafts. The flag only affects new PRs.

## Cleanup and recovery

### "repository changed after the cleanup preview"

Another jj operation or a newly snapshotted edit changed the reviewed state. Run the cleanup command again and review its new candidates. Do not reuse an old commit selection without checking it.

### "abandonment would affect protected commit"

The selected changes feed immutable history, a remote bookmark, a workspace, or a divergent version you chose to keep. Inspect their descendants with `jj log -r '<commit-id>::'`. Move or resolve the relevant references deliberately in jj, or keep that group. `--yes` does not bypass these protections.

### "finish or abort the pending sync before cleanup"

Run `jjk doctor` to inspect the pending phase, then resolve conflicts and run `jjk sync --continue`, or restore the saved local operation with `jjk sync --abort`. Completed remote pushes and GitHub edits cannot be rolled back by local restoration.

### A merged bookmark was preserved

A PR with the same bookmark name may refer to an older commit. Cleanup requires the exact merged PR head SHA to match the current local bookmark. New work on a reused bookmark is preserved. Review the PR and local diff rather than treating the name as proof that the work merged.

### A merged segment could not be verified on trunk

A matching PR head does not prove it merged into the trunk you selected. Sync also checks the actual landing commit, or verified dependency ancestry. An unknown destination or a merge newer than the fetch stops destructive cleanup. Inspect the PR's base and local history, then start a fresh sync using current remote state. `jjk prune --merged` can remove a verified merged bookmark locally while preserving its changes.

### Recovering a cleanup

The command prints `jj op restore <operation-id>` before mutation. Inspect `jj op log` and restore that operation if appropriate. This restores the whole recorded local repository state, so account for any work you did afterward. The command does not undo remote activity.

### "ambiguous bookmark around @"

Pass the bookmark explicitly to `submit` or `open`. Interactive terminals offer a picker; scripts do not guess between candidate stacks.

## UI Issues

### Interactive view doesn't work

**Symptom:** Graph view displays incorrectly or keyboard doesn't respond.

**Possible causes:**

1. **Terminal doesn't support TUI** - Try a different terminal emulator

2. **Piped input** - Interactive mode requires a TTY:
   ```bash
   # This won't work
   echo "" | jj-stacked

   # Use JSON output instead
   jj-stacked analyze --json
   ```

3. **Terminal size too small** - Resize your terminal window

### Colors not displaying

**Symptom:** Output is plain text without colors.

**Possible causes:**

1. **NO_COLOR environment variable** - Unset it:
   ```bash
   unset NO_COLOR
   ```

2. **Terminal doesn't support colors** - Try a different terminal

3. **Explicitly disabled:**
   ```bash
   # Remove --no-color if you want colors
   jj-stacked --no-color  # Disables colors
   ```

## Performance Issues

### Slow startup

**Symptom:** jj-stacked takes a long time to start.

**Possible causes:**

1. **Remote fetch** - Skip it for faster startup:
   ```bash
   jj-stacked analyze --no-fetch
   ```

2. **Large repository** - jj-stacked analyzes the change graph, which takes longer for large repos

### Slow submission

**Symptom:** `jj-stacked submit` takes a long time.

**This is normal** for large stacks. Each bookmark requires:
- A push to GitHub
- API calls to check/create/update PRs
- Stack comment updates

**Tips:**
- Use `--dry-run` to verify before committing to the wait
- Submit smaller stacks more frequently

## Debug Mode

Start with local diagnostics, then request authentication checks if needed:

```bash
jjk doctor
jjk doctor --github
```

For detailed troubleshooting, enable debug output:

```bash
jj-stacked submit my-feature --debug
```

This shows:
- Jujutsu commands being executed
- GitHub API calls
- Detailed error information

## Getting Help

### Check authentication

```bash
jj-stacked auth test
jj-stacked auth help
```

### Verify Jujutsu is working

```bash
jj --version
jj log
jj bookmark list
```

### View repository state

```bash
jj-stacked analyze --json | jq .
```

### Report issues

If you encounter a bug:

1. Run with `--debug` and save the output
2. Include your Jujutsu version: `jj --version`
3. Include your jj-stacked version: `jj-stacked --version`
4. Open an issue at https://github.com/OSMorph/jj-stacked/issues
