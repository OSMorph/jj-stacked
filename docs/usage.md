# Usage Guide

This guide covers how to use jj-stacked for managing stacked pull requests.

## Core Concepts

### What are Stacked PRs?

Stacked PRs are a series of pull requests where each PR builds on the previous one. Instead of one large PR, you create a chain of smaller, focused PRs that are easier to review.

```
main
  └── feature-part-1 (PR #1)
        └── feature-part-2 (PR #2)
              └── feature-part-3 (PR #3)
```

### How jj-stacked Works

jj-stacked uses Jujutsu bookmarks to identify stacks. Each bookmark becomes a separate PR, with the base branch automatically set to maintain the stack structure:

- The bottom bookmark's PR targets `main` (or your default branch)
- Each subsequent bookmark's PR targets the bookmark below it

## Basic Workflow

### 1. Create Your First Change

Start from your main branch and create a change:

```bash
jj new main -m "Add user data model"
# Make your changes...
jj bookmark create user-model
```

### 2. Stack Another Change

Create a new change on top:

```bash
jj new -m "Add user API endpoints"
# Make more changes...
jj bookmark create user-api
```

### 3. Continue Stacking

Add more changes as needed:

```bash
jj new -m "Add user tests"
# Make more changes...
jj bookmark create user-tests
```

### 4. View Your Stack

See all your bookmark stacks:

```bash
jj-stacked
```

This opens an interactive view showing:
- All bookmark stacks in your repository
- Number of changes in each segment
- Sync status with remote

Use arrow keys to navigate, Enter to select, `q` to quit.

### 5. Submit Your Stack

Create PRs for your entire stack:

```bash
jj-stacked submit user-tests
```

This will:
1. Push all bookmarks to GitHub
2. Create PRs for `user-model`, `user-api`, and `user-tests`
3. Set correct base branches (user-api PR targets user-model branch)
4. Add stack navigation comments to each PR

### 6. After Review

When the bottom PR merges:

```bash
# Fetch the merged changes
jj git fetch

# Rebase your stack onto main
jj rebase -d main

# Re-submit to update remaining PRs
jj-stacked submit user-tests
```

## Commands Reference

### `jj-stacked` (Default Command)

Launches the interactive graph view.

```bash
jj-stacked
```

**Keyboard Controls:**
- `↑/↓` or `j/k` - Navigate between bookmarks
- `Enter` - Select bookmark (prints submission command)
- `q` or `Ctrl+C` - Quit

### `jj-stacked analyze`

Analyze and display bookmark stacks with more options.

```bash
# Interactive view
jj-stacked analyze

# JSON output for scripting
jj-stacked analyze --json

# Skip remote fetch (faster)
jj-stacked analyze --no-fetch
```

**Flags:**
- `--json` - Output as JSON
- `--no-fetch` - Skip fetching from remotes
- `--debug` - Enable debug output

### `jj-stacked submit <bookmark>`

Submit a bookmark and all its downstack bookmarks as PRs.

```bash
# Submit a stack
jj-stacked submit my-feature

# Preview without making changes
jj-stacked submit my-feature --dry-run

# Create as draft PRs
jj-stacked submit my-feature --draft

# Specify a remote
jj-stacked submit my-feature --remote upstream
```

**Flags:**
- `--dry-run` - Show plan without executing
- `--draft` - Create PRs as drafts
- `--remote <name>` - Specify remote to push to
- `--debug` - Enable debug output

### `jj-stacked auth test`

Test GitHub authentication.

```bash
# Test default host (auto-detected or github.com)
jj-stacked auth test

# Test specific host
jj-stacked auth test --host git.mycompany.com
```

### `jj-stacked auth help`

Display authentication setup instructions.

```bash
jj-stacked auth help
```

## Advanced Usage

### Working with Multiple Stacks

You can have multiple independent stacks:

```bash
# Stack 1: User feature
jj new main -m "Add user model"
jj bookmark create user-model
jj new -m "Add user API"
jj bookmark create user-api

# Stack 2: Auth feature (independent)
jj new main -m "Add auth middleware"
jj bookmark create auth-middleware
jj new -m "Add login endpoint"
jj bookmark create auth-login
```

Submit each stack separately:

```bash
jj-stacked submit user-api
jj-stacked submit auth-login
```

### Dry Run Mode

Always preview before submitting:

```bash
jj-stacked submit my-feature --dry-run
```

Output shows:
- Bookmarks that will be pushed
- PRs that will be created
- Base branch assignments
- Any warnings

### Draft PRs

Create draft PRs when your work isn't ready for review:

```bash
jj-stacked submit my-feature --draft
```

Later, mark them ready on GitHub, or re-submit without `--draft`.

### Using with Multiple Remotes

If you have multiple GitHub remotes (e.g., fork workflow):

```bash
# Push to upstream instead of origin
jj-stacked submit my-feature --remote upstream
```

### JSON Output for Scripts

Get machine-readable output:

```bash
jj-stacked analyze --json | jq '.stacks[0].bookmarks'
```

