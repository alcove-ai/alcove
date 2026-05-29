# Adopter Guide: Using Alcove to Automate Your Development

This guide walks you through using Alcove to develop your own application against an existing Alcove instance. You'll learn to connect your CLI, create agent definitions, run sessions, and set up automated workflows for your project.

## Who This Guide Is For

**You are an adopter** if you:
- Have access to an existing Alcove Bridge instance (URL + account)
- Want to use Alcove to automate development tasks on your own project
- Are looking to integrate AI-powered coding agents into your workflow

**This guide is NOT for you** if you:
- Want to self-host Alcove (see [Getting Started](getting-started.md))
- Want to contribute to Alcove itself (see [Development Guide](development-guide.md))
- Are looking for complete CLI command reference (see [CLI Reference](cli-reference.md))

## What You Configure Where

| Resource | Where to Configure | Tool Used |
|----------|-------------------|-----------|
| Agent definitions | `.alcove/agents/*.yml` | YAML files in your repo |
| Workflows | `.alcove/workflows/*.yml` | YAML files in your repo |
| Policy rules | `.alcove/policy-rules/*.yml` | YAML files in your repo |
| Credentials (API keys) | Bridge database | `alcove credentials` CLI |
| Teams and members | Bridge database | `alcove teams` CLI |
| Session monitoring | Read-only | Dashboard + `alcove logs` CLI |

**Key insight:** Agent definitions, workflows, and security policies live in YAML files in your repository. The API and dashboard are for credentials, teams, and monitoring—not for creating agents.

## Prerequisites

Before you begin, ensure you have:
- **Bridge URL** — the URL of your Alcove instance (e.g., `https://alcove.company.com`)
- **Account** — username/password or API token for the Bridge instance
- **Project repository** — a Git repo where you want to add Alcove automation
- **API credentials** — tokens for GitHub/GitLab and an LLM provider (Anthropic or Google Vertex)

**What you DON'T need:**
- podman or Docker (sessions run on the Bridge instance, not locally)
- Go compiler (agents run in sandboxed containers)
- Kubernetes cluster (provided by the Bridge instance)

## Install the CLI

Follow the [CLI Installation Guide](cli-installation.md) to install the `alcove` binary.

Quick install:
```bash
# Linux/macOS
curl -fsSL https://raw.githubusercontent.com/alcove-ai/alcove/main/scripts/install.sh | bash

# Windows PowerShell
iex (iwr -useb 'https://raw.githubusercontent.com/alcove-ai/alcove/main/scripts/install.ps1').Content

# Verify installation
alcove version
```

## Connect to Your Alcove Instance

### Login with Username/Password

```bash
alcove login https://your-bridge-instance.com
# Enter username and password when prompted
```

### Login with API Token (Recommended)

Personal API tokens (starting with `apat_`) are more secure than passwords:

```bash
# Create token in dashboard: Profile → API Tokens → Create
alcove login https://your-bridge-instance.com --token apat_your_token_here
```

### Verify Connection

```bash
alcove config validate
# Should show: ✓ Configuration is valid
# Should show: ✓ Connected to Bridge at https://...

alcove version
# Shows both client and server versions
```

### Multiple Environments

You can manage multiple Alcove instances with profiles:

```bash
# Login to staging
alcove login https://staging.company.com --profile staging

# Login to production  
alcove login https://alcove.company.com --profile prod

# Switch between environments
alcove config set-profile staging
alcove config set-profile prod

# Use a specific profile for one command
alcove list --profile staging
```

**Troubleshooting:**
- **Certificate errors**: Use `--insecure` flag for self-signed certificates
- **Corporate proxies**: Set `HTTP_PROXY`/`HTTPS_PROXY` environment variables or use `--proxy-url`
- **Permission denied**: Check your username/password or API token

## Explore the Dashboard

Before creating agents, familiarize yourself with the dashboard. Open your Bridge URL in a browser and log in.

### Key Dashboard Sections

**Teams** — Every resource belongs to a team. You'll see:
- Teams you belong to
- Members in each team
- Team-specific resources (agents, credentials, workflows)

**Catalog** — Available tools and agents:
- Official starter agents (code reviewer, issue implementer, etc.)
- Custom agents synced from your repos
- Tools and extensions your team has enabled

**Sessions** — Running and completed sessions:
- Real-time status of active agents
- Session transcripts and outputs
- Resource usage and costs

**Workflows** — Automated pipelines:
- Workflow definitions from your repos
- Running workflow instances
- Execution history and logs

**Note:** The dashboard is read-only for agent definitions. You create agents by adding YAML files to your repository, not through the web interface.

## Set Up Your Team

If you're just getting started, you'll need to set up your team context.

### List Available Teams

```bash
alcove teams list
# Shows teams you're a member of
```

### Create a Team (if needed)

```bash
alcove teams create "My Development Team"
```

### Set Team Context

All subsequent commands will operate in this team's scope:

```bash
alcove teams switch "My Development Team"

# Or use --team flag for individual commands
alcove list --team "My Development Team"
```

