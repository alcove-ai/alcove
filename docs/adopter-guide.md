# Alcove Adopter Guide

## Who This Guide Is For

This guide is for **users connecting to an existing Alcove instance** to automate development on their own projects. If you need to self-host Alcove, see the [Getting Started Guide](getting-started.md) instead. If you want to contribute code to Alcove itself, see the [Development Guide](development-guide.md).

After completing this guide, you'll know how to:

- Connect the CLI to your Alcove instance
- Create and run your first AI coding agent
- Set up policy rules for secure automation
- Build workflows that automatically implement features, review code, and merge changes
- Schedule agents to run on a cron schedule
- Monitor costs and session activity

## What You Configure Where

Alcove splits configuration between YAML files (single source of truth) and CLI/dashboard (runtime data):

| What | Where | Why |
|------|-------|-----|
| **Agent definitions** | `.alcove/agents/*.yml` in your repo | Version-controlled with your project |
| **Workflows** | `.alcove/workflows/*.yml` in your repo | Part of your development process |
| **Policy rules** | `.alcove/policy-rules/*.yml` in your repo | Security is part of your codebase |
| **Credentials** | CLI (`alcove credentials create`) | Sensitive data, not in git |
| **Teams** | CLI (`alcove teams create`) | User management |
| **Catalog** | Dashboard (read-only) | Browse available agents/tools |

## Prerequisites

Before starting, ensure you have:

1. **Access to an Alcove instance** — Bridge URL and user account
2. **CLI binary** — download from the [CLI Installation Guide](cli-installation.md)
3. **Personal API token** (recommended) — from the dashboard user settings
4. **GitHub or GitLab repository** — where you'll add `.alcove/` directory

You do **not** need podman, Go, or build tools — just the CLI binary and Bridge access.

## Install the CLI

### Quick Installation

**Linux/macOS:**
```bash
curl -fsSL https://raw.githubusercontent.com/alcove-ai/alcove/main/scripts/install.sh | bash
```

**Windows (PowerShell):**
```powershell
iex (iwr -useb 'https://raw.githubusercontent.com/alcove-ai/alcove/main/scripts/install.ps1').Content
```

### Verify Installation

```bash
alcove version
alcove --help
```

For detailed installation options including manual downloads and custom directories, see the [CLI Installation Guide](cli-installation.md).

## Connect to Your Alcove Instance

### Basic Login

Connect to your Alcove instance using your credentials:

```bash
alcove login --server https://<your-bridge-url> --username <your-username>
```

For production use, personal API tokens are more secure than passwords:

```bash
alcove login --server https://<your-bridge-url> --token <your-personal-api-token>
```

Generate personal API tokens in the dashboard under **User Settings** → **API Tokens**. They have the `apat_` prefix.

### Multiple Environments

You can configure multiple environment profiles:

```bash
# Production environment
alcove login --server https://alcove-prod.company.com --username <user> --profile prod

# Staging environment  
alcove login --server https://alcove-staging.company.com --username <user> --profile staging

# Switch between environments
alcove config set-profile prod
alcove config set-profile staging
```

### Validate Connection

Test your connection and view server information:

```bash
alcove config validate
alcove version  # Shows both client and server versions
```

### Corporate Networks

If you're behind a corporate proxy, configure proxy settings:

```bash
# Set environment variables
export HTTP_PROXY=http://proxy.company.com:8080
export HTTPS_PROXY=http://proxy.company.com:8080

# Or use CLI flag
alcove --proxy-url http://proxy.company.com:8080 config validate
```

## Explore the Dashboard

Before creating agents, familiarize yourself with the dashboard (web interface at your Bridge URL):

- **Teams** — view your team memberships and switch team context
- **Sessions** — list running and completed agent sessions
- **Catalog** — browse available agents, tools, and LSP servers
- **Workflows** — view workflow definitions and runs
- **Credentials** — manage LLM providers and service credentials

**Important:** The dashboard is read-only for agent definitions, workflows, and policy rules. These must be created as YAML files in your repository.

## Set Up Your Team

Teams are the ownership unit in Alcove. Every resource belongs to a team.

### Create a Team

```bash
alcove teams create --name "My Development Team" --description "Team for automating our Go web service"
```

### Switch Team Context

All subsequent commands will target this team:

```bash
alcove teams switch "My Development Team"
```

### Invite Team Members

```bash
alcove teams invite --email colleague@company.com --team "My Development Team"
```

### List Teams

```bash
alcove teams list
```

## Register Credentials

Agents need credentials to access external services. Register them via CLI:

### LLM Provider

**For Anthropic Claude:**
```bash
alcove credentials create llm anthropic \
  --name "Primary Claude API" \
  --key "ANTHROPIC_API_KEY" \
  --value "<your-claude-api-key>"
```

