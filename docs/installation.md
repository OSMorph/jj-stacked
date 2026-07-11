# Installation

This guide covers all methods to install jj-stacked.

## Prerequisites

Before installing jj-stacked, ensure you have:

- **Jujutsu 0.27.0+** - The `jj` command must be in your PATH
- **Git** - Required for the colocated workflow
- **GitHub account** - With a personal access token (repo scope)

Go 1.24+ is required only for source builds and `go install`. Prebuilt releases and Homebrew do not require Go.

### Verify Jujutsu Installation

```bash
jj --version
# Should output: jj 0.27.0 or higher
```

If you don't have Jujutsu installed, see the [Jujutsu installation guide](https://martinvonz.github.io/jj/latest/install-and-setup/).

## Installation Methods

### Homebrew (macOS/Linux)

The easiest way to install on macOS or Linux:

```bash
brew tap OSMorph/tap
brew install jj-stacked
```

This installs both `jj-stacked` and the `jjk` alias.

To upgrade:

```bash
brew update
brew upgrade jj-stacked
```

### From Releases (Recommended)

Download the latest release for your platform from [GitHub Releases](https://github.com/OSMorph/jj-stacked/releases/latest). Release asset names include the version, for example `jj-stacked_2.4.3_darwin_arm64.tar.gz`; do not use the old versionless URLs.

```bash
# The verified installer selects the current version and platform automatically
curl -sSL https://raw.githubusercontent.com/OSMorph/jj-stacked/main/install.sh | bash
```

### Using go install

Install the latest version directly from the repository:

```bash
go install github.com/OSMorph/jj-stacked/cmd/jj-stacked@latest
```

Ensure `$GOPATH/bin` (usually `~/go/bin`) is in your PATH:

```bash
export PATH="$HOME/go/bin:$PATH"
```

Verify installation:

```bash
jj-stacked --version
```

Note: This only installs `jj-stacked`. To use the `jjk` alias, install from a release or create a symlink:

```bash
ln -sf "$(go env GOPATH)/bin/jj-stacked" "$(go env GOPATH)/bin/jjk"
```

### Build from Source

Clone and build locally:

```bash
# Clone the repository
git clone https://github.com/OSMorph/jj-stacked.git
cd jj-stacked

# Build
make build

# Verify the Makefile build
./bin/jj-stacked --version

# Or manually
go build -o jj-stacked ./cmd/jj-stacked

# Verify the manual build
./jj-stacked --version
```

Optionally install to your PATH:

```bash
# Install both jj-stacked and jjk to ~/.local/bin
make install

# Or choose another prefix
make install PREFIX="$HOME"

# Or copy manually
sudo cp jj-stacked /usr/local/bin/
sudo ln -sf /usr/local/bin/jj-stacked /usr/local/bin/jjk
```

## Post-Installation Setup

### 1. Configure GitHub Authentication

jj-stacked needs access to the GitHub API. The easiest method is using GitHub CLI:

```bash
# Install GitHub CLI if you don't have it
# https://cli.github.com/

# Authenticate
gh auth login
```

Alternatively, create a personal access token:

1. Go to https://github.com/settings/tokens/new
2. Select `repo` scope (full control of private repositories)
3. Generate and copy the token
4. Set the environment variable:

```bash
export GITHUB_TOKEN=ghp_your_token_here
```

### 2. Verify Authentication

```bash
jj-stacked auth test
```

You should see:

```
✓ Authentication successful!

  Host:   github.com
  Method: GitHub CLI (gh)
  User:   your-username
  ...
```

### 3. Prepare Your Repository

jj-stacked works with Jujutsu repositories that are colocated with Git:

```bash
# If starting fresh
git init my-project
cd my-project
jj git init --colocate

# If you have an existing Git repo
cd existing-repo
jj git init --colocate
```

Ensure you have a GitHub remote:

```bash
jj git remote list
# Should show: origin (or another name pointing to GitHub)
```

## GitHub Enterprise Setup

For GitHub Enterprise (GHE) instances:

### Using GitHub CLI

```bash
gh auth login --hostname git.mycompany.com
```

### Using Environment Variables

```bash
export GHE_TOKEN=ghp_your_ghe_token
export GITHUB_HOST=git.mycompany.com
```

### Test GHE Authentication

```bash
jj-stacked auth test --host git.mycompany.com
```

## Upgrading

### From a Release or the Installer

```bash
jjk update --check
jjk update
```

`jjk update` verifies the release checksum before atomically replacing `jj-stacked` and `jjk`. Windows users receive the exact matching download link.

### From Homebrew

```bash
brew update
brew upgrade jj-stacked
```

### From Go Install

```bash
go install github.com/OSMorph/jj-stacked/cmd/jj-stacked@latest
```

### From Source

```bash
cd jj-stacked
git pull
make build
```

## Uninstalling

Remove the binaries:

```bash
# If installed with the recommended installer
rm -f ~/.local/bin/jj-stacked ~/.local/bin/jjk

# If installed via go install
rm -f "$(go env GOPATH)/bin/jj-stacked" "$(go env GOPATH)/bin/jjk"

# Or from ~/go/bin explicitly
rm -f ~/go/bin/jj-stacked ~/go/bin/jjk

# Or if installed to /usr/local/bin
sudo rm -f /usr/local/bin/jj-stacked /usr/local/bin/jjk
```

## Troubleshooting Installation

### "command not found: jj-stacked" or "command not found: jjk"

For the recommended installer, ensure `~/.local/bin` is in your PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

For `go install`, ensure Go's bin directory is in your PATH:

```bash
echo $PATH | grep -q go/bin && echo "OK" || echo "Add ~/go/bin to PATH"
```

Add to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.):

```bash
export PATH="$HOME/go/bin:$PATH"
```

### "cannot find module"

Ensure you have Go 1.24 or later:

```bash
go version
```

### "jj: command not found"

Install Jujutsu before using jj-stacked:

```bash
# macOS
brew install jj

# Cargo
cargo install jj-cli

# See https://martinvonz.github.io/jj/latest/install-and-setup/
```

## Next Steps

- [Usage Guide](usage.md) - Learn how to use jj-stacked
- [Troubleshooting](troubleshooting.md) - Common issues and solutions
