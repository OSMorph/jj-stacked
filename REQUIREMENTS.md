# jj-stacked Requirements Specification

## Project Summary

**jj-stacked** is a command-line interface (CLI) tool designed to streamline the creation and management of stacked pull requests on GitHub for developers using Jujutsu (jj) version control system locally. Unlike analogous tools for Git, jj-stacked focuses specifically on bridging the gap between local Jujutsu repository state and GitHub pull requests, without attempting to manipulate the local repository. It leverages Jujutsu's bookmark system to understand the hierarchical structure of stacked changes and automatically creates properly linked pull requests on GitHub with correct base branch relationships.

The tool is built in Go, utilizing Bubble Tea for interactive terminal interfaces and go-github for GitHub API interactions. It follows a three-phase submission architecture (analyze, plan, execute) and implements dependency injection patterns for modularity and testability.

## Core Features

### 1. Interactive Change Graph Visualization

#### 1.1 Default Command (No Arguments)
**Description**: When invoked without any arguments, jj-stacked displays an interactive visualization of the current bookmark stacks and change graph.

**Functionality**:
- Automatically fetches the latest changes from all configured Git remotes using `jj git fetch --all-remotes`
- Builds a complete graph of bookmarked changes by analyzing local bookmarks and their relationships
- Displays an interactive, navigable visualization of stacked bookmarks
- Shows the hierarchical structure of how bookmarks are stacked on top of each other
- Allows users to select a bookmark directly from the graph to submit
- Provides visual indicators for bookmark sync status (synced with remote, needs push, etc.)

**User Experience**:
- Clean, terminal-based UI using Bubble Tea components
- Arrow key navigation through bookmarks
- Real-time display of bookmark relationships and dependencies
- Color-coded status indicators for bookmark states

**Technical Implementation**:
- Uses `AnalyzeCommand()` from `internal/commands/analyze/analyze.go`
- Renders interactive components via Bubble Tea models
- Leverages `BuildChangeGraph()` from `internal/jjutils/jjutils.go` to construct the change graph

---

### 2. Bookmark Stack Submission

#### 2.1 Submit Command
**Description**: Submit a specified bookmark and all bookmarks downstack (toward trunk) as pull requests on GitHub.

**Command Syntax**:
```bash
jj-stacked submit <bookmark-name> [--dry-run] [--remote <name>] [--draft]
```

**Functionality**:

##### 2.1.1 Validation Phase
- Validates that the specified bookmark exists locally
- Checks that the bookmark is not tainted by merge commits (merge commits are not supported in stacks)
- Verifies that all downstack bookmarks are accessible and valid

##### 2.1.2 Analysis Phase
- Infers the bookmark stack structure by traversing from the specified bookmark toward `trunk()`
- Builds a complete graph of bookmark relationships and dependencies
- Identifies all bookmarks in the stack that need to be submitted
- Tracks which bookmarks have remote counterparts and sync status
- Detects and excludes bookmarks that contain or descend from merge commits

##### 2.1.3 Planning Phase
- Determines if each bookmark needs to be pushed to the remote repository
- Checks for existing open pull requests for each bookmark
- Determines the correct base branch for each PR:
  - If the bookmark is stacked on another bookmark, uses that bookmark as the base
  - Otherwise, uses the default branch (`main`, `master`, or `trunk` in descending priority)
- Identifies PRs that need base branch updates due to stack reorganization
- Generates PR titles from the first line of the change description

##### 2.1.4 Execution Phase (Normal Mode)
- Pushes all bookmarks that need pushing to the remote repository using `jj git push`
- Updates base branches for existing PRs that have incorrect bases
- Creates new pull requests for bookmarks that don't have PRs yet
- Adds or updates stack navigation comments on each PR to help reviewers
- Links PRs together to show the complete stack structure

##### 2.1.5 Stack Comment Management
- Creates structured comments on each PR showing its position in the stack
- Includes links to all related PRs in the stack
- Marks the current PR with "← this PR" indicator
- Shows the complete stack hierarchy from `trunk()` to the top
- Updates existing stack comments when the stack structure changes
- Preserves information about already-merged parent PRs in the stack

**PR Title and Body Generation**:
- **Title**: Uses the first line of the change description
- **Body**: Uses the remaining lines of the change description (after the first line)
- Respects the conventional blank line separator between title and body
- Falls back to the bookmark name for title if no description is available
- Extracted from the change description using Jujutsu templates

**Draft PR Support**:
- Use `--draft` flag to create all PRs as drafts
- Useful for work-in-progress stacks or early feedback
- Draft PRs can be converted to ready via GitHub UI

**Base Branch Determination Logic**:
- Bottom-most bookmark in a stack: uses the default branch (main/master/trunk)
- Stacked bookmarks: uses the name of the immediately downstack bookmark
- Handles multiple GitHub remotes with interactive selection

---

#### 2.2 Dry Run Mode
**Description**: Simulates the entire submission process without making any actual changes to the remote repository or GitHub.

**Command Syntax**:
```bash
jj-stacked submit <bookmark-name> --dry-run
```

**Functionality**:
- Executes all analysis and planning phases
- Outputs a detailed plan of what would be done in normal mode
- Shows which bookmarks would be pushed
- Lists PRs that would be created with their titles and base branches
- Identifies PRs that would have their base branches updated
- Does not execute any GitHub API calls or git push operations
- Allows users to preview and verify the submission plan before committing

**Output Details**:
- Clear indication that dry-run mode is active
- Step-by-step breakdown of planned actions
- Visual presentation of the stack structure
- Summary of changes that would be made