JSON structure:
```json
{
  "stacks": [
    {
      "bookmarks": ["user-model", "user-api"],
      "segments": [
        {
          "bookmark": "user-model",
          "change_count": 3,
          "is_synced": false,
          "needs_push": true,
          "parent": ""
        }
      ]
    }
  ],
  "excluded_count": 0,
  "warnings": []
}
```

## Stack Navigation Comments

Each PR receives a navigation comment showing the full stack:

```markdown
## Stack

| PR | Status |
|---|---|
| #45 `user-tests` | |
| #44 `user-api` | <- this PR |
| #43 `user-model` | |

---
*Managed by [jj-stacked](https://github.com/OSMorph/jj-stacked)*
```

These comments:
- Link to all PRs in the stack
- Show which PR you're currently viewing
- Update automatically when you re-submit

## GitHub Enterprise

jj-stacked fully supports GitHub Enterprise instances.

### Setup

```bash
# Using GitHub CLI
gh auth login --hostname git.mycompany.com

# Or environment variables
export GHE_TOKEN=ghp_your_token
export GITHUB_HOST=git.mycompany.com
```

### Test Connection

```bash
jj-stacked auth test --host git.mycompany.com
```

### Submit

jj-stacked auto-detects the host from your remote URL. No special flags needed.

## Merging and Syncing Stacks

### Merge Order: Bottom to Top

Always merge PRs starting from the **bottom** of the stack (closest to main) and work your way up:

```
main
  └── feature-part-1 (PR #1) ← merge this first
        └── feature-part-2 (PR #2) ← then this
              └── feature-part-3 (PR #3) ← finally this
```

**Why bottom to top?**
- Each PR's base branch is the one below it
- Merging bottom-first means GitHub can cleanly merge each subsequent PR
- Merging out of order causes conflicts and broken base branches

### After Merging a PR

When you merge the bottom PR into main, here's how to sync your remaining stack:

#### Step 1: Fetch the merged changes

```bash
jj git fetch
```

This pulls the merge commit from GitHub into your local repo.

#### Step 2: Abandon the merged change

The merged bookmark's change is now in main, so abandon it locally:

```bash
# Find the change ID of the merged bookmark
jj log

# Abandon it (this removes it from your working set)
jj abandon <change-id>
```

Or if you know the bookmark name:

```bash
jj abandon <merged-bookmark-name>
```

#### Step 3: Rebase remaining changes onto main

```bash
jj rebase -d main
```

This moves your remaining stack to be based on the updated main branch.

#### Step 4: Re-submit the stack

```bash
jj-stacked submit <top-bookmark>
```

This will:
- Update PR base branches (next PR now targets main instead of merged branch)
- Update stack navigation comments
- Push any new changes

### Complete Example

Say you have this stack and PR #1 was just merged:

```
main
  └── user-model (PR #1 - MERGED)
        └── user-api (PR #2)
              └── user-tests (PR #3)
```

Sync your local repo:

```bash
# 1. Fetch merged changes
jj git fetch

# 2. Abandon the merged change
jj abandon user-model

# 3. Rebase onto updated main
jj rebase -d main

# 4. Re-submit remaining stack
jj-stacked submit user-tests
```

Your stack is now:

```
main (includes user-model changes)
  └── user-api (PR #2 - now targets main)
        └── user-tests (PR #3)
```

### Handling Multiple Merges

If several PRs merged while you were away:

```bash
jj git fetch

# Abandon all merged bookmarks
jj abandon user-model
jj abandon user-api

# Rebase what remains
jj rebase -d main

# Re-submit
jj-stacked submit user-tests
```

### What NOT to Do

**Don't merge from the top down** - This leaves orphaned PRs with invalid base branches.

**Don't delete remote branches manually** - Let jj-stacked manage them. If you delete a branch that other PRs depend on, those PRs break.

**Don't forget to rebase** - If you skip `jj rebase -d main`, your local changes still have the old parent and subsequent submits will be confused.

## Best Practices

### Keep Stacks Small

Aim for 3-5 PRs per stack. Larger stacks:
- Are harder to review
- Have more merge conflicts
- Take longer to land

### Use Descriptive Bookmark Names

Bookmarks become branch names and help identify PRs:

```bash
# Good
jj bookmark create add-user-validation
jj bookmark create fix-login-redirect

# Less descriptive
jj bookmark create part1
jj bookmark create wip
```

### Commit Messages Matter

The first line of your commit description becomes the PR title:

```bash
jj new -m "Add email validation to user registration

This adds server-side email validation with:
- Format checking
- Domain verification
- Duplicate detection"
```

### Preview Before Submitting

Always use `--dry-run` first:

```bash
jj-stacked submit my-feature --dry-run
```

### Rebase After Merges

When a PR in your stack merges:

```bash
jj git fetch
jj rebase -d main
jj-stacked submit top-of-stack
```

This updates the remaining PRs to target the correct base branches.

## Next Steps

- [Troubleshooting](troubleshooting.md) - Common issues and solutions
- [Installation](installation.md) - Setup and configuration