**For Google Vertex AI:**
```bash
alcove credentials create llm google-vertex \
  --name "Production Vertex AI" \
  --key "GOOGLE_APPLICATION_CREDENTIALS" \
  --value '<your-service-account-json>' \
  --metadata project_id=your-gcp-project \
  --metadata region=us-east5
```

### Source Control

**For GitHub:**
```bash
alcove credentials create scm github \
  --name "GitHub PAT" \
  --key "GITHUB_TOKEN" \
  --value "<your-github-personal-access-token>"
```

**For GitLab:**
```bash
alcove credentials create scm gitlab \
  --name "GitLab PAT" \
  --key "GITLAB_TOKEN" \
  --value "<your-gitlab-personal-access-token>"
```

### List Credentials

```bash
alcove credentials list
```

**Security Note:** Credentials are encrypted at rest and never exposed to agents directly. Gate (the auth proxy) injects them into requests at runtime.

## Create Your First Agent Definition

Let's create a simple agent that analyzes your codebase and suggests improvements. In your project repository:

### Step 1: Create Directory Structure

```bash
mkdir -p .alcove/agents
```

### Step 2: Create Basic Agent

Create `.alcove/agents/hello.yml`:

```yaml
name: Hello World Agent
description: A simple agent that explores the codebase and suggests improvements
prompt: |
  You are a code analysis agent. Please:
  
  1. Examine the repository structure and identify the main programming language
  2. Look for common code quality issues (unused imports, TODO comments, etc.)
  3. Suggest 3 concrete improvements with file paths and line numbers
  4. Create a summary report in ANALYSIS.md

repos:
  - name: main
    url: https://github.com/<your-org>/<your-repo>.git
    ref: main

timeout: 900  # 15 minutes
budget_usd: 2.00  # Cost limit
model: claude-sonnet-4-20250514
provider: anthropic
```

Replace `<your-org>/<your-repo>` with your actual repository.

### Step 3: Progressive Enhancement

After testing the basic agent, enhance it with additional features:

```yaml
name: Advanced Code Analyzer
description: Comprehensive code analysis with security and performance checks
prompt: |
  You are a senior code analysis agent. Please:
  
  1. Analyze code quality and suggest improvements
  2. Check for security vulnerabilities (SQL injection, XSS, etc.)
  3. Identify performance bottlenecks
  4. Suggest refactoring opportunities
  5. Create a detailed report in ANALYSIS.md with:
     - Executive summary
     - Detailed findings by category
     - Prioritized recommendations

repos:
  - name: main
    url: https://github.com/<your-org>/<your-repo>.git
    ref: main

timeout: 1800  # 30 minutes
budget_usd: 5.00
model: claude-sonnet-4-20250514
provider: anthropic

# Optional: Schedule to run weekly
schedule:
  cron: "0 9 * * 1"  # Mondays at 9 AM
  enabled: false  # Enable after testing

# Optional: Use a custom development environment
dev_container:
  image: golang:1.25-bookworm
  network_access: internal  # or 'external' if needed
```

## Register Your Agent Repository

After creating agent definitions, register the repository with Alcove:

```bash
# Add repository
alcove agents repos add https://github.com/<your-org>/<your-repo>.git

# Trigger sync to pull latest agent definitions
alcove agents sync

# Verify agents are loaded
alcove agents list
```

### Troubleshooting Repository Sync

If your agents don't appear:

```bash
# Check sync status
alcove agents repos status

# View sync logs
alcove agents sync --verbose

# Common issues:
# - Repository URL incorrect
# - No .alcove/agents/*.yml files
# - YAML syntax errors
# - Team doesn't have repo access
```

## Run Your Agent

### Start a Session

```bash
# Run specific agent
alcove agents run hello-world-agent

# Run with custom inputs (for parameterized agents)
alcove agents run hello-world-agent --input branch=feature-123

# Monitor session in real-time
alcove run --agent hello-world-agent --watch
```

### Monitor Running Sessions

```bash
# List all sessions
alcove list

# List only running sessions
alcove list --status running

# Get detailed session info
alcove status <session-id>
```

### Session States

- **queued** — waiting for available resources
- **initializing** — setting up containers and cloning repos
- **running** — agent is actively working
- **completed** — finished successfully
- **failed** — encountered an error
- **cancelled** — manually stopped
- **timeout** — exceeded time limit
- **budget_exceeded** — exceeded cost limit

## Read Session Output

### View Session Transcript

```bash
# Full session transcript (agent's reasoning and actions)
alcove logs <session-id>

# Stream logs in real-time for running sessions
alcove logs <session-id> --follow

# View only the final summary
alcove logs <session-id> --summary
```

### View Proxy Logs

Gate (the auth proxy) logs all external API calls:

```bash
# See what external services the agent accessed
alcove logs <session-id> --proxy

# Filter proxy logs
alcove logs <session-id> --proxy --filter github.com
```

### Download Session Artifacts

Some agents produce files (reports, code changes, etc.):