---

### 3. GitHub Authentication

#### 3.1 Multiple Authentication Methods
**Description**: Supports multiple authentication methods with automatic fallback and priority ordering. Works with both GitHub.com and GitHub Enterprise (GHE) instances.

**Authentication Priority** (highest to lowest):
1. **GitHub CLI** (Recommended)
2. **Environment Variables** (GITHUB_TOKEN or GH_TOKEN)

##### 3.1.1 GitHub CLI Authentication
**Functionality**:
- Automatically detects if GitHub CLI (`gh`) is installed
- Checks if the user is authenticated via `gh auth status`
- Retrieves authentication token using `gh auth token`
- For GitHub Enterprise: uses `gh auth token --hostname <ghe-host>` to get the correct token
- Provides seamless integration without requiring manual token management
- No local storage of credentials

**GitHub Enterprise Support**:
- GitHub CLI supports multiple hosts via `gh auth login --hostname git.mycompany.com`
- Token retrieval is host-aware: `gh auth token --hostname git.mycompany.com`
- Authentication status checked per-host

**Implementation**:
- Uses Go's `os/exec` package to invoke `gh` commands
- Located in `internal/auth/auth.go` via `GetGitHubCLIAuth()`

##### 3.1.2 Environment Variable Authentication
**Functionality**:
- Reads GitHub Personal Access Token from environment variables
- Supports both `GITHUB_TOKEN` and `GH_TOKEN` variable names
- For GitHub Enterprise: also checks `GHE_TOKEN` environment variable
- Validates tokens by making a test API call to the appropriate GitHub instance
- Provides clear error messages for invalid tokens

**Token Validation**:
- Makes authenticated API call to `GET /user` endpoint on the target GitHub instance
- For GHE: uses the configured API base URL (e.g., `https://git.mycompany.com/api/v3/user`)
- Verifies token has necessary permissions
- Returns detailed error messages if token is invalid or expired

**Required Token Scopes**:
- `repo` (Full control of private repositories, includes pull request access)

---

#### 3.2 Authentication Testing
**Description**: Command to test and validate GitHub authentication setup.

**Command Syntax**:
```bash
jj-stacked auth test [--host <hostname>]
```

**Functionality**:
- Tests the current authentication configuration
- Auto-detects GitHub host from repository remote if `--host` not specified
- Displays authenticated user information:
  - GitHub hostname (github.com or GHE instance)
  - GitHub username
  - Full name (if available)
  - Email address (if public)
  - Token scopes and permissions
- Confirms that the user has repository access for creating PRs
- Validates that required scopes are present
- Shows which authentication method is being used (GitHub CLI or environment variable)
- Provides clear success or failure messages

**Output Details**:
- User information display with formatted output
- GitHub instance hostname displayed prominently
- Scope validation with checkmarks for required permissions
- Clear indication of authentication source
- Actionable error messages if authentication fails

---

#### 3.3 Authentication Help
**Description**: Display comprehensive authentication setup instructions.

**Command Syntax**:
```bash
jj-stacked auth help
```

**Functionality**:
- Shows detailed setup instructions for all authentication methods
- Provides step-by-step guides for:
  - Installing and configuring GitHub CLI
  - Creating GitHub Personal Access Tokens
  - Setting environment variables
- Displays required token scopes
- Includes troubleshooting tips
- Links to relevant documentation

---

### 4. Change Graph Analysis

#### 4.1 Bookmark Discovery
**Description**: Automatically discovers all user-created bookmarks in the repository.

**Functionality**:
- Executes `jj bookmark list` with custom templates to extract bookmark information
- Filters bookmarks using the revset `mine() ~ trunk()` to find user-owned bookmarks not on trunk
- Collects metadata for each bookmark:
  - Bookmark name
  - Commit ID (short format)
  - Change ID (short format)
  - Local bookmarks pointing to the same commit
  - Remote bookmarks and their sync status
  - Whether the bookmark has a matching remote tracking branch
  - Whether the local and remote are synchronized

**Data Extraction**:
- Uses Jujutsu template language to format output as JSON
- Parses JSON output using Go's encoding/json package with struct tags
- Handles multiple bookmarks pointing to the same commit
- Tracks remote bookmark synchronization state

---

#### 4.2 Change Graph Construction
**Description**: Builds a complete directed acyclic graph (DAG) of bookmark relationships and their associated changes.

**Functionality**:

##### 4.2.1 Segment Discovery
- Traverses from each bookmark toward `trunk()` to discover change segments
- A segment is a contiguous sequence of changes between two bookmarks
- Handles empty changes (commits with no diff) by including them in segments with a warning
- Collects detailed information for each change in a segment:
  - Commit ID and Change ID
  - Author name and email
  - Description (first line)
  - Parent commits
  - Associated bookmarks (local and remote)
  - Timestamps (authored and committed)
  - Working copy status

##### 4.2.2 Relationship Mapping
- Builds an adjacency list mapping child bookmarks to parent bookmarks
- Identifies stack roots (bookmarks directly based on trunk)
- Identifies stack leaves (bookmarks with no children)
- Detects cycles and handles complex graph topologies

##### 4.2.3 Stack Construction
- Groups related bookmark segments into complete stacks
- Each stack represents a full path from trunk to a leaf bookmark
- Orders segments from base to top (trunk → intermediate → leaf)
- Supports multiple independent stacks in the repository

