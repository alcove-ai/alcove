# Alcove Starter Agents

A collection of project-agnostic AI agent definitions and workflows to help teams get started with [Alcove](https://github.com/alcove-ai/alcove).

## Quick Start (1 minute)

1. **Add as a skill repo** in your Alcove dashboard:
   - Go to Settings → Skill Repositories
   - Add: `https://github.com/alcove-ai/alcove-starter-agents`
   - Click "Sync Now"

2. **Enable agents** from the catalog:
   - Go to Catalog → Starter Kits
   - Toggle on the agents you want to use
   - Choose GitHub or GitLab variants as appropriate

3. **Reference in workflows**:
   ```yaml
   workflow:
     - id: review
       type: agent
       agent: alcove-starter-agents/code-reviewer
   ```

## What's Included

### 🔍 **Agents** (8 total: 4 GitHub + 4 GitLab)

| Agent | Purpose | Outputs |
|-------|---------|---------|
| **Code Reviewer** | Reviews PRs/MRs for bugs, style, and correctness | `approved`, `comments` |
| **Test Runner** | Auto-detects and runs project test suites | `passed`, `summary` |
| **Documentation Updater** | Updates docs when code changes affect APIs | `summary` |
| **Backlog Triage** | Detects duplicate issues, suggests labels | `duplicate_of`, `suggested_labels` |

### 🔄 **Workflows** (4 total)

- **PR Review Pipeline**: Automated code review + testing for GitHub
- **MR Review Pipeline**: Automated code review + testing for GitLab  
- **Issue Triage Pipeline**: Duplicate detection + labeling for GitHub
- **GitLab Issue Triage Pipeline**: Duplicate detection + labeling for GitLab

## Repository Structure

```
.alcove/
├── agents/           # Agent definitions
│   ├── code-reviewer.yml
│   ├── code-reviewer-gitlab.yml
│   ├── test-runner.yml
│   ├── test-runner-gitlab.yml
│   ├── doc-updater.yml
│   ├── doc-updater-gitlab.yml
│   ├── backlog-triage.yml
│   └── backlog-triage-gitlab.yml
└── workflows/        # Workflow templates
    ├── pr-review-pipeline.yml
    ├── mr-review-pipeline.yml
    ├── issue-triage-pipeline.yml
    └── gitlab-issue-triage-pipeline.yml
docs/                 # Per-agent documentation
├── code-reviewer.md
├── test-runner.md
├── doc-updater.md
└── backlog-triage.md
CUSTOMIZATION.md      # Guide to forking and adapting
```

## Key Features

- **Project-agnostic**: No hardcoded repo URLs or project-specific logic
- **Template variables**: Use `{{trigger.*}}` and `{{steps.*}}` for any repo
- **Dual platform**: GitHub and GitLab variants for all agents
- **Dev container support**: Test runner includes conditional container logic
- **Security-first**: Monitor mode by default, minimal required scopes

## GitHub vs GitLab

| GitHub | GitLab | Notes |
|--------|--------|-------|
| `{{trigger.repo}}` | `{{trigger.gitlab_project}}` | Repository/project identifier |
| `{{trigger.pr_number}}` | `{{trigger.mr_iid}}` | PR number vs MR IID |
| `{{trigger.issue_number}}` | `{{trigger.issue_iid}}` | Issue number vs IID |
| `{{trigger.issue_body}}` | `{{trigger.issue_description}}` | Issue content field |
| `gh` CLI | `glab` CLI | Command-line tools |

## Required Scopes

| Agent | GitHub Scopes | GitLab Scopes |
|-------|---------------|---------------|
| Code Reviewer | `github:read`, `github:review` | `gitlab:read`, `gitlab:review` |
| Test Runner | `github:read` | `gitlab:read` |
| Doc Updater | `github:read`, `github:write` | `gitlab:read`, `gitlab:write` |
| Backlog Triage | `github:read` | `gitlab:read` |

## Customization

Want to adapt these for your project? See [`CUSTOMIZATION.md`](CUSTOMIZATION.md) for:
- How to fork and modify agent prompts
- Adding project-specific logic
- Integrating with your existing workflows
- Best practices for maintenance

## License

Apache-2.0 - same as the main [Alcove project](https://github.com/alcove-ai/alcove).

## Support

- 📖 [Main Alcove Documentation](https://github.com/alcove-ai/alcove/tree/main/docs)
- 🐛 [Report Issues](https://github.com/alcove-ai/alcove/issues)
- 💬 [Discussions](https://github.com/alcove-ai/alcove/discussions)