```bash
# List session outputs
alcove outputs <session-id>

# Download specific file
alcove outputs <session-id> --file ANALYSIS.md --download ./analysis-report.md
```

## Add Policy Rules

Policy rules control what external APIs your agents can access. This is critical for security in production environments.

### Step 1: Create Policy Rules Directory

```bash
mkdir -p .alcove/policy-rules
```

### Step 2: Create Read-Only GitHub Profile

Start with restrictive permissions and expand as needed. Create `.alcove/policy-rules/readonly-github.yml`:

```yaml
name: read-only-github
description: Safe read-only access to GitHub API for code analysis
enforcement_mode: enforce  # Use 'monitor' for testing

rule_sets:
  # Allow basic repository information
  - github-read-issues
  - github-read-prs
  - github-read-contents
  - github-read-commits
  - github-read-branches
  - github-read-git

# Deny all write operations
denied_operations:
  - github-create-issue
  - github-create-comment
  - github-create-pr
  - github-merge-pr
  - github-write-contents
  - github-write-git
```

### Step 3: Update Agent to Use Profile

Add the profile to your agent definition:

```yaml
name: Safe Code Analyzer
# ... other fields ...

profiles:
  - readonly-github
```

### Step 4: Test Enforcement

```bash
# Sync policy rules
alcove agents sync

# Run agent with new profile
alcove agents run safe-code-analyzer

# Check proxy logs for denied requests
alcove logs <session-id> --proxy | grep -i denied
```

### Common Policy Profiles

**Read-Write GitHub (for agents that create PRs):**

```yaml
name: github-contributor
description: Can read repos and create PRs but not merge
enforcement_mode: enforce

rule_sets:
  # Read permissions
  - github-read-issues
  - github-read-prs
  - github-read-contents
  - github-read-commits
  - github-read-branches
  - github-read-git
  
  # Write permissions for PRs
  - github-create-pr
  - github-update-pr
  - github-create-comment
  - github-create-branch
  - github-write-contents
  - github-write-git

denied_operations:
  - github-merge-pr  # Prevent auto-merge
  - github-delete-branch  # Prevent cleanup
```

**Full GitHub Access (for automation pipelines):**

```yaml
name: github-automation
description: Full GitHub API access for SDLC automation
enforcement_mode: enforce

rule_sets:
  # Include all GitHub rule sets
  - github-read-issues
  - github-read-prs
  - github-read-contents
  - github-read-commits
  - github-read-branches
  - github-read-git
  - github-create-comment
  - github-create-issue
  - github-update-issue
  - github-create-pr
  - github-update-pr
  - github-merge-pr
  - github-create-review
  - github-write-contents
  - github-write-git
  - github-create-branch
  - github-delete-branch
```

For complete rule sets, see the existing policy rules:
- [GitHub Rules](../.alcove/policy-rules/github.yml)
- [GitLab Rules](../.alcove/policy-rules/gitlab.yml)

### Enforcement Modes

- **`enforce`** — Block unauthorized requests (production)
- **`monitor`** — Log violations but allow requests (testing)

Start with `monitor` mode when developing new agents, then switch to `enforce` for production use.

## Set Up a Workflow

Workflows chain multiple agents and bridge actions into automated pipelines. Let's create a simple feature implementation workflow.

### Step 1: Create Workflow Directory

```bash
mkdir -p .alcove/workflows
```

### Step 2: Basic Two-Step Workflow

Create `.alcove/workflows/basic-feature.yml`:

```yaml
name: Basic Feature Implementation
description: Implement a feature and create a PR

trigger:
  github_issue_labeled:
    repo: <your-org>/<your-repo>
    label: ready-for-dev

workflow:
  # Step 1: Agent implements the feature
  - id: implement
    type: agent
    agent: developer-agent
    inputs:
      issue_number: "{{trigger.issue_number}}"
      branch: "feature-{{trigger.issue_number}}"
    outputs: [summary, changes_made]

  # Step 2: Bridge creates a pull request
  - id: create-pr
    type: bridge
    action: create-pr
    depends: "implement.Succeeded"
    inputs:
      repo: <your-org>/<your-repo>
      branch: "{{steps.implement.inputs.branch}}"
      title: "Implement #{{trigger.issue_number}}"
      body: |
        Automated implementation of issue #{{trigger.issue_number}}.
        
        ## Changes Made
        {{steps.implement.outputs.changes_made}}
        
        ## Summary
        {{steps.implement.outputs.summary}}
      base: main
```

### Step 3: Advanced Review Loop Workflow

For production use, add review and revision cycles. Create `.alcove/workflows/sdlc-pipeline.yml`:

```yaml
name: Full SDLC Pipeline
description: Complete feature implementation with review loop

trigger:
  github_issue_labeled:
    repo: <your-org>/<your-repo>
    label: ready-for-dev

workflow:
  # Step 1: Claim the issue (remove trigger label, assign bot)
  - id: claim-issue
    type: bridge
    action: update-issue
    inputs:
      repo: <your-org>/<your-repo>
      issue_number: "{{trigger.issue_number}}"
      assignee: alcove-bot
      labels:
        add: [in-progress]
        remove: [ready-for-dev]

  # Step 2: Agent implements the feature
  - id: implement
    type: agent
    agent: developer-agent
    depends: "claim-issue.Succeeded"
    inputs:
      issue_number: "{{trigger.issue_number}}"
      branch: "feature-{{trigger.issue_number}}"
    outputs: [summary, changes_made]

  # Step 3: Bridge creates PR
  - id: create-pr
    type: bridge
    action: create-pr
    depends: "implement.Succeeded"
    inputs:
      repo: <your-org>/<your-repo>
      branch: "{{steps.implement.inputs.branch}}"
      title: "Implement #{{trigger.issue_number}}"
      base: main

  # Step 4: Wait for CI to pass
  - id: await-ci
    type: bridge
    action: await-ci
    depends: "create-pr.Succeeded || ci-fix.Succeeded"
    max_iterations: 4  # Prevent infinite CI retry loops
    inputs:
      repo: <your-org>/<your-repo>
      pr: "{{steps.create-pr.outputs.pr_number}}"

  # Step 5: Fix CI failures if needed
  - id: ci-fix
    type: agent
    agent: developer-agent
    depends: "await-ci.Failed"
    max_iterations: 3
    inputs:
      branch: "{{steps.implement.inputs.branch}}"
      ci_logs: "{{steps.await-ci.outputs.failure_logs}}"
    outputs: [summary]

  # Step 6: Code review (runs after CI passes)
  - id: code-review
    type: agent
    agent: reviewer-agent
    depends: "await-ci.Succeeded || revision.Succeeded"
    max_iterations: 3
    inputs:
      pr: "{{steps.create-pr.outputs.pr_number}}"
    outputs: [approved, comments]
    output_contract:
      required: [approved, comments]
      allowed_values:
        approved: ["true", "false"]
      routing_field: approved
      success_value: "true"

  # Step 7: Address review feedback
  - id: revision
    type: agent
    agent: developer-agent
    depends: "code-review.Failed"
    max_iterations: 3
    inputs:
      branch: "{{steps.implement.inputs.branch}}"
      feedback: "{{steps.code-review.outputs.comments}}"
    outputs: [summary]

  # Step 8: Merge the PR
  - id: merge
    type: bridge
    action: merge-pr
    depends: "code-review.Succeeded"
    inputs:
      repo: <your-org>/<your-repo>
      pr: "{{steps.create-pr.outputs.pr_number}}"
      merge_method: squash
```

### Step 4: Bridge Actions Reference

Common bridge actions:

- **`create-pr`** — Create pull/merge request
- **`update-pr`** — Update PR title, body, or labels
- **`merge-pr`** — Merge an approved PR
- **`await-ci`** — Wait for CI checks to pass
- **`update-issue`** — Add/remove labels, assign users
- **`create-issue`** — Create new issue

For complete action reference, see [Workflow Authoring Guide](workflow-authoring.md).

### Step 5: Depends Expressions

Control workflow flow with `depends` expressions:

```yaml
# Simple dependency
depends: "implement.Succeeded"

# Multiple conditions with AND
depends: "code-review.Succeeded && security-review.Succeeded"

# Multiple conditions with OR (retry scenarios)
depends: "create-pr.Succeeded || ci-fix.Succeeded"

# Complex boolean logic
depends: "(review-1.Succeeded || review-2.Succeeded) && await-ci.Succeeded"
```

For advanced workflow patterns, see [Workflow Authoring Guide](workflow-authoring.md).

## Schedule Agents

Add cron-based scheduling to any agent definition:

### Basic Scheduling

```yaml
name: Weekly Security Audit
# ... agent definition ...

schedule:
  cron: "0 9 * * 1"  # Mondays at 9:00 AM
  enabled: true
  timezone: "America/New_York"  # Optional, defaults to UTC
```

### Cron Expression Examples

| Expression | Description |
|-----------|-------------|
| `0 9 * * 1` | Mondays at 9:00 AM |
| `0 0 * * *` | Daily at midnight |
| `0 */6 * * *` | Every 6 hours |
| `30 14 * * 5` | Fridays at 2:30 PM |
| `0 8 1 * *` | First day of each month at 8:00 AM |

### Disable Scheduling

To temporarily disable scheduled runs without removing the schedule:

```yaml
schedule:
  cron: "0 9 * * 1"
  enabled: false  # Disable scheduling
```

### Monitor Scheduled Runs

```bash
# List upcoming scheduled runs
alcove schedule list

# View schedule history
alcove schedule history --agent weekly-security-audit

# Cancel a scheduled run
alcove schedule cancel <schedule-id>
```

## Use Dev Containers

Dev containers provide isolated environments with project-specific tooling. They're especially useful for builds, tests, and language-specific analysis.

