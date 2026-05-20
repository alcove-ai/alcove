# Code Reviewer Agent

## Purpose

Reviews pull requests and merge requests for code quality, bugs, and adherence to best practices. Posts formal reviews via the SCM API.

## Configuration

| Field | Value | Notes |
|-------|-------|-------|
| **Timeout** | 600 seconds (10 minutes) | Sufficient for reviewing most PRs |
| **Enforcement Mode** | `monitor` | Safe default for new teams |
| **Dev Container** | Not used | Reviews don't require code execution |

## Required Scopes

### GitHub (`code-reviewer.yml`)
- `github:read` - Read PR details and diffs
- `github:review` - Post review comments and approve/reject

### GitLab (`code-reviewer-gitlab.yml`)
- `gitlab:read` - Read MR details and diffs
- `gitlab:review` - Post review notes and approve

## Expected Outputs

| Output | Type | Description |
|--------|------|-------------|
| `approved` | boolean | Whether the code is approved ("true" or "false") |
| `comments` | string | Summary of review findings |

## Example Workflow

```yaml
workflow:
  - id: code-review
    type: agent
    agent: alcove-starter-agents/code-reviewer  # or code-reviewer-gitlab
    inputs:
      pr_number: "{{trigger.pr_number}}"        # or mr_iid for GitLab
      repo: "{{trigger.repo}}"                  # or gitlab_project
      repo_url: "{{trigger.repo_url}}"          # or project_url
    outputs: [approved, comments]
    output_contract:
      required: [approved, comments]
      allowed_values:
        approved: ["true", "false"]
      routing_field: approved
      success_value: "true"
```

## Review Criteria

The agent evaluates code changes for:

1. **Correctness**: Logic errors, incorrect API usage, edge case handling
2. **Style**: Formatting, naming conventions, code organization
3. **Security**: Potential vulnerabilities, unsafe patterns
4. **Performance**: Inefficient algorithms, memory leaks, blocking operations
5. **Best Practices**: Language/framework conventions, maintainability

## SCM Differences

| Aspect | GitHub | GitLab |
|--------|--------|--------|
| **CLI Tool** | `gh` | `glab` |
| **Repository ID** | `{{trigger.repo}}` | `{{trigger.gitlab_project}}` |
| **PR/MR Number** | `{{trigger.pr_number}}` | `{{trigger.mr_iid}}` |
| **Approval Command** | `gh pr review --approve` | `glab mr approve` |
| **Comment Command** | `gh pr review --comment` | `glab mr note` |
| **Changes Request** | `gh pr review --request-changes` | `glab mr note` (with text) |

## Customization

Common customizations include:

- **Project-specific standards**: Add checks for internal conventions
- **Security focus**: Emphasize security review patterns
- **Language-specific**: Add rules for specific languages or frameworks
- **Integration**: Connect to external tools like SonarQube or CodeClimate

See [`CUSTOMIZATION.md`](../CUSTOMIZATION.md) for detailed examples.

## Troubleshooting

### Agent not posting reviews
- Check that GitHub/GitLab scopes include review permissions
- Verify the repository/project ID is correct
- Ensure the PR/MR number exists and is accessible

### Reviews too strict/lenient
- Adjust the prompt to emphasize different aspects
- Add guard rails for specific situations
- Consider separate agents for different review types

### Template variable errors
- Verify trigger provides expected fields
- Check spelling: `pr_number` vs `mr_iid`, `repo` vs `gitlab_project`
- Test with a simple workflow first

## Related Workflows

This agent is commonly used with:
- **Test Runner**: Run tests before or after code review
- **Security Reviewer**: Additional security-focused review
- **Documentation Updater**: Update docs when APIs change