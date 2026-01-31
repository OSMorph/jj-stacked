# jj-stacked

Stacked pull requests for Jujutsu users.

`jj-stacked` (also available as `jjk`) is a CLI tool for creating and managing stacked pull requests on GitHub for developers using [Jujutsu (jj)](https://github.com/martinvonz/jj) version control.

## Quick Start

```bash
# Download latest release (macOS Apple Silicon example)
curl -sL https://github.com/OSMorph/jj-stacked/releases/latest/download/jj-stacked_darwin_arm64.tar.gz | tar xz
sudo mv jj-stacked jjk /usr/local/bin/

# Authenticate (if you have GitHub CLI)
gh auth login

# View your bookmark stacks
jjk

# Submit a stack of PRs
jjk submit my-feature
```

See [Releases](https://github.com/OSMorph/jj-stacked/releases) for all platforms (macOS, Linux, Windows).

> **Note:** `jjk` is a short alias for `jj-stacked`. All examples in this README work with either command.

## Features

- **Interactive stack visualization** - See all your bookmark stacks and their sync status
- **Automatic base branch management** - PRs are created with correct base branches to maintain stack structure
- **Stack navigation comments** - Each PR includes a comment showing the full stack with links
- **Dry-run mode** - Preview what will happen before making changes
- **GitHub Enterprise support** - Works with both GitHub.com and GHE instances
- **Draft PR support** - Create PRs as drafts with `--draft`

## Requirements

- Go 1.21+
- Jujutsu 0.20.0+ (with `jj` in your PATH)
- GitHub account with `repo` scope token
- A Jujutsu repository colocated with Git (`jj git init --colocate`)

## Installation

### Quick Install (Recommended)

```bash
curl -sSL https://raw.githubusercontent.com/OSMorph/jj-stacked/main/install.sh | bash
```

Options:
- Install specific version: `curl -sSL https://raw.githubusercontent.com/OSMorph/jj-stacked/main/install.sh | bash -s -- --version v0.1.0`
- Custom location: `curl -sSL https://raw.githubusercontent.com/OSMorph/jj-stacked/main/install.sh | bash -s -- --prefix /usr/local`

### Homebrew (macOS/Linux)

```bash
brew tap OSMorph/tap
brew install jj-stacked
```

### From Releases

Download from [Releases](https://github.com/OSMorph/jj-stacked/releases):

| Platform | Download |
|----------|----------|
| macOS Apple Silicon | `jj-stacked_<version>_darwin_arm64.tar.gz` |
| macOS Intel | `jj-stacked_<version>_darwin_amd64.tar.gz` |
| Linux x86_64 | `jj-stacked_<version>_linux_amd64.tar.gz` |
| Linux ARM64 | `jj-stacked_<version>_linux_arm64.tar.gz` |
| Windows x64 | `jj-stacked_<version>_windows_amd64.zip` |

### Using Go

```bash
go install github.com/OSMorph/jj-stacked/cmd/jj-stacked@latest
```

Note: This only installs `jj-stacked`. For the `jjk` alias, use another method.

## Authentication

jj-stacked needs a GitHub token with `repo` scope. The easiest method is using GitHub CLI:

```bash
# For GitHub.com
gh auth login

# For GitHub Enterprise
gh auth login --hostname git.mycompany.com
```

Alternatively, set environment variables:

```bash
# GitHub.com
export GITHUB_TOKEN=ghp_xxxxxxxxxxxxx

# GitHub Enterprise
export GHE_TOKEN=ghp_xxxxxxxxxxxxx
export GITHUB_HOST=git.mycompany.com
```

Test your authentication:

```bash
jj-stacked auth test
```

## Usage

### View Your Stacks

Run `jj-stacked` without arguments to launch the interactive graph viewer:

```bash
jj-stacked
```

Navigate with arrow keys, press Enter to select a bookmark, or `q` to quit.

### Submit a Stack

Create PRs for a bookmark and all its dependencies (downstack bookmarks):

```bash
jj-stacked submit my-feature
```

This will:
1. Push all bookmarks in the stack to GitHub
2. Create PRs for bookmarks without existing PRs
3. Update base branches if stack structure changed
4. Add stack navigation comments to all PRs

### Preview Changes (Dry Run)

See what would happen without making changes:

```bash
jj-stacked submit my-feature --dry-run
```

### Create Draft PRs

```bash
jj-stacked submit my-feature --draft
```

### JSON Output

Get machine-readable output for scripting:

```bash
jj-stacked analyze --json
```

## Workflow

A typical stacked PR workflow with jj-stacked:

### Creating and Submitting a Stack

```bash
# 1. Create your first change
jj new -m "Add user model"
# ... make changes ...
jj bookmark create user-model

# 2. Stack another change on top
jj new -m "Add user API endpoints"
# ... make changes ...
jj bookmark create user-api

# 3. View your stack
jj-stacked

# 4. Submit all PRs
jj-stacked submit user-api
```

### After Merging PRs

**Important:** Always merge PRs from the bottom of the stack upward (closest to main first).

After the bottom PR merges on GitHub:

```bash
# 1. Fetch the merged changes
jj git fetch

# 2. Abandon the merged bookmark's change (it's now in main)
jj abandon user-model

# 3. Rebase remaining stack onto updated main@origin
jj rebase -d main@origin

# 4. Re-submit to update remaining PRs
jj-stacked submit user-api
```

> **Note:** Use `main@origin` (not just `main`) to rebase onto the remote trunk. After fetch, your local `main` bookmark may not automatically track the remote, which can cause "immutable commits" errors.

This updates the remaining PRs so they now target main (or the next bookmark in the stack) instead of the merged branch.

See [Usage Guide](docs/usage.md#merging-and-syncing-stacks) for more details on handling merges.

## Commands

All commands work with both `jj-stacked` and `jjk`.

### `jjk` / `jj-stacked` (default)

Launch interactive graph view showing all bookmark stacks.

### `jjk analyze`

Analyze and display bookmark stacks.

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON instead of interactive UI |
| `--no-fetch` | Skip fetching from remotes |
| `--debug` | Enable debug output |

### `jjk submit <bookmark>`

Submit a bookmark stack as pull requests.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be done without making changes |
| `--draft` | Create PRs as drafts |
| `--remote <name>` | Specify remote to push to |
| `--debug` | Enable debug output |

### `jjk sync [bookmark]`

Sync local stack with remote. This command:
1. Fetches the latest changes from all remotes
2. Rebases the stack onto the updated trunk (e.g., `main@origin`)
3. Pushes bookmarks that are ahead of origin

If a bookmark is specified, only that bookmark's stack will be synced. Otherwise, all stacks are synced.

This is useful after merging PRs on GitHub - it will automatically rebase your remaining stack onto the updated main branch.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be done without making changes |
| `--continue` | Continue sync after resolving conflicts |
| `--abort` | Abort sync in progress |
| `--yes`, `-y` | Skip confirmation prompt |
| `--debug` | Enable debug output |

**Examples:**
```bash
# Sync all stacks
jjk sync

# Sync only a specific bookmark's stack (recommended)
jjk sync my-feature

# Preview what would be synced for a specific stack
jjk sync my-feature --dry-run
```

### `jjk auth test`

Test GitHub authentication.

| Flag | Description |
|------|-------------|
| `--host <hostname>` | GitHub host to test (default: auto-detect) |

### `jjk auth help`

Show detailed authentication setup instructions.

## Global Flags

| Flag | Description |
|------|-------------|
| `--debug` | Enable debug logging |
| `--no-color` | Disable colored output |
| `--version` | Show version |

## Stack Comments

When you submit a stack, jj-stacked adds a navigation comment to each PR:

```
This PR is part of a stack of 2 bookmark(s):

1. `main` (base)
2. [user-model](https://github.com/owner/repo/pull/102)
3. **[user-api](https://github.com/owner/repo/pull/103) ← this PR**

### Merged

- ~~[user-setup](https://github.com/owner/repo/pull/101)~~ → merged into `main`

---
*Created with jj-stacked*
```

These comments are automatically updated when you re-submit the stack.
When PRs from the stack are merged and their bookmarks removed locally,
they appear in the "Merged" section to preserve the stack's history.

## Troubleshooting

### "bookmark not found"

Ensure the bookmark exists:
```bash
jj bookmark list
```

### "authentication failed"

Check your authentication:
```bash
jjk auth test
jjk auth help
```

### "merge commit detected"

jj-stacked only supports linear stacks. Bookmarks containing or descending from merge commits are excluded. Rebase your changes to create a linear history.

### "no GitHub remote found"

Ensure your repository has a GitHub remote:
```bash
jj git remote list
```

## Documentation

- [Installation Guide](docs/installation.md)
- [Usage Guide](docs/usage.md)
- [Troubleshooting](docs/troubleshooting.md)

## Contributing

Contributions welcome! Please read the requirements in `REQUIREMENTS.md` and implementation plan in `tasks/README.md`.

## License

MIT