### Basic Dev Container

```yaml
name: Go Test Runner
prompt: |
  Run the full test suite and analyze any failures:
  
  1. Run `go test ./...` in the dev container
  2. If tests fail, analyze the failures and suggest fixes
  3. Create a test report in TEST_RESULTS.md

repos:
  - name: main
    url: https://github.com/<your-org>/<your-go-repo>.git
    ref: main

dev_container:
  image: golang:1.25-bookworm
  network_access: internal  # No external network access

timeout: 1200
budget_usd: 3.00
```

### Network Access Options

- **`internal`** (default) — No external network access, more secure
- **`external`** — Full network access for package installs, API calls

### Available Base Images

Popular dev container images:
- `golang:1.25-bookworm` — Go development
- `node:20-alpine` — Node.js development  
- `python:3.12-bookworm` — Python development
- `ruby:3.3-alpine` — Ruby development
- `openjdk:21-jdk-bookworm` — Java development

### CLAUDE.md Injection

If your repository has a `CLAUDE.md` file, it's automatically appended to the agent prompt to provide project context:

```yaml
name: Context-Aware Agent
prompt: |
  You are a development assistant. Please help with the current task.
  
  # This agent will also see the content of CLAUDE.md from the repository

repos:
  - name: main
    url: https://github.com/<your-org>/<your-repo>.git
    ref: main
```

### Dev Container Commands

Agents can run commands in the dev container using the shim API:

```yaml
prompt: |
  Use the dev container to run project commands:
  
  1. Run `make build` to build the project
  2. Run `make test` to run tests
  3. Run `go vet ./...` to check for issues
  4. Report any problems found

dev_container:
  image: golang:1.25-bookworm
```

## Multi-Repo Agents

Agents can work across multiple repositories simultaneously:

```yaml
name: Cross-Repo Consistency Checker
description: Ensure API contracts are consistent between service and client repos

prompt: |
  Compare the API definitions between the service and client repositories:
  
  1. Check `/workspace/service/api/` for OpenAPI specs
  2. Check `/workspace/client/src/api/` for client implementations  
  3. Report any inconsistencies
  4. Suggest synchronization changes

repos:
  - name: service
    url: https://github.com/<your-org>/user-service.git
    ref: main
  - name: client
    url: https://github.com/<your-org>/user-client.git
    ref: main

timeout: 1800
budget_usd: 4.00
```

### Workspace Layout

Multi-repo agents get a workspace with all repositories:

```
/workspace/
├── service/          # First repo
│   ├── api/
│   └── src/
└── client/           # Second repo
    ├── src/
    └── tests/
```

Refer to repositories by name in your prompts and file paths.

## Cost Control

Monitor and limit agent costs to prevent unexpected charges:

### Agent-Level Limits

```yaml
name: Budget-Controlled Agent
# ... other config ...

timeout: 1800        # 30 minutes maximum
budget_usd: 5.00     # $5 maximum cost
```

Agents stop automatically when they hit either limit.

### Monitor Spending

```bash
# View costs for recent sessions
alcove list --since 24h --format table --columns name,cost,status

# View costs for specific agent
alcove list --agent-name "code-reviewer" --since 7d

# Team spending summary
alcove teams spending --since 30d
```

### Cost Breakdown

Session costs include:

- **LLM API calls** — prompt tokens + completion tokens
- **Compute time** — Skiff pod runtime (typically minimal)
- **Storage** — session transcripts and artifacts (typically minimal)

Most costs come from LLM API usage. Monitor token consumption for high-usage agents.

### Cost Optimization Tips

1. **Use appropriate models** — `claude-haiku` for simple tasks, `claude-sonnet` for complex ones
2. **Set reasonable timeouts** — prevent runaway sessions
3. **Optimize prompts** — clear, concise prompts reduce back-and-forth
4. **Use policy rules** — prevent unnecessary API calls
5. **Monitor regularly** — review high-cost sessions and optimize

## End-to-End Example: Automate Your Development Workflow

Let's tie everything together with a complete `.alcove/` directory structure for a Go web service that automatically implements issues, reviews PRs, and merges changes.

### Directory Structure

```
<your-repo>/
├── .alcove/
│   ├── agents/
│   │   ├── developer.yml
│   │   ├── reviewer.yml
│   │   └── security-scanner.yml
│   ├── workflows/
│   │   ├── feature-pipeline.yml
│   │   └── security-audit.yml
│   └── policy-rules/
│       ├── github-read-write.yml
│       └── github-read-only.yml
├── CLAUDE.md                 # Project context for agents
├── src/
└── tests/
```

### 1. Project Context (CLAUDE.md)