##### 4.2.4 Merge Commit Handling
- Detects merge commits (commits with multiple parents)
- Marks merge commits as "tainted" and excludes them from stacks
- Excludes all descendants of merge commits (taint propagation)
- Tracks count of excluded bookmarks for user awareness
- Prevents stack submission for bookmarks containing merges

**Optimization Features**:
- Paginated change retrieval (100 changes per page) for large repositories
- Early termination when encountering fully-collected bookmarks
- Reuses previously analyzed segments to avoid redundant work
- Efficient change traversal using Jujutsu revsets

---

#### 4.3 Repository State Analysis
**Description**: Analyzes the current state of the repository and its relationship with remotes.

**Functionality**:
- Identifies the default branch (main, master, or trunk)
- Lists all Git remotes and their URLs
- Filters remotes to identify GitHub remotes (including GitHub Enterprise)
- Validates remote URLs for GitHub compatibility
- Supports both HTTPS and SSH remote URL formats:
  - HTTPS: `https://github.com/owner/repo.git`
  - SSH: `git@github.com:owner/repo.git`
- Supports GitHub Enterprise URL formats:
  - HTTPS: `https://git.mycompany.com/owner/repo.git`
  - SSH: `git@git.mycompany.com:owner/repo.git`
- Extracts and stores the GitHub hostname for API routing

---

### 5. Pull Request Management

#### 5.1 PR Creation
**Description**: Creates new pull requests on GitHub for bookmarks that don't have existing PRs.

**Functionality**:
- Uses GitHub REST API via go-github to create PRs
- Sets PR title from the change description first line
- Sets PR body from the remaining description lines
- Sets the correct base branch based on stack relationships
- Sets the head branch to the bookmark name
- Creates PRs in the correct order (bottom to top of stack)
- Returns PR metadata including:
  - PR number
  - PR URL
  - PR state
  - Base and head branch information

**API Integration**:
- Uses `client.PullRequests.Create()` from go-github
- Handles rate limiting and API errors
- Validates repository ownership and permissions

---

#### 5.2 PR Base Branch Updates
**Description**: Updates the base branch of existing PRs when stack relationships change.

**Functionality**:
- Detects when an existing PR's base branch doesn't match expected stack relationships
- Automatically updates PR bases to maintain correct stacking
- Updates PRs in order from bottom to top of stack
- Handles GitHub API responses for base branch updates
- Validates that base branch updates are successful

**Use Cases**:
- User reorders bookmarks in the stack
- User rebases bookmarks onto different base bookmarks
- User adds new bookmarks between existing ones
- After merging a PR and rebasing upstack work

**API Integration**:
- Uses `client.PullRequests.Edit()` with base branch parameter from go-github
- Handles edge cases like closed or merged PRs

---

#### 5.3 PR Detection
**Description**: Finds existing open pull requests for bookmarks.

**Functionality**:
- Queries GitHub API for open PRs with matching head branches
- Filters PRs by repository owner and branch name
- Returns first matching open PR for each bookmark
- Caches PR information for efficient lookup during submission
- Handles cases where multiple PRs exist for the same branch (uses first match)

**Query Optimization**:
- Uses GitHub API filtering to reduce response size
- Queries only for open PRs to avoid processing closed/merged PRs

---

#### 5.4 Stack Navigation Comments
**Description**: Creates and maintains informative comments on PRs showing stack structure and relationships.

**Functionality**:

##### 5.4.1 Comment Creation
- Adds a structured comment to each PR in a stack
- Shows the PR's position in the overall stack
- Lists all PRs in the stack from bottom to top
- Links to related PRs for easy navigation
- Marks the current PR with a "← this PR" indicator
- Includes a footer identifying the comment as created by jj-stacked

##### 5.4.2 Comment Updates
- Detects existing jj-stacked comments by footer signature
- Updates existing comments instead of creating duplicates
- Preserves comment history and edit timestamps
- Synchronizes stack information across all PRs when structure changes

##### 5.4.3 Comment Data Format
- Embeds structured metadata in HTML comments within the comment body
- Uses Base64 encoding for JSON data to avoid parsing issues
- Validates comment data using Go struct tags and json.Unmarshal
- Includes version number for future compatibility
- Stores stack information:
  - Bookmark names in order
  - PR URLs for each bookmark
  - PR numbers for API operations

##### 5.4.4 Merged PR Tracking
- Detects when parent PRs in the stack have been merged
- Preserves merged PR information in stack comments
- Allows users to see full stack history including merged work
- Updates stack comments to show complete context

**Comment Format Example**:
```markdown
<!--- JJ-STACK_INFO: <base64-encoded-json> --->
This PR is part of a stack of 2 bookmark(s):

1. `main` (base)
2. [feature-base](https://github.com/owner/repo/pull/42)
3. **[feature-top](https://github.com/owner/repo/pull/43) ← this PR**

### Merged

- ~~[feature-setup](https://github.com/owner/repo/pull/41)~~ → merged into `main`

---
*Created with [jj-stacked](https://github.com/OSMorph/jj-stacked)*
```

The "Merged" section appears when PRs from the stack have been merged and their
bookmarks removed locally. This preserves the stack's history so reviewers can
see the full context of the work.

---

### 6. Git Remote Management

#### 6.1 Remote Discovery
**Description**: Discovers and lists all Git remotes configured in the repository.

**Functionality**:
- Uses `jj git remote list` command to enumerate remotes
- Parses remote name and URL for each configured remote
- Validates remote URL formats
- Returns structured remote information (name and URL)

---