### Invite Team Members

```bash
alcove teams invite "My Development Team" colleague@company.com
```

## Register Credentials

Agents need API credentials to interact with external services. Store these securely via the CLI—never put real tokens in YAML files.

### LLM Provider Credentials

**For Anthropic API:**
```bash
alcove credentials create anthropic \
  --key api-key \
  --value sk-ant-your-anthropic-key-here \
  --description "Anthropic API for agents"
```

**For Google Vertex AI:**
```bash
# Save service account JSON to a file first
alcove credentials create google-vertex \
  --key service-account-json \
  --file /path/to/service-account.json \
  --description "Google Cloud service account for Vertex AI"

# Also store project ID and region
alcove credentials create google-vertex-project \
  --key project-id \
  --value your-gcp-project-id

alcove credentials create google-vertex-region \
  --key region \
  --value us-east5
```

### Source Code Management

**For GitHub:**
```bash
alcove credentials create github \
  --key pat \
  --value ghp_your_github_token_here \
  --description "GitHub PAT with repo access"
```

**For GitLab:**
```bash
alcove credentials create gitlab \
  --key pat \
  --value glpat-your-gitlab-token-here \
  --description "GitLab PAT with api scope"
```

### Verify Credentials

```bash
alcove credentials list
# Shows stored credentials (values are hidden)
```

**Security note:** Credentials are encrypted in the Bridge database and never appear in agent transcripts. Agents receive only temporary tokens through the Gate proxy.

## Create Your First Agent Definition

Let's create a simple agent that can analyze your codebase. Agents are defined in `.alcove/agents/*.yml` files in your repository.

### Step 1: Create Directory Structure

In your project repository:
```bash
mkdir -p .alcove/agents
```

### Step 2: Create a Minimal Agent

Create `.alcove/agents/hello.yml`:

```yaml
name: Code Analyzer
prompt: |
  You are a code analyzer. Examine the codebase and provide:
  
  1. A summary of what this project does
  2. The main programming language and frameworks used
  3. Potential areas for improvement
  4. Any obvious bugs or security issues
  
  Focus on actionable insights that would help a developer understand and improve this code.

repos:
  - name: main
    url: <your-org>/<your-repo>
    ref: main

timeout: 600
budget_usd: 5.0
```

### Step 3: Add Progressive Configuration

Enhance your agent with provider and model settings:

```yaml
name: Code Analyzer
description: Analyzes code quality and suggests improvements
prompt: |
  You are a code analyzer. Examine the codebase and provide:
  
  1. A summary of what this project does
  2. The main programming language and frameworks used
  3. Potential areas for improvement
  4. Any obvious bugs or security issues
  
  Focus on actionable insights that would help a developer understand and improve this code.

repos:
  - name: main
    url: <your-org>/<your-repo>
    ref: main

# Cost and time controls
timeout: 600          # 10 minutes max
budget_usd: 5.0      # $5 spending limit

# LLM configuration
provider: anthropic   # or "google-vertex"
model: claude-sonnet-4-20250514
credentials:
  ANTHROPIC_API_KEY: anthropic  # references credential named "anthropic"
```

**For Vertex AI, use:**
```yaml
provider: google-vertex
model: claude-sonnet-4-20250514
credentials:
  GOOGLE_VERTEX_SA_JSON: google-vertex
  GOOGLE_VERTEX_PROJECT: google-vertex-project
  GOOGLE_VERTEX_REGION: google-vertex-region
```

### Step 4: Commit and Push

```bash
git add .alcove/
git commit -m "Add Alcove code analyzer agent"
git push
```

**Placeholder reminder:** Replace `<your-org>/<your-repo>` with your actual repository URL throughout this guide.

## Register Your Agent Repository

Tell Alcove to monitor your repository for agent definitions:

### Add Repository

```bash
alcove agents repos add https://github.com/<your-org>/<your-repo>
```

### Trigger Sync

Force an immediate sync to pick up your new agent:

```bash
alcove agents sync
```

### Verify Agent Registration

```bash
alcove agents list
# Should show your "Code Analyzer" agent

# Get detailed agent info
alcove agents describe "Code Analyzer"
```

**Troubleshooting:**
- **Agent not appearing**: Check that your YAML file is valid and in `.alcove/agents/`
- **Sync failed**: Verify repository permissions and that the repo is accessible
- **YAML errors**: Use `alcove agents validate` to check syntax locally

## Run Your Agent

Now let's run your first agent and see it in action:

### Start a Session

```bash
alcove agents run "Code Analyzer"
# Returns a session ID like: ses_abc123
```

### Watch Progress in Real-time

```bash
alcove run --watch --agent "Code Analyzer"
# Shows live updates as the agent works
```

### Check Session Status

```bash
alcove status ses_abc123
# Shows current state: pending, running, completed, failed
```

### Alternative: Quick Ad-hoc Run

For simple tasks, you can run agents without predefined definitions:

```bash
alcove run "Review the README file and suggest improvements" \
  --repo <your-org>/<your-repo> \
  --timeout 300 \
  --budget 2.0
```

## Read Session Output

Once your session completes, examine the results:

### View the Main Transcript

```bash
alcove logs ses_abc123
# Shows the complete agent conversation and actions
```

### View Proxy Activity

See what external APIs your agent accessed:

```bash
alcove logs ses_abc123 --proxy
# Shows GitHub API calls, LLM requests, etc.
```

### Export Session Data

For deeper analysis or sharing:

```bash
alcove export ses_abc123
# Creates a JSON file with complete session data
```

### Monitor Multiple Sessions

```bash
alcove list --since 1h          # Last hour
alcove list --status running    # Only active sessions
alcove list --agent "Code Analyzer"  # Specific agent
```

**Understanding output:**
- **Main transcript**: Shows the agent's reasoning and file changes
- **Proxy logs**: Shows external API calls (usually successful, denied calls are flagged)
- **Exit code**: 0 = success, non-zero = error or cancellation

## Add Policy Rules

By default, agents have broad access. Use policy rules to control what agents can do for security and cost control.

### Create Policy Rules Directory

```bash
mkdir -p .alcove/policy-rules
```

### Read-Only GitHub Access

Create `.alcove/policy-rules/readonly-github.yml`:

```yaml
rule_sets:
  - name: github-readonly
    enforcement_mode: enforce  # or "monitor" to log violations without blocking
    rules:
      # Allow repository information
      - allow:
          method: GET
          host: api.github.com
          path: "/repos/*/*"
      - allow:
          method: GET  
          host: api.github.com
          path: "/repos/*/contents/**"
      
      # Allow user information
      - allow:
          method: GET
          host: api.github.com
          path: "/user"
      
      # Block all write operations
      - deny:
          method: POST
          host: api.github.com
          path: "/**"
      - deny:
          method: PUT
          host: api.github.com
          path: "/**"
      - deny:
          method: PATCH
          host: api.github.com
          path: "/**"
      - deny:
          method: DELETE
          host: api.github.com
          path: "/**"
```

### Update Agent to Use Policy Rules

Add the `profiles` field to your agent definition:

```yaml
name: Code Analyzer
description: Analyzes code quality and suggests improvements
prompt: |
  You are a code analyzer. Examine the codebase and provide insights.

repos:
  - name: main
    url: <your-org>/<your-repo>
    ref: main

# Security policy
profiles:
  - readonly-github

timeout: 600
budget_usd: 5.0
provider: anthropic
model: claude-sonnet-4-20250514
credentials:
  ANTHROPIC_API_KEY: anthropic
```

### Common Policy Scenarios

**Read-write GitHub with PR creation:**
```yaml
rule_sets:
  - name: github-pr-creation
    enforcement_mode: enforce
    rules:
      # All read operations
      - allow:
          method: GET
          host: api.github.com
          path: "/**"
      
      # PR and branch management
      - allow:
          method: POST
          host: api.github.com
          path: "/repos/*/pulls"
      - allow:
          method: PATCH
          host: api.github.com
          path: "/repos/*/pulls/*"
      
      # Git references (branches, tags)
      - allow:
          method: POST
          host: api.github.com
          path: "/repos/*/git/refs"
      
      # Block dangerous operations
      - deny:
          method: DELETE
          host: api.github.com
          path: "/repos/*"
      - deny:
          method: PATCH
          host: api.github.com
          path: "/repos/*/settings"
```

**Budget and LLM controls:**
```yaml
rule_sets:
  - name: cost-control
    enforcement_mode: enforce
    rules:
      # Limit to specific LLM models
      - allow:
          method: POST
          host: api.anthropic.com
          path: "/v1/messages"
          conditions:
            - json_path: "$.model"
              equals: "claude-sonnet-4-20250514"
      
      # Block expensive models
      - deny:
          method: POST
          host: api.anthropic.com
          path: "/v1/messages"
          conditions:
            - json_path: "$.model"
              contains: "opus"
```

### Commit Policy Rules

```bash
git add .alcove/policy-rules/
git commit -m "Add read-only GitHub policy rules"
git push

# Re-sync to pick up new policies
alcove agents sync
```

## Set Up a Workflow

Individual agents are useful, but workflows let you chain multiple agents and bridge actions into automated pipelines.

### Create Workflow Directory

```bash
mkdir -p .alcove/workflows
```

### Minimal 2-Step Workflow

Create `.alcove/workflows/code-review.yml`:

```yaml
name: Code Review Workflow

workflow:
  # Step 1: Analyze the code
  - id: analyze
    type: agent
    agent: Code Analyzer
    outputs: [summary, issues]
  
  # Step 2: Create an issue if problems found
  - id: create-issue
    type: bridge
    action: create-issue
    depends: "analyze.Succeeded && analyze.issues"
    inputs:
      title: "Code Analysis Results"
      body: |
        ## Analysis Summary
        {{analyze.summary}}
        
        ## Issues Found
        {{analyze.issues}}
        
        Created by Alcove Code Review Workflow
```