```markdown
# User Service API

This is a Go microservice that provides user authentication and profile management.

## Architecture
- REST API built with Go 1.25 and gorilla/mux
- PostgreSQL database with GORM
- Redis for session storage
- Docker for containerization

## Development Workflow
- Feature branches from `main`
- All changes require PR review
- CI runs tests and security scans
- Squash merge to `main`

## Code Standards
- gofmt, go vet, and golangci-lint must pass
- Unit tests required for new features
- Integration tests for API endpoints
- Documentation for public APIs

## Testing
- `make test` runs unit tests
- `make integration` runs API tests
- `make lint` runs all linters
```

### 2. Developer Agent (`.alcove/agents/developer.yml`)

```yaml
name: Go Developer
description: Full-stack Go developer that implements features and fixes bugs

prompt: |
  You are an experienced Go developer working on a user service API. Your task is to implement the requested feature or fix the reported bug.
  
  ## Process:
  1. Read the issue description carefully
  2. Examine the existing codebase structure
  3. Implement the changes following Go best practices
  4. Add or update unit tests
  5. Run `make test` and `make lint` to verify quality
  6. Create clear commit messages
  7. Push changes to the feature branch
  
  ## Quality Standards:
  - Follow existing code patterns and structure
  - Add comprehensive error handling
  - Include unit tests for new functionality
  - Update documentation for API changes
  - Ensure thread safety for concurrent operations

repos:
  - name: main
    url: https://github.com/<your-org>/user-service.git
    ref: main

dev_container:
  image: golang:1.25-bookworm
  network_access: internal

timeout: 2400  # 40 minutes
budget_usd: 8.00
model: claude-sonnet-4-20250514
provider: anthropic

profiles:
  - github-read-write
```

### 3. Review Agent (`.alcove/agents/reviewer.yml`)

```yaml
name: Code Reviewer
description: Senior Go developer focused on code quality and security

prompt: |
  You are a senior Go developer performing a thorough code review. Analyze the pull request for:
  
  ## Code Quality:
  - Go idioms and best practices
  - Error handling patterns
  - Performance implications
  - Code organization and clarity
  - Documentation completeness
  
  ## Security:
  - Input validation
  - SQL injection prevention
  - Authentication/authorization
  - Data sanitization
  - Dependency vulnerabilities
  
  ## Testing:
  - Test coverage for new features
  - Edge case handling
  - Integration test needs
  - Mock usage appropriateness
  
  ## Output Format:
  Provide your review as structured feedback:
  
  **approved**: true/false
  **comments**: Detailed feedback with specific file/line references
  
  If approved=false, explain what must be fixed before approval.

repos:
  - name: main
    url: https://github.com/<your-org>/user-service.git
    ref: main

timeout: 1800  # 30 minutes
budget_usd: 5.00
model: claude-sonnet-4-20250514
provider: anthropic

profiles:
  - github-read-only
```

### 4. Security Scanner (`.alcove/agents/security-scanner.yml`)

```yaml
name: Security Scanner
description: Automated security analysis for Go applications

prompt: |
  Perform a comprehensive security analysis of this Go web service:
  
  1. **Dependency Vulnerabilities**: Check go.mod for known CVEs
  2. **Code Security**: Look for common vulnerabilities (OWASP Top 10)
  3. **Configuration Issues**: Check for hardcoded secrets, weak defaults
  4. **API Security**: Validate authentication and authorization
  5. **Database Security**: Check for SQL injection risks
  
  Create a security report in SECURITY_REPORT.md with:
  - Executive summary
  - Detailed findings by severity (Critical, High, Medium, Low)
  - Remediation recommendations
  - Compliance notes (if applicable)

repos:
  - name: main
    url: https://github.com/<your-org>/user-service.git
    ref: main

dev_container:
  image: golang:1.25-bookworm
  network_access: external  # For vulnerability database updates

schedule:
  cron: "0 9 * * 1"  # Weekly security scans
  enabled: true

timeout: 3600  # 60 minutes
budget_usd: 10.00
model: claude-sonnet-4-20250514
provider: anthropic

profiles:
  - github-read-only
```

### 5. Feature Pipeline (`.alcove/workflows/feature-pipeline.yml`)

```yaml
name: Automated Feature Pipeline
description: End-to-end automation for implementing GitHub issues

trigger:
  github_issue_labeled:
    repo: <your-org>/user-service
    label: ready-for-dev

workflow:
  - id: claim-issue
    type: bridge
    action: update-issue
    inputs:
      repo: <your-org>/user-service
      issue_number: "{{trigger.issue_number}}"
      assignee: alcove-bot
      labels:
        add: [in-progress]
        remove: [ready-for-dev]

  - id: implement
    type: agent
    agent: go-developer
    depends: "claim-issue.Succeeded"
    max_retries: 2
    inputs:
      issue_number: "{{trigger.issue_number}}"
      branch: "feature-{{trigger.issue_number}}"
    outputs: [summary, changes_made]

  - id: create-pr
    type: bridge
    action: create-pr
    depends: "implement.Succeeded"
    inputs:
      repo: <your-org>/user-service
      branch: "{{steps.implement.inputs.branch}}"
      title: "feat: implement #{{trigger.issue_number}}"
      body: |
        Automated implementation of issue #{{trigger.issue_number}}.
        
        ## Changes Made
        {{steps.implement.outputs.changes_made}}
        
        ## Implementation Summary  
        {{steps.implement.outputs.summary}}
        
        ## Checklist
        - [x] Unit tests added/updated
        - [x] Code follows Go best practices
        - [x] Documentation updated
        - [x] CI passes locally
        
        /cc @team-leads for review
      base: main

  - id: await-ci
    type: bridge
    action: await-ci
    depends: "create-pr.Succeeded || ci-fix.Succeeded"
    max_iterations: 4
    inputs:
      repo: <your-org>/user-service
      pr: "{{steps.create-pr.outputs.pr_number}}"
      timeout: 1200

  - id: ci-fix
    type: agent
    agent: go-developer
    depends: "await-ci.Failed"
    max_iterations: 3
    inputs:
      branch: "{{steps.implement.inputs.branch}}"
      ci_logs: "{{steps.await-ci.outputs.failure_logs}}"
      pr_number: "{{steps.create-pr.outputs.pr_number}}"
    outputs: [summary]

  - id: code-review
    type: agent
    agent: code-reviewer
    depends: "await-ci.Succeeded || revision.Succeeded"
    max_iterations: 3
    inputs:
      pr: "{{steps.create-pr.outputs.pr_number}}"
      repo: <your-org>/user-service
    outputs: [approved, comments]
    output_contract:
      required: [approved, comments]
      allowed_values:
        approved: ["true", "false"]
      routing_field: approved
      success_value: "true"

  - id: revision
    type: agent
    agent: go-developer
    depends: "code-review.Failed"
    max_iterations: 2
    inputs:
      branch: "{{steps.implement.inputs.branch}}"
      feedback: "{{steps.code-review.outputs.comments}}"
      pr_number: "{{steps.create-pr.outputs.pr_number}}"
    outputs: [summary]

  - id: merge
    type: bridge
    action: merge-pr
    depends: "code-review.Succeeded"
    inputs:
      repo: <your-org>/user-service
      pr: "{{steps.create-pr.outputs.pr_number}}"
      merge_method: squash
      delete_branch: true
```

### 6. Policy Rules (`.alcove/policy-rules/github-read-write.yml`)

```yaml
name: github-read-write
description: Read/write GitHub access for development agents
enforcement_mode: enforce

rule_sets:
  # Read permissions
  - github-read-issues
  - github-read-prs
  - github-read-contents
  - github-read-commits
  - github-read-branches
  - github-read-git
  - github-read-actions
  
  # Write permissions for development
  - github-create-comment
  - github-create-pr
  - github-update-pr
  - github-create-branch
  - github-write-contents
  - github-write-git

denied_operations:
  # Prevent dangerous operations
  - github-merge-pr      # Only workflows can merge
  - github-delete-branch # Let workflows handle cleanup
  - github-create-issue  # Prevent spam
```

### 7. Setup Commands

After creating all files:

```bash
# 1. Register repository
alcove agents repos add https://github.com/<your-org>/user-service.git

# 2. Sync agents and workflows
alcove agents sync

# 3. Verify everything loaded
alcove agents list
alcove workflows list

# 4. Test with a simple issue
# Create a GitHub issue and add the "ready-for-dev" label
```

### 8. Monitoring

Monitor your automated pipeline:

```bash
# Watch workflow runs
alcove workflows runs --status running

# Monitor session activity
alcove list --since 1h

# Check costs
alcove teams spending --since 24h

# View specific workflow run details
alcove workflows run-details <run-id>
```

This complete setup provides:
- ✅ Automatic issue implementation when labeled `ready-for-dev`
- ✅ Quality code review before merging
- ✅ CI integration with automatic fixes
- ✅ Bounded retry loops to prevent infinite cycles
- ✅ Weekly security scans
- ✅ Cost controls and monitoring
- ✅ Secure policy rules

## Troubleshooting

### Repository Sync Issues

**Problem:** Agents don't appear after running `alcove agents sync`

**Solutions:**
```bash
# Check repo registration
alcove agents repos list

# Verify YAML syntax
cd .alcove/agents && yamllint *.yml

# Check sync status with details
alcove agents sync --verbose

# Common fixes:
# - Ensure .alcove/agents/ directory exists
# - Verify YAML files have .yml extension (not .yaml)
# - Check that repo URL is accessible
# - Confirm team has access to repository
```

### Policy Rule Violations

**Problem:** Agent sessions fail with "Request denied by policy"

**Solutions:**
```bash
# Check proxy logs for denied requests
alcove logs <session-id> --proxy | grep -i denied

# Temporarily switch to monitor mode
# In .alcove/policy-rules/your-profile.yml:
enforcement_mode: monitor  # Change from 'enforce'

# Common fixes:
# - Add missing rule sets to your policy profile
# - Check if agent needs 'external' network access
# - Verify credentials are registered correctly
# - Ensure policy profile is referenced in agent YAML
```

### Agent Can't Access Services

**Problem:** Agent gets connection errors to GitHub, GitLab, etc.

**Solutions:**
```bash
# Verify credentials are registered
alcove credentials list

# Check if agent needs external network access
# In agent YAML:
dev_container:
  network_access: external  # Change from 'internal'

# Verify corporate proxy settings
export HTTP_PROXY=http://proxy.company.com:8080
alcove --proxy-url $HTTP_PROXY agents run <agent>

# Check that required policy rules are enabled
```

### Session Errors

**Common Session Failures:**

| Error | Cause | Solution |
|-------|--------|----------|
| `timeout` | Agent exceeded time limit | Increase `timeout` in agent YAML or optimize prompts |
| `budget_exceeded` | Hit cost limit | Increase `budget_usd` or use more efficient model |
| `failed_to_clone` | Git access issues | Check repository URL and credentials |
| `policy_violation` | Policy rules blocked request | Update policy rules or switch to `monitor` mode |
| `container_failed` | Dev container issue | Check `dev_container.image` and network settings |
| `no_available_workers` | Resource exhaustion | Contact admin or try again later |

### Workflow Issues

**Problem:** Workflow steps don't trigger properly

**Solutions:**
```bash
# Check workflow syntax
cd .alcove/workflows && yamllint *.yml

# View workflow run details
alcove workflows run-details <run-id>

# Verify trigger configuration
# For GitHub label triggers, ensure:
# - Webhook is configured (admin task)
# - Label name matches exactly
# - Repository URL is correct

# Check depends expressions
# Use simple syntax: "step-id.Succeeded"
# For complex logic: "(step-a.Succeeded || step-b.Succeeded) && step-c.Succeeded"
```

### Performance Issues

**Problem:** Agents are slow or expensive

**Solutions:**
1. **Optimize prompts** — Be specific, avoid repetitive instructions
2. **Use appropriate models** — `claude-haiku` for simple tasks
3. **Enable dev containers** — For build/test tasks requiring tools
4. **Set reasonable timeouts** — Prevent runaway sessions
5. **Monitor token usage** — Check session logs for excessive API calls

### Getting Help

**Debug Information:**
```bash
# Gather diagnostic info
alcove version
alcove config validate
alcove teams list
alcove agents repos status

# Session diagnostics
alcove logs <session-id>
alcove logs <session-id> --proxy
alcove status <session-id>
```

**Contact Support:**
- Include output from diagnostic commands above
- Provide session IDs for failing runs
- Describe expected vs actual behavior
- Include relevant YAML configuration files (remove sensitive data)

## Next Steps

Now that you have Alcove set up for your project, explore these advanced topics:

### Advanced Guides

- **[Workflow Authoring Guide](workflow-authoring.md)** — Complex workflow patterns, parallel execution, conditional logic
- **[Configuration Reference](configuration.md)** — Complete YAML schema and options reference
- **[CLI Reference](cli-reference.md)** — Full command documentation with examples

### Starter Agents

The Alcove repository includes proven agent definitions you can copy and customize:

```bash
# Browse starter agents
ls /workspace/starter-agents/.alcove/agents/

# Example starter agents:
# - autonomous-developer.yml — Full-stack development agent
# - code-reviewer.yml — Thorough code review agent
# - security-auditor.yml — Security vulnerability scanner
# - doc-updater.yml — Documentation maintenance agent
# - test-runner.yml — Automated testing agent
```

### Integration Patterns

**CI/CD Integration:**
- Use `alcove agents run` in your CI pipeline
- Trigger workflows from CI events via webhooks
- Export workflow results for reporting

**Issue Tracking Integration:**
- GitHub Issues (built-in label triggers)
- GitLab Issues (built-in triggers)
- JIRA (policy rules provided)
- Custom webhooks for other systems

**Notification Integration:**
- Slack notifications via webhook agents
- Email alerts via SMTP bridges
- Custom integrations via API

### Production Deployment

When ready to scale Alcove for team use:

1. **Set up production instance** — See [Getting Started Guide](getting-started.md)
2. **Configure SSO** — OIDC, SAML, or corporate identity providers
3. **Set up monitoring** — Session metrics, cost tracking, error alerting
4. **Establish governance** — Agent review process, policy standards
5. **Train team members** — Share this adopter guide and best practices

### Community

Join the Alcove community for tips, examples, and support:

- **GitHub Discussions** — Ask questions and share agent definitions
- **Documentation** — Contribute improvements and examples
- **Issue Reports** — Help improve Alcove by reporting bugs

**Happy automating!** 🤖

---

*This guide covered the essentials of using Alcove as an adopter. For contributing to Alcove's development, see the [Development Guide](development-guide.md). For self-hosting Alcove, see the [Getting Started Guide](getting-started.md).*