#### 6.2 GitHub Remote Filtering
**Description**: Identifies and filters remotes that point to GitHub.com or GitHub Enterprise instances.

**Functionality**:
- Validates remote URLs against GitHub patterns
- Supports both HTTPS and SSH GitHub URL formats
- Supports GitHub Enterprise instances with custom hostnames
- Filters out non-GitHub remotes from consideration
- Groups remotes by GitHub hostname for multi-instance support

**GitHub.com Pattern Matching**:
- Matches `github.com` in remote URLs
- Handles both colon (SSH) and slash (HTTPS) separators
- Validates repository owner and name extraction

**GitHub Enterprise Detection**:
- Checks for known GHE indicators in URL structure
- Supports explicit GHE hostname configuration via `GITHUB_HOST` environment variable
- Auto-detects GHE when remote URL contains `/api/v3` path structure
- Validates GHE API endpoint accessibility during authentication

---

#### 6.3 Interactive Remote Selection
**Description**: Provides an interactive UI for selecting from multiple GitHub remotes.

**Functionality**:
- Automatically triggered when multiple GitHub remotes are detected
- Displays a list of available GitHub remotes
- Shows remote names and URLs for context
- Allows arrow key navigation and selection
- Returns selected remote name for use in submission
- Cleans up UI components after selection

**User Experience**:
- Clear indication that selection is required
- Visual presentation of available options
- Immediate feedback on selection
- Seamless integration into workflow

**Implementation**:
- Uses Bubble Tea model in `internal/ui/remote_selection.go` for interactive UI
- Tea-based component rendering with Update/View pattern
- Channel-based selection flow

---

#### 6.4 Automatic Remote Resolution
**Description**: Intelligently determines which remote to use based on configuration and available remotes.

**Resolution Logic**:
1. If `--remote <name>` is specified: validates and uses that remote
2. If only one GitHub remote exists: automatically uses it
3. If multiple GitHub remotes exist: launches interactive selector
4. If no GitHub remotes exist: exits with error message

**Validation**:
- Verifies specified remote exists
- Confirms remote is a GitHub remote
- Provides clear error messages for invalid configurations

---

### 7. Bookmark Push Operations

#### 7.1 Push to Remote
**Description**: Pushes local bookmarks to the configured remote repository.

**Functionality**:
- Uses `jj git push` command with bookmark-specific options
- Pushes to the specified remote (not hardcoded to 'origin')
- Tracks new remote bookmarks explicitly on jj 0.36+ using the syntax supported by the installed jj version
- Uses `--allow-new` only on jj 0.27-0.35, where explicit tracking of a new remote bookmark is unavailable
- Allows empty change descriptions during submit because PR titles fall back to bookmark names
- Handles push errors with detailed error messages
- Provides progress feedback during push operations
- Updates remote tracking information after successful push

**Command Construction**:
```bash
jj bookmark track <bookmark-name>@<remote>           # jj 0.36-0.40
jj bookmark track <bookmark-name> --remote <remote>  # jj 0.41+
jj git push --remote <remote> --bookmark <bookmark-name> --allow-empty-description
```

**Force Push Handling**:
- Jujutsu's `jj git push` automatically handles force pushes after rebases
- No explicit `--force` flag needed as jj manages branch history
- Clear messaging to users when force push is performed
- Warns if push would overwrite remote changes not present locally

**Error Handling**:
- Catches and reports authentication errors
- Detects network connectivity issues
- Handles conflicts and push rejections
- Provides actionable error messages

---

#### 7.2 Push Status Determination
**Description**: Determines which bookmarks need to be pushed based on local and remote state.

**Functionality**:
- Checks if bookmark has a remote tracking branch
- Compares local and remote bookmark states
- Identifies bookmarks that:
  - Have no remote counterpart (need initial push)
  - Are out of sync with their remote (need update push)
  - Are already synchronized (can skip push)
- Returns list of bookmarks requiring push operations

**Synchronization Logic**:
- Bookmark is synced if:
  - It has local bookmarks pointing to it
  - It has matching remote bookmarks
  - Remote bookmark names match local bookmark names (excluding `@git`)

---

### 8. Environment Variables and Configuration

#### 8.1 GitHub Configuration Variables
**Description**: Environment variables for GitHub API and repository configuration. Supports both GitHub.com and GitHub Enterprise instances.

**Supported Variables**:

##### 8.1.1 `GITHUB_TOKEN`
- GitHub Personal Access Token for API authentication
- Takes precedence over `GH_TOKEN` (both are checked)
- Should have `repo` scope permissions
- Optional if GitHub CLI is configured
- Used for both GitHub.com and GHE (unless `GHE_TOKEN` is set for GHE)

##### 8.1.2 `GH_TOKEN`
- Alternative name for GitHub Personal Access Token
- Same functionality as `GITHUB_TOKEN`
- Checked if `GITHUB_TOKEN` is not set

##### 8.1.3 `GHE_TOKEN`
- GitHub Enterprise-specific Personal Access Token
- Takes precedence over `GITHUB_TOKEN` when connecting to a GHE instance
- Allows using different tokens for GitHub.com and GHE
- Should have `repo` scope permissions

##### 8.1.4 `GITHUB_HOST`
- GitHub hostname to use (default: `github.com`)
- Set to your GHE hostname for GitHub Enterprise (e.g., `git.mycompany.com`)
- When set, all API calls are routed to this host
- Overrides auto-detection from remote URL

##### 8.1.5 `GITHUB_API_URL`
- Full base URL for GitHub API (advanced configuration)
- For GitHub.com: `https://api.github.com` (default)
- For GHE: `https://git.mycompany.com/api/v3`
- Typically auto-derived from `GITHUB_HOST`, but can be overridden

##### 8.1.6 `GITHUB_OWNER`
- Override for repository owner detection
- Optional - auto-detected from git remote by default
- Useful for testing or non-standard configurations

##### 8.1.7 `GITHUB_REPO`
- Override for repository name detection
- Optional - auto-detected from git remote by default
- Useful for testing or non-standard configurations

---

#### 8.2 Jujutsu Configuration Variables

##### 8.2.1 `JJ_PATH`
**Description**: Custom path to the Jujutsu (`jj`) executable.

**Functionality**:
- Allows specifying a non-standard jj installation location
- Falls back to searching system PATH if not set
- Validated during CLI initialization
- Used for all Jujutsu command invocations

**Default Behavior**:
- Searches for `jj` in system PATH using the `which` utility
- Validates that the binary exists and is executable
- Reports clear errors if jj is not found

---

### 9. CLI User Interface Components

#### 9.1 Interactive Components
**Description**: Rich terminal UI components built with Bubble Tea.

**Component Features**:

##### 9.1.1 Change Graph Visualization Component
- Displays hierarchical bookmark structure
- Shows change metadata (commit IDs, descriptions, authors)
- Indicates sync status with visual markers
- Supports keyboard navigation
- Allows bookmark selection for submission
- Real-time rendering updates via Bubble Tea Update/View cycle

##### 9.1.2 Bookmark Selection Component
- Presents list of bookmarks in a segment
- Handles multiple bookmarks pointing to same change
- Allows user to choose which bookmark to use for PR
- Shows bookmark metadata for context
- Supports cancel/abort operations

##### 9.1.3 Remote Selection Component
- Lists available GitHub remotes
- Shows remote names and URLs
- Keyboard navigation (arrow keys)
- Selection confirmation
- Clean program termination after selection

##### 9.1.4 Progress and Status Indicators
- Shows progress during long-running operations
- Displays current phase (analysis, planning, execution)
- Real-time updates for push and PR creation
- Success/error status reporting
- Detailed operation logs

---

#### 9.2 Output Formatting
**Description**: Consistent, readable output formatting throughout the CLI.

**Formatting Features**:
- Emoji indicators for different message types (✅ success, ❌ error, 🔀 info)
- Color-coded output for different severity levels
- Structured presentation of complex data
- Clear delineation between sections
- Consistent indentation and spacing
- Unicode characters for visual hierarchy

---

### 10. Error Handling and Validation

#### 10.1 Input Validation
**Description**: Comprehensive validation of user inputs and system state.

**Validation Checks**:
- Bookmark name existence verification
- Remote name validation
- Command argument validation
- Environment variable format checking
- GitHub token scope validation
- Repository state consistency checks

**Error Messages**:
- Descriptive error messages with context
- Suggestions for fixing common issues
- Links to documentation when relevant
- Exit codes for scripting integration

---

#### 10.2 API Error Handling
**Description**: Robust handling of GitHub API errors and rate limiting.

**Error Scenarios**:
- Network connectivity issues
- Authentication failures
- Rate limit exceeded
- Permission denied errors
- Resource not found (404) errors
- API version incompatibilities

**Retry Logic**:
- Graceful handling of transient failures
- Clear reporting of permanent failures
- Preservation of partial progress when possible

---

#### 10.3 Jujutsu Integration Error Handling
**Description**: Handling of errors from Jujutsu command execution.

**Error Scenarios**:
- `jj` binary not found
- Invalid revset syntax
- Merge commit detection
- Repository state issues
- Insufficient Jujutsu version
- Working copy conflicts

**Error Recovery**:
- Clear reporting of underlying jj errors
- Suggestions for resolution
- Graceful degradation where possible

---

### 11. Logging and Debugging

#### 11.1 Debug Logging
**Description**: Optional debug logging for troubleshooting and development using Go's standard `log/slog` package.

**Functionality**:
- Controlled via `JJ_STACK_DEBUG=1` environment variable or `--debug` flag
- Logs detailed operation steps
- Shows API calls and responses (without sensitive data)
- Tracks state transitions
- Records timing information
- Outputs to stderr to avoid polluting normal output
- Supports JSON format via `JJ_STACK_LOG_FORMAT=json`

**Implementation**:
- Uses Go 1.21+ `log/slog` package for structured logging
- Located in `internal/logger/logger.go` (thin wrapper around slog)
- Supports debug, info, warn, and error levels
- Never logs sensitive credentials or tokens
- Context-aware logging for request tracing

---

### 12. Workflow Support Features

#### 12.1 Stacked PR Workflow
**Description**: Complete support for the stacked pull request development workflow.

**Workflow Steps Supported**:

##### Step 1: Create Local Changes
- Create changes with `jj new`
- Add bookmarks to changes with `jj bookmark create`
- Stack changes on top of each other
- Group multiple changes into single PR by having one bookmark span multiple changes

##### Step 2: Submit Stack
- Use `jj-stacked submit <top-bookmark>` to create PRs
- Automatically creates all downstack PRs
- Sets correct base branches for stacking relationships
- Adds navigation comments to all PRs

##### Step 3: Review and Merge
- Review PRs on GitHub starting from bottom of stack
- Merge bottom PR when approved
- Use navigation comments to move to next PR

##### Step 4: Update Stack After Merge
- Abandon merged changes locally with `jj abandon`
- Fetch latest trunk with `jj git fetch`
- Rebase remaining bookmarks with `jj rebase -b <top> -d trunk()`
- Re-submit stack with `jj-stacked submit <top>` to update PRs

##### Step 5: Repeat
- Continue merging and updating until entire stack is merged

**Workflow Automation**:
- Automatic base branch management
- PR comment synchronization
- Stack consistency maintenance
- Change tracking across rebases

---

### 13. Repository Requirements

#### 13.1 System Requirements
**Description**: External dependencies and version requirements.

**Required Software**:
- **Go**: Version 1.21 or later
- **Jujutsu (jj)**: Version 0.30.0 or later
- **Git**: Implicitly required (Jujutsu operates on Git repositories)
- **GitHub CLI (optional)**: Latest version recommended for seamless authentication

**Repository Requirements**:
- Must be a Git repository with GitHub remote
- Must have at least one of: `main`, `master`, or `trunk` branch as the default branch
- The default branch must be available on the remote

---

#### 13.2 Jujutsu Integration Points
**Description**: Specific Jujutsu features and commands utilized by jj-stacked.

**Jujutsu Commands Used**:
- `jj bookmark list`: List all bookmarks with metadata
- `jj log`: Retrieve change history and metadata
- `jj git fetch`: Fetch from Git remotes
- `jj git push`: Push bookmarks to Git remotes
- `jj git remote list`: List configured Git remotes

**Revsets Used**:
- `trunk()`: Resolves to the default branch (main/master/trunk)
- `mine()`: Changes authored by the current user
- `mine() ~ trunk()`: User changes excluding trunk ancestors
- `<from>..<to>`: Changes between two revisions
- `<rev>::`: Revision and all its descendants

**Template Language Features**:
- JSON output formatting with `.escape_json()`
- Field extraction (commit_id, change_id, bookmarks, etc.)
- Array operations (`.map()`, `.join()`)
- Conditional expressions
- String manipulation functions

---

## Technical Architecture

### Architecture Patterns

#### Three-Phase Submission Architecture
**Description**: Separates submission into distinct phases for clarity and testability.

**Phases**:

1. **Analysis Phase** (`AnalyzeSubmissionGraph`):
   - Pure function with no side effects
   - Analyzes change graph to identify relevant segments
   - Returns structured analysis result
   - Located in `internal/submit/submit.go`

2. **Planning Phase** (`CreateSubmissionPlan`):
   - Queries GitHub API for existing PRs
   - Determines what actions need to be taken
   - Creates execution plan without making changes
   - Supports callbacks for progress reporting
   - Located in `internal/submit/submit.go`

3. **Execution Phase** (`ExecuteSubmissionPlan`):
   - Executes the plan with no decision-making
   - Pushes bookmarks to remote
   - Creates and updates PRs
   - Updates stack comments
   - Collects and reports results
   - Located in `internal/submit/submit.go`

**Benefits**:
- Clear separation of concerns
- Testability of each phase independently
- Support for dry-run mode (stop after planning)
- Easy to add progress reporting and callbacks

---

#### Dependency Injection Pattern
**Description**: Uses dependency injection for Jujutsu operations to enable testing and modularity.

**Implementation**:
- `JjFunctions` interface defines interface for all jj operations
- `NewJjFunctions()` factory function creates implementation
- All commands receive `jjFunctions` parameter
- Located in `internal/jjutils/jjutils.go`

**Benefits**:
- Easy to mock for testing
- Configurable jj binary path
- Clear API boundaries
- Supports different jj implementations

---

#### Context Propagation Pattern
**Description**: All long-running operations accept `context.Context` as the first parameter.

**Implementation**:
- All public functions accept `context.Context` as first parameter
- Context is propagated to child operations (API calls, command execution)
- `context.WithTimeout` used for network operations
- `context.WithCancel` for user-initiated cancellation

**Benefits**:
- Graceful cancellation on Ctrl+C
- Timeout support for all operations
- Request tracing capability
- Clean shutdown behavior

---

### Technology Stack

#### Core Technologies
- **Go**: Primary language for all implementation (Go 1.21+ required)
- **Cobra**: CLI framework for command structure (github.com/spf13/cobra)
- **Bubble Tea**: Terminal UI framework (github.com/charmbracelet/bubbletea)
- **Lipgloss**: Terminal styling (github.com/charmbracelet/lipgloss)
- **go-github**: GitHub API client library (github.com/google/go-github/v67/github)
- **log/slog**: Standard library structured logging (Go 1.21+)
- **encoding/json**: JSON parsing and validation with struct tags
- **context**: Context propagation for cancellation and timeouts throughout

#### Build Tools
- **Go toolchain**: Compilation and building
- **go mod**: Dependency management
- **goreleaser** (optional): Release automation

#### Testing and Development
- **testing**: Standard Go testing package
- **testify** (optional): Testing assertions and mocks
- **golangci-lint**: Code linting
- **gofmt/goimports**: Code formatting

---

### Code Organization

#### Directory Structure

Following the standard Go project layout:

```
├── cmd/                        # Application entry points
│   └── jj-stacked/              # Main application
│       └── main.go            # Main entry point and command routing
├── internal/                  # Private application code (not importable by external projects)
│   ├── commands/             # Command implementations
│   │   ├── analyze/          # Analyze command
│   │   │   └── analyze.go
│   │   ├── submit/           # Submit command
│   │   │   └── submit.go
│   │   └── auth/             # Auth command
│   │       └── auth.go
│   ├── auth/                 # Authentication logic
│   │   └── auth.go
│   ├── submit/               # Submission workflow (3 phases)
│   │   └── submit.go
│   ├── jjutils/              # Jujutsu integration
│   │   ├── jjutils.go
│   │   └── types.go
│   ├── github/               # GitHub API integration
│   │   └── github.go
│   ├── ui/                   # Terminal UI components (Bubble Tea)
│   │   ├── analyze.go        # Change graph UI
│   │   ├── bookmark_selection.go
│   │   └── remote_selection.go
│   ├── config/               # Configuration management
│   │   └── config.go
│   └── logger/               # Logging utilities
│       └── logger.go
└── pkg/                       # Public library code (importable by external projects)
    └── (reserved for truly reusable components if needed)
```

**Structure Rationale**:
- **`cmd/`**: Contains the application entry points. Each subdirectory is a separate executable.
- **`internal/`**: Contains all application-specific code. The Go compiler enforces that code in `internal/` cannot be imported by external projects, making it ideal for application logic.
- **`pkg/`**: Reserved for library code that's designed to be imported by external projects. Since jj-stacked is primarily a CLI tool with application-specific logic, most code resides in `internal/`. The `pkg/` directory is left available for any truly reusable components that may be extracted in the future.

---

### Data Models

#### Core Types

##### LogEntry
Represents a single change (commit) in the Jujutsu graph.
```go
type LogEntry struct {
    CommitID             string    `json:"commit_id"`              // Short commit ID
    ChangeID             string    `json:"change_id"`              // Jujutsu change ID
    AuthorName           string    `json:"author_name"`            // Author's name
    AuthorEmail          string    `json:"author_email"`           // Author's email
    DescriptionFirstLine string    `json:"description_first_line"` // First line of commit message
    Parents              []string  `json:"parents"`                // Parent commit IDs
    LocalBookmarks       []string  `json:"local_bookmarks"`        // Local bookmarks at this change
    RemoteBookmarks      []string  `json:"remote_bookmarks"`       // Remote bookmarks at this change
    IsCurrentWorkingCopy bool      `json:"is_current_working_copy"` // Whether this is the working copy
    AuthoredAt           time.Time `json:"authored_at"`            // Authoring timestamp
    CommittedAt          time.Time `json:"committed_at"`           // Commit timestamp
}
```

##### Bookmark
Represents a Jujutsu bookmark with sync status.
```go
type Bookmark struct {
    Name      string `json:"name"`       // Bookmark name
    CommitID  string `json:"commit_id"`  // Commit ID it points to
    ChangeID  string `json:"change_id"`  // Change ID it points to
    HasRemote bool   `json:"has_remote"` // Whether remote tracking exists
    IsSynced  bool   `json:"is_synced"`  // Whether local and remote are in sync
}
```

##### BookmarkSegment
Represents a contiguous section of changes between bookmarks.
```go
type BookmarkSegment struct {
    Bookmarks []Bookmark `json:"bookmarks"` // All bookmarks at this segment
    Changes   []LogEntry `json:"changes"`   // Changes in this segment
}
```

##### BranchStack
Represents a complete stack from trunk to a leaf bookmark.
```go
type BranchStack struct {
    Segments []BookmarkSegment `json:"segments"` // Ordered from base to top
}
```

##### ChangeGraph
Complete graph of all bookmarks and their relationships.
```go
type ChangeGraph struct {
    Bookmarks                     map[string]Bookmark    // All bookmarks by name
    BookmarkToChangeID            map[string]string      // Bookmark to change ID mapping
    BookmarkedChangeAdjacencyList map[string]string      // Child to parent mapping
    BookmarkedChangeIDToSegment   map[string][]LogEntry  // Change ID to segment
    StackLeafs                    map[string]bool        // Leaf change IDs (using map as set)
    StackRoots                    map[string]bool        // Root change IDs (using map as set)
    Stacks                        []BranchStack          // All stacks in the graph
    ExcludedBookmarkCount         int                    // Count of excluded bookmarks
}
```

---

## Feature Implementation Details

### Anchor Comments (AIDEV Notes)
**Description**: Special comments throughout the codebase for AI and developer knowledge.

**Comment Types**:
- `AIDEV-NOTE:` - Inline knowledge and architectural explanations
- `AIDEV-TODO:` - Tasks and future work items
- `AIDEV-QUESTION:` - Areas needing clarification

**Purpose**:
- Document complex implementation decisions
- Preserve architectural intent
- Aid AI-assisted development
- Support onboarding and maintenance

**Guidelines**:
- Always check for existing anchors before modifying code
- Update anchors when changing associated code
- Never remove AIDEV-NOTEs without explicit instruction

---

### Security Considerations

#### Credential Management
- No local storage of GitHub tokens
- Relies on GitHub CLI or environment variables
- Tokens validated before use
- Clear separation between auth and business logic
- No logging of sensitive credentials

#### Command Injection Prevention
- Uses `exec.Command` with separate arguments to prevent shell injection
- All arguments properly escaped and validated
- No dynamic command construction from user input

#### API Token Scopes
- Requires only `repo` scope for minimal permissions
- Validates token scopes during auth test
- Clear documentation of required permissions

---

### Performance Optimizations

#### Paginated Change Retrieval
- Retrieves changes in batches of 100
- Early termination when hitting fully-collected bookmarks
- Cursor-based pagination for efficient traversal

#### Bookmark Collection Optimization
- Marks bookmarks as "fully collected" to avoid reprocessing
- Reuses segment information across traversals
- Builds adjacency list incrementally

#### API Request Optimization
- Batches PR checks when possible
- Uses GitHub API filtering to reduce response sizes
- Caches PR information during submission

---

### Extensibility Points

#### Future Enhancement Opportunities
- Support for GitLab, Bitbucket, or other platforms (would require significant refactoring)
- Custom PR templates
- Automated conflict detection
- Integration with CI/CD systems
- Branch protection rule handling
- PR label automation
- Reviewer assignment automation

---

### GitHub Enterprise Support

#### Overview
Full support for GitHub Enterprise (GHE) instances in addition to GitHub.com. The tool automatically detects GHE instances from remote URLs or can be explicitly configured.

#### Configuration Methods

##### 1. Automatic Detection (Recommended)
- Parses git remote URLs to extract the GitHub hostname
- Works for both HTTPS and SSH URL formats
- Example: `git@git.mycompany.com:org/repo.git` → hostname: `git.mycompany.com`

##### 2. Environment Variable Configuration
```bash
export GITHUB_HOST=git.mycompany.com
export GHE_TOKEN=ghp_xxxxxxxxxxxxx  # Optional: separate token for GHE
```

##### 3. GitHub CLI Configuration
```bash
gh auth login --hostname git.mycompany.com
```

#### API Endpoint Resolution
- **GitHub.com**: `https://api.github.com`
- **GitHub Enterprise**: `https://<hostname>/api/v3`
- Endpoint automatically derived from hostname
- Can be overridden via `GITHUB_API_URL` if needed

#### Authentication for GHE
- GitHub CLI: `gh auth token --hostname git.mycompany.com`
- Environment: `GHE_TOKEN` (preferred) or `GITHUB_TOKEN`
- Tokens must be created on the GHE instance, not GitHub.com

#### Multi-Instance Support
- Supports repositories that have remotes pointing to different GitHub instances
- Example: `origin` → `github.com`, `upstream` → `git.mycompany.com`
- Interactive remote selection shows the hostname for clarity
- Each remote uses the appropriate authentication for its host

---

## Limitations and Constraints

### Current Limitations

1. **Merge Commit Support**: Bookmarks containing merge commits or descending from merge commits are excluded from stacks and cannot be submitted.

2. **GitHub Platforms Only**: Supports GitHub.com and GitHub Enterprise. Does not support GitLab, Bitbucket, or other platforms.

3. **Single PR per Segment**: Each bookmark segment results in exactly one PR. No support for splitting segments.

4. **Branch Name Constraints**: Bookmark names must be valid Git branch names and GitHub branch names.

5. **Linear Stacks Only**: Complex graph structures with multiple parents (except for trunk) are not supported.

6. **No Conflict Resolution**: Does not handle or assist with merge conflicts during rebase operations.

7. **Jujutsu Version Dependency**: Requires specific Jujutsu template syntax and revset features available in v0.30.0+.

8. **Default Branch Requirements**: Repository must have one of `main`, `master`, or `trunk` as the default branch name.

---

## Development Guidelines

### Code Style Patterns

#### AIDEV Anchor Comments
- Add anchor comments for complex or important code sections
- Use `AIDEV-NOTE:` for explanations
- Use `AIDEV-TODO:` for pending work
- Use `AIDEV-QUESTION:` for unclear areas
- Always grep for existing anchors before modifying related code

#### Error Handling
- Throw errors with descriptive messages
- Include context in error messages
- Use type-safe error handling where possible
- Provide actionable error messages to users

#### Testing Approach
- Tests encode human intent and should never be modified by AI without explicit instruction
- Test files use Go's standard testing package
- Located in `*_test.go` files alongside source files

---

## Appendix

### Glossary

**Bookmark**: A named pointer to a commit in Jujutsu, similar to Git branches but with different semantics. Bookmarks don't move when creating new commits but do follow rewrites.

**Change**: A Jujutsu concept representing a commit as it evolves over time. Has a stable change ID across rewrites.

**Change ID**: A unique identifier for a change that remains stable across rebases and amendments.

**Commit ID**: The Git SHA hash identifying a specific commit snapshot.

**Segment**: A contiguous sequence of changes between two bookmarks in a stack.

**Stack**: A hierarchical arrangement of bookmarks where each depends on the one below it, ultimately rooting at trunk.

**Trunk**: The main development branch, resolved via Jujutsu's `trunk()` revset to find `main`, `master`, or `trunk` branches.

**Downstack**: Moving toward trunk (from leaf to root).

**Upstack**: Moving away from trunk (from root to leaf).

**Tainted Change**: A change that is or descends from a merge commit, excluded from stacks.

---

### Related Documentation

- **Jujutsu Documentation**: https://jj-vcs.github.io/jj/latest/
- **Jujutsu CLI Reference**: https://jj-vcs.github.io/jj/latest/cli-reference/
- **Jujutsu Revsets**: https://jj-vcs.github.io/jj/latest/revsets/
- **Jujutsu Templates**: Project includes local copies in `jj-docs/`
- **GitHub REST API**: https://docs.github.com/en/rest
- **go-github Documentation**: https://pkg.go.dev/github.com/google/go-github/v58/github
- **Bubble Tea Documentation**: https://github.com/charmbracelet/bubbletea
- **Lipgloss Documentation**: https://github.com/charmbracelet/lipgloss

---

### Version History

- **v1.2.1** (Current): Latest stable release
- Built with Go 1.21+
- Requires Go 1.21+, Jujutsu 0.30.0+