### Advanced: Review Loop Pattern

For production workflows, add review and iteration cycles:

```yaml
name: Feature Implementation Pipeline

workflow:
  # Step 1: Implement the feature
  - id: implement
    type: agent
    agent: Feature Developer
    inputs:
      issue_number: "{{trigger.issue_number}}"
      branch: "feature-{{trigger.issue_number}}"
    outputs: [summary, branch]
  
  # Step 2: Create pull request
  - id: create-pr
    type: bridge
    action: create-pr
    depends: "implement.Succeeded"
    inputs:
      branch: "{{implement.branch}}"
      title: "Implement feature from issue #{{trigger.issue_number}}"
      body: "{{implement.summary}}"
    outputs: [pr_number]
  
  # Step 3: Wait for CI to pass
  - id: await-ci
    type: bridge
    action: await-ci
    depends: "create-pr.Succeeded"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
    timeout: 1800  # 30 minutes
  
  # Step 4: Code review (parallel with security review)
  - id: code-review
    type: agent
    agent: PR Reviewer
    depends: "await-ci.Succeeded"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
    outputs: [approved, comments]
  
  - id: security-review
    type: agent
    agent: Security Reviewer  
    depends: "await-ci.Succeeded"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
    outputs: [approved, findings]
  
  # Step 5: Address review feedback (if rejected)
  - id: revision
    type: agent
    agent: Feature Developer
    depends: "(code-review.Succeeded && !code-review.approved) || (security-review.Succeeded && !security-review.approved)"
    max_iterations: 3  # Limit revision loops
    inputs:
      pr_number: "{{create-pr.pr_number}}"
      code_feedback: "{{code-review.comments}}"
      security_feedback: "{{security-review.findings}}"
    outputs: [revised]
  
  # Step 6: Merge when both reviews pass
  - id: merge
    type: bridge
    action: merge-pr
    depends: "code-review.approved && security-review.approved"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
```

### Workflow Triggers

You can trigger workflows manually or on schedule:

```yaml
name: Daily Code Quality Check

# Trigger every weekday at 9 AM
trigger:
  schedule:
    cron: "0 9 * * 1-5"

workflow:
  - id: quality-check
    type: agent
    agent: Code Analyzer
    outputs: [quality_score, recommendations]
  
  - id: notify-team
    type: bridge
    action: send-notification
    depends: "quality-check.quality_score < 8.0"
    inputs:
      message: "Code quality score: {{quality-check.quality_score}}. Review needed."
```

### Run Workflows

```bash
# Manual trigger
alcove workflows run "Code Review Workflow"

# With inputs
alcove workflows run "Feature Implementation Pipeline" \
  --input issue_number=123

# Monitor progress
alcove workflows runs --status running
alcove workflows run-status <run-id>
```

## Schedule Agents

For regular automation, add scheduling directly to agent definitions:

### Add Schedule to Agent

```yaml
name: Daily Security Scan
description: Scans codebase for security vulnerabilities

schedule:
  cron: "0 2 * * *"  # Daily at 2 AM UTC
  enabled: true

prompt: |
  Scan the codebase for security vulnerabilities and generate a report.
  
  Focus on:
  - Hardcoded credentials or secrets
  - SQL injection risks
  - XSS vulnerabilities
  - Dependency vulnerabilities
  
  Create a summary with severity levels and recommended fixes.

repos:
  - name: main
    url: <your-org>/<your-repo>
    ref: main

timeout: 1800
budget_usd: 10.0
provider: anthropic
model: claude-sonnet-4-20250514
credentials:
  ANTHROPIC_API_KEY: anthropic

profiles:
  - readonly-github
```

### Cron Schedule Format

```yaml
schedule:
  cron: "minute hour day-of-month month day-of-week"
  enabled: true
```

**Examples:**
- `"0 9 * * 1-5"` — Weekdays at 9 AM
- `"*/15 * * * *"` — Every 15 minutes
- `"0 0 1 * *"` — First day of every month at midnight
- `"0 2 * * 0"` — Sundays at 2 AM

### Monitor Scheduled Runs

```bash
alcove list --since 24h --scheduled
alcove agents status "Daily Security Scan"
```

## Use Dev Containers

For agents that need to build, test, or run your project, use dev containers to provide the right environment.

### When to Use Dev Containers

- **Building**: Agents need to compile code, run tests, or use build tools
- **Language-specific tools**: Project uses specific versions of Node.js, Python, Go, etc.
- **Dependencies**: Agent needs databases, caches, or other services
- **Testing**: Running unit tests, integration tests, or linting

### Add Dev Container to Agent

```yaml
name: Test Runner
description: Runs project tests and reports results

dev_container:
  image: node:18-slim  # or your custom image
  network_access: internal  # or "external" if tests need internet

prompt: |
  You have access to a development container with Node.js 18 installed.
  
  Your task:
  1. Install project dependencies with `npm install`
  2. Run the test suite with `npm test`
  3. Analyze any failing tests and suggest fixes
  4. If tests pass, run linting with `npm run lint`
  
  Use the /exec command to run shell commands in the dev container.

repos:
  - name: main
    url: <your-org>/<your-repo>
    ref: main

timeout: 1200
budget_usd: 8.0
provider: anthropic
model: claude-sonnet-4-20250514
credentials:
  ANTHROPIC_API_KEY: anthropic

profiles:
  - readonly-github
```

### Custom Dev Container Images

For complex environments, build your own dev container:

```dockerfile
# Dockerfile.dev
FROM node:18-slim

# Install additional tools
RUN apt-update && apt-get install -y \
  git \
  python3 \
  postgresql-client \
  && rm -rf /var/lib/apt/lists/*

# Install project-specific tools
RUN npm install -g typescript eslint jest

# Set working directory
WORKDIR /workspace
```

```yaml
dev_container:
  image: your-registry.com/your-project/dev:latest
  network_access: external  # if container needs to fetch dependencies
```

### CLAUDE.md Injection

If your repository has a `CLAUDE.md` file, its contents are automatically appended to the agent prompt:

```markdown
# CLAUDE.md - Project Context

## Project Structure

This is a React web application with:
- Frontend: React 18 + TypeScript + Vite
- Backend: Node.js + Express + PostgreSQL  
- Testing: Jest + React Testing Library
- Deployment: Docker + Kubernetes

## Development Workflow

1. Run `npm install` to install dependencies
2. Use `npm run dev` for development server
3. Run `npm test` for unit tests
4. Run `npm run lint` for code linting
5. Use `npm run build` for production build

## Code Standards

- Use TypeScript for all new code
- Follow ESLint configuration in .eslintrc.js
- Write tests for all new features
- Use Prettier for code formatting

## Important Notes

- Database migrations are in migrations/
- Environment variables are documented in .env.example
- API routes follow REST conventions
```

The agent receives this context automatically—no need to repeat it in the prompt.

## Multi-Repo Agents

Some tasks require working across multiple repositories. Agents can clone and work with several repos simultaneously.

### Multiple Repository Configuration

```yaml
name: Cross-Repo Dependency Updater
description: Updates shared library version across multiple projects

repos:
  - name: shared-lib
    url: <your-org>/shared-library
    ref: main
  - name: web-app
    url: <your-org>/web-application  
    ref: main
  - name: mobile-app
    url: <your-org>/mobile-application
    ref: main
  - name: api-service
    url: <your-org>/api-service
    ref: develop

prompt: |
  You have access to multiple repositories in your workspace:
  
  - /workspace/shared-lib/ — the shared library
  - /workspace/web-app/ — web application
  - /workspace/mobile-app/ — mobile application  
  - /workspace/api-service/ — API service
  
  Your task:
  1. Check the latest version of the shared library
  2. Update package.json/requirements.txt in each consuming project
  3. Run tests to ensure compatibility
  4. Create separate PRs for each project with appropriate commit messages
  
  Work on one repository at a time and test thoroughly.

timeout: 2400  # 40 minutes for multi-repo work
budget_usd: 15.0
provider: anthropic
model: claude-sonnet-4-20250514
credentials:
  ANTHROPIC_API_KEY: anthropic

profiles:
  - github-pr-creation
```

### Workspace Layout

With multiple repos, the agent's workspace looks like:

```
/workspace/
  shared-lib/          # First repo
    package.json
    src/
    ...
  web-app/             # Second repo  
    package.json
    src/
    ...
  mobile-app/          # Third repo
    package.json
    src/
    ...
  api-service/         # Fourth repo
    requirements.txt
    api/
    ...
```

### Multi-Repo Best Practices

- **Start with discovery**: List all repos and their current state
- **Work sequentially**: Focus on one repo at a time
- **Test thoroughly**: Run tests in each repo before moving to the next
- **Coordinate changes**: Ensure version compatibility across repos
- **Create focused PRs**: One PR per repo with clear descriptions

## Cost Control

AI agents can consume significant resources. Use these mechanisms to control costs:

### Agent-Level Controls

```yaml
name: Expensive Analysis Agent

# Time limit (seconds)
timeout: 1800  # 30 minutes maximum

# Cost limit (USD)
budget_usd: 25.0  # Stop if session exceeds $25

# Model selection (smaller models cost less)
model: claude-sonnet-4-20250514  # vs claude-opus-4-20250514

prompt: |
  You have a 30-minute timeout and $25 budget for this task.
  Be efficient with your LLM calls and focus on the most important issues.
```

### Policy-Based Cost Controls

Limit expensive operations via policy rules:

```yaml
# .alcove/policy-rules/cost-control.yml
rule_sets:
  - name: llm-cost-control
    enforcement_mode: enforce
    rules:
      # Block expensive models
      - deny:
          method: POST
          host: api.anthropic.com
          path: "/v1/messages"
          conditions:
            - json_path: "$.model"
              contains: "opus"
      
      # Limit token count
      - deny:
          method: POST
          host: api.anthropic.com
          path: "/v1/messages"
          conditions:
            - json_path: "$.max_tokens"
              greater_than: 8192
```

### Monitor Usage

Track spending across sessions:

```bash
# View session costs
alcove list --since 7d --show-cost

# Detailed cost breakdown
alcove cost-report --team "My Development Team" --since 30d

# Set team spending alerts
alcove teams cost-alert "My Development Team" --limit 500.0 --period monthly
```

### Cost Optimization Tips

- **Use Sonnet over Opus**: Claude Sonnet is faster and cheaper for most tasks
- **Set reasonable timeouts**: Most tasks complete in 10-30 minutes
- **Cache common results**: Avoid re-running expensive analysis repeatedly
- **Batch operations**: Process multiple files in one session vs. separate sessions
- **Profile-specific budgets**: Set lower budgets for experimental agents

## End-to-End Example: Automate Your Development Workflow

Let's put everything together by setting up Alcove to automatically implement GitHub issues on your project.

### Scenario

You want Alcove to:
1. Monitor GitHub issues labeled `ready-for-implementation`
2. Automatically implement the feature described in the issue
3. Create a pull request with tests
4. Wait for CI to pass and request human review
5. Automatically merge when approved

### Complete .alcove/ Directory Structure

```
.alcove/
├── agents/
│   ├── feature-developer.yml     # Implements features
│   ├── pr-reviewer.yml           # Reviews pull requests  
│   └── ci-fixer.yml              # Fixes CI failures
├── workflows/
│   └── feature-pipeline.yml     # End-to-end automation
├── policy-rules/
│   ├── github-dev.yml            # Read-write GitHub access
│   └── cost-control.yml          # Budget and model limits
└── README.md                     # Team documentation
```

### 1. Feature Developer Agent

`.alcove/agents/feature-developer.yml`:

```yaml
name: Feature Developer
description: Implements features from GitHub issues with tests

dev_container:
  image: node:18-slim
  network_access: internal

prompt: |
  You are a senior software engineer implementing a feature based on a GitHub issue.
  
  Your task:
  1. Read the issue description carefully 
  2. Analyze the existing codebase structure
  3. Implement the feature with clean, maintainable code
  4. Write comprehensive tests (unit and integration)
  5. Update documentation if needed
  6. Ensure code follows the project's style guidelines
  
  Work incrementally and test frequently. Create focused commits with clear messages.
  Before finishing, run the full test suite to ensure nothing is broken.

repos:
  - name: main
    url: <your-org>/<your-repo>
    ref: main

timeout: 3600  # 1 hour
budget_usd: 20.0
provider: anthropic
model: claude-sonnet-4-20250514
credentials:
  ANTHROPIC_API_KEY: anthropic

profiles:
  - github-dev
  - cost-control
```

### 2. PR Reviewer Agent

`.alcove/agents/pr-reviewer.yml`:

```yaml
name: PR Reviewer
description: Reviews pull requests for code quality and best practices

prompt: |
  You are an experienced code reviewer. Analyze this pull request and provide feedback.
  
  Review criteria:
  - Code quality and maintainability
  - Test coverage and quality
  - Security considerations  
  - Performance implications
  - Documentation updates
  - Adherence to project standards
  
  Provide specific, actionable feedback. If you approve, say "APPROVED".
  If changes are needed, explain what should be improved.

repos:
  - name: main
    url: <your-org>/<your-repo>
    ref: main

timeout: 900  # 15 minutes
budget_usd: 8.0
provider: anthropic
model: claude-sonnet-4-20250514
credentials:
  ANTHROPIC_API_KEY: anthropic

profiles:
  - readonly-github
```

### 3. GitHub Policy Rules

`.alcove/policy-rules/github-dev.yml`:

```yaml
rule_sets:
  - name: github-development
    enforcement_mode: enforce
    rules:
      # Read operations
      - allow:
          method: GET
          host: api.github.com
          path: "/**"
      
      # Branch and PR management
      - allow:
          method: POST
          host: api.github.com
          path: "/repos/*/git/refs"
      - allow:
          method: POST
          host: api.github.com
          path: "/repos/*/pulls"
      - allow:
          method: PATCH
          host: api.github.com
          path: "/repos/*/pulls/*"
      
      # Issue management
      - allow:
          method: PATCH
          host: api.github.com
          path: "/repos/*/issues/*"
      
      # Git operations via SSH/HTTPS
      - allow:
          method: "*"
          host: github.com
          path: "/**"
      
      # Block dangerous repository operations
      - deny:
          method: DELETE
          host: api.github.com
          path: "/repos/*"
      - deny:
          method: PATCH
          host: api.github.com
          path: "/repos/*/settings"
```

### 4. Feature Pipeline Workflow

`.alcove/workflows/feature-pipeline.yml`:

```yaml
name: Automated Feature Implementation

# Trigger when GitHub issue gets labeled
trigger:
  github:
    events: ["issues.labeled"]
    label: "ready-for-implementation"

workflow:
  # Step 1: Claim the issue
  - id: claim-issue
    type: bridge
    action: assign-issue
    inputs:
      issue_number: "{{trigger.issue.number}}"
      assignee: "alcove-bot"
    outputs: [assigned]
  
  # Step 2: Implement the feature
  - id: implement
    type: agent
    agent: Feature Developer
    depends: "claim-issue.Succeeded"
    inputs:
      issue_number: "{{trigger.issue.number}}"
      issue_title: "{{trigger.issue.title}}"
      issue_body: "{{trigger.issue.body}}"
      branch: "feature-{{trigger.issue.number}}"
    outputs: [summary, branch_created, files_changed]
  
  # Step 3: Create pull request
  - id: create-pr
    type: bridge
    action: create-pr
    depends: "implement.Succeeded"
    inputs:
      branch: "{{implement.branch}}"
      title: "Implement {{trigger.issue.title}} (#{{trigger.issue.number}})"
      body: |
        ## Implementation Summary
        {{implement.summary}}
        
        ## Files Changed
        {{implement.files_changed}}
        
        Closes #{{trigger.issue.number}}
        
        ---
        
        This PR was created automatically by Alcove. Please review and test thoroughly.
    outputs: [pr_number, pr_url]
  
  # Step 4: Wait for CI to pass
  - id: await-ci
    type: bridge
    action: await-ci
    depends: "create-pr.Succeeded"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
    timeout: 1800  # 30 minutes
    outputs: [ci_status]
  
  # Step 5: Fix CI if it fails (up to 2 attempts)
  - id: fix-ci
    type: agent
    agent: Feature Developer
    depends: "await-ci.Failed"
    max_iterations: 2
    inputs:
      pr_number: "{{create-pr.pr_number}}"
      ci_failures: "{{await-ci.failures}}"
      task: "Fix CI failures and ensure tests pass"
    outputs: [fixes_applied]
  
  # Step 6: Code review  
  - id: code-review
    type: agent
    agent: PR Reviewer
    depends: "await-ci.Succeeded || fix-ci.Succeeded"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
    outputs: [approved, feedback]
  
  # Step 7: Request human review
  - id: request-review
    type: bridge
    action: request-review
    depends: "code-review.Succeeded"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
      reviewers: ["tech-lead", "senior-developer"]
      message: |
        Automated implementation complete. Code review passed with: {{code-review.feedback}}
        
        Please review for business logic and final approval.
    outputs: [review_requested]
  
  # Step 8: Auto-merge when approved (optional)
  - id: auto-merge
    type: bridge
    action: merge-pr
    depends: "request-review.Succeeded && pr.approved_by_human"
    inputs:
      pr_number: "{{create-pr.pr_number}}"
      merge_method: "squash"
      commit_message: "{{trigger.issue.title}} (#{{trigger.issue.number}})\n\n{{implement.summary}}"
```

### 5. Set Up the Pipeline

```bash
# 1. Commit all configuration
git add .alcove/
git commit -m "Add complete Alcove automation pipeline"
git push

# 2. Register repository and sync
alcove agents repos add https://github.com/<your-org>/<your-repo>
alcove agents sync

# 3. Verify everything is configured
alcove agents list
alcove workflows list
alcove agents validate

# 4. Test with a manual run
alcove workflows run "Automated Feature Implementation" \
  --input issue_number=42
```

### 6. Test the Pipeline

Create a GitHub issue with the label `ready-for-implementation`:

1. **Create issue**: Describe a small feature or bug fix
2. **Add label**: Apply the `ready-for-implementation` label
3. **Monitor**: Watch the dashboard for workflow execution
4. **Review**: Check the generated PR and provide final approval

**Expected flow:**
- Issue labeled → Workflow triggers automatically
- Feature Developer agent claims issue and implements feature
- PR created with implementation and tests
- CI runs → passes or gets fixed automatically
- Code review agent provides feedback
- Human reviewers get notification for final approval
- PR merges automatically when approved

## Troubleshooting

### Sync Issues

**Agent not appearing after sync:**
```bash
# Check sync status
alcove agents repos list
alcove agents repos sync --verbose

# Validate YAML locally
alcove agents validate .alcove/agents/

# Check file permissions and git status
git status
git log --oneline -5
```

**Common YAML errors:**
- Incorrect indentation (use spaces, not tabs)
- Missing required fields (`name`, `prompt`, `repos`)
- Invalid cron syntax in schedules
- Typos in field names (`repo` instead of `repos`)

### Permission and Scope Issues

**Agent can't access external APIs:**
```bash
# Check policy rules
alcove agents describe "Your Agent" --show-policies

# View denied requests
alcove logs ses_abc123 --proxy --show-denied

# Test credentials
alcove credentials test github
alcove credentials test anthropic
```

**Common scope violations:**
- Agent tries to write to GitHub without write permissions
- LLM requests blocked by cost controls
- Network access denied (check `dev_container.network_access`)

### Session Failures

**Sessions failing immediately:**
```bash
# Check session details
alcove status ses_abc123 --verbose
alcove logs ses_abc123 --errors

# Common issues
# - Invalid credentials references
# - Repository access denied  
# - Budget/timeout too restrictive
# - Dev container image not accessible
```

**Sessions timing out:**
```bash
# Increase timeout in agent definition
timeout: 3600  # 1 hour instead of 600 (10 min)

# Or optimize prompt for faster execution
prompt: |
  You have 10 minutes to complete this task. Focus on the essentials:
  1. Identify the core issue
  2. Make minimal changes to fix it
  3. Test quickly and report results
```

### Cost and Budget Issues

**Sessions stopped due to budget:**
```bash
# Check current spending
alcove cost-report --since 7d

# Increase budget or optimize usage
budget_usd: 15.0  # instead of 5.0

# Use cheaper model
model: claude-sonnet-4-20250514  # instead of opus
```

**Unexpected high costs:**
```bash
# Identify expensive sessions
alcove list --since 7d --show-cost --sort-by cost

# Review session transcripts for excessive LLM calls
alcove logs ses_expensive123 --show-tokens
```

### Workflow Issues

**Workflow stuck in pending:**
```bash
# Check workflow run status
alcove workflows run-status <run-id>

# Look for dependency issues
alcove workflows run-details <run-id> --show-dependencies

# Common causes:
# - Depends expression never becomes true
# - Required outputs not generated by previous step
# - Agent or bridge action failing silently
```

**Bridge actions failing:**
```bash
# Check bridge action logs
alcove logs <session-id> --bridge-actions

# Common bridge action issues:
# - Invalid GitHub/GitLab API credentials
# - Insufficient permissions for PR creation
# - Branch protection rules blocking auto-merge
```

### Development Container Issues

**Dev container not starting:**
```bash
# Check image availability
podman pull node:18-slim  # or your custom image

# Verify network access setting
dev_container:
  network_access: external  # if image needs internet for setup
```

**Commands failing in dev container:**
```bash
# Test container locally
podman run --rm -it node:18-slim /bin/bash

# Check /workspace mount and permissions
alcove logs ses_abc123 --dev-container
```

### Getting Help

**When to escalate:**
- Persistent authentication failures despite valid credentials
- Workflows triggering but never executing  
- Bridge instance returning 500 errors
- Data corruption or lost session transcripts

**Self-service debugging:**
- Enable verbose logging: `alcove --debug logs ses_abc123`
- Export session for analysis: `alcove export ses_abc123`
- Test individual components: `alcove agents run` → `alcove workflows run`
- Check system status: `alcove config validate --verbose`

## Next Steps

You've now learned the fundamentals of using Alcove to automate your development workflow. Here's how to expand your usage:

### Advanced Guides

- **[Workflow Authoring Guide](workflow-authoring.md)** — Complex workflow patterns, advanced dependency expressions, error handling
- **[CLI Reference](cli-reference.md)** — Complete command documentation with examples
- **[Configuration Reference](configuration.md)** — Full YAML schema and all configuration options

### Integration Patterns

- **Multi-team workflows** — Cross-team collaboration, shared agents, permission models
- **CI/CD integration** — Triggering Alcove workflows from GitHub Actions, GitLab CI
- **Monitoring and alerting** — Setting up dashboards, cost alerts, failure notifications

### Security and Compliance

- **Advanced policy rules** — Complex scope restrictions, audit logging, compliance frameworks  
- **Credential rotation** — Automated token refresh, credential lifecycle management
- **Network isolation** — VPN integration, air-gapped environments

### Starter Agents and Templates

Explore the catalog for pre-built agents:

```bash
alcove catalog browse --category development
alcove catalog enable code-reviewer
alcove catalog enable security-scanner
alcove catalog enable documentation-generator
```

### Community and Support

- **GitHub Discussions** — Ask questions and share patterns with other adopters
- **Documentation updates** — Contribute improvements to this guide
- **Feature requests** — Suggest new capabilities for future Alcove releases

### Measuring Success

Track your automation ROI:

```bash
# Development velocity metrics  
alcove metrics --team "My Team" --metric="issues-implemented" --since 30d
alcove metrics --team "My Team" --metric="pr-cycle-time" --since 30d

# Cost and efficiency
alcove cost-report --team "My Team" --since 30d
alcove sessions-summary --team "My Team" --since 30d
```

### Scaling Up

As your team grows comfortable with Alcove:

1. **Expand agent coverage** — Add agents for testing, documentation, security scanning
2. **Automate more workflows** — Release management, dependency updates, incident response  
3. **Cross-repository orchestration** — Multi-repo feature development, organization-wide updates
4. **Custom tooling** — Build domain-specific agents for your technology stack

---

**Welcome to automated development with Alcove!** Start with simple agents and gradually build more sophisticated workflows. The key is to begin with tasks that are well-defined and gradually expand as you see the value.

Remember: Alcove agents work best on concrete, automatable tasks. Focus on repetitive development work that follows predictable patterns, and let your human developers handle the creative, strategic, and ambiguous challenges.