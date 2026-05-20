# Backlog Triage Agent

## Purpose

Automatically triages new issues by detecting potential duplicates and suggesting appropriate labels. Helps maintain organized issue backlogs without manual effort.

## Configuration

| Field | Value | Notes |
|-------|-------|-------|
| **Timeout** | 300 seconds (5 minutes) | Fast triage for quick feedback |
| **Enforcement Mode** | `monitor` | Safe default for new teams |
| **Repository** | `{{trigger.repo_url}}` or `{{trigger.project_url}}` | For fetching existing issues |

## Required Scopes

### GitHub (`backlog-triage.yml`)
- `github:read` - Read issue details and existing backlog

### GitLab (`backlog-triage-gitlab.yml`) 
- `gitlab:read` - Read issue details and existing backlog

## Expected Outputs

| Output | Type | Description |
|--------|------|-------------|
| `duplicate_of` | string | Issue number/IID if duplicate found, empty string otherwise |
| `suggested_labels` | string | Comma-separated list of appropriate labels |

## Duplicate Detection Logic

The agent compares new issues against the open backlog using:

### Similarity Metrics
- **Title matching**: Fuzzy comparison of issue titles
- **Content overlap**: Shared error messages, file paths, component names
- **Affected areas**: Same feature, command, or functionality
- **Stack traces**: Similar error patterns

### Detection Process
1. **Fetch backlog**: Gets recent open issues (last 200)
2. **Compare content**: Analyzes title and description similarity
3. **Score matches**: Ranks potential duplicates by confidence
4. **Take action**: Labels and comments if high-confidence match found

## Label Suggestion Categories

The agent suggests labels from common categories:

| Category | Example Labels |
|----------|----------------|
| **Type** | `bug`, `enhancement`, `feature`, `documentation`, `question` |
| **Priority** | `low`, `medium`, `high`, `critical` |  
| **Area** | `frontend`, `backend`, `api`, `cli`, `docs`, `tests`, `ci` |
| **Status** | `needs-investigation`, `needs-reproduction`, `good-first-issue` |

## Example Workflow

```yaml
workflow:
  - id: triage-new-issue
    type: agent
    agent: alcove-starter-agents/backlog-triage  # or backlog-triage-gitlab
    inputs:
      issue_number: "{{trigger.issue_number}}"    # or issue_iid for GitLab
      issue_title: "{{trigger.issue_title}}"
      issue_body: "{{trigger.issue_body}}"        # or issue_description for GitLab
      repo: "{{trigger.repo}}"                    # or gitlab_project
      repo_url: "{{trigger.repo_url}}"            # or project_url
    outputs: [duplicate_of, suggested_labels]
```

## Actions Taken

### On Duplicate Detection
1. **Add label**: `possible-duplicate`
2. **Post comment**: Links to potential duplicate with title
3. **Human review**: Never auto-closes, humans make final decision

### On Label Suggestions
1. **Analyze content**: Examines issue title and description
2. **Suggest labels**: Only adds clearly appropriate labels
3. **Avoid conflicts**: Skips labels that already exist

## SCM Differences

| Aspect | GitHub | GitLab |
|--------|--------|--------|
| **Issue Reference** | `#123` | `#123` (same format) |
| **Issue Number** | `{{trigger.issue_number}}` | `{{trigger.issue_iid}}` |
| **Issue Body** | `{{trigger.issue_body}}` | `{{trigger.issue_description}}` |
| **List Command** | `gh issue list` | `glab issue list` |
| **Edit Command** | `gh issue edit` | `glab issue update` |
| **Comment Command** | `gh issue comment` | `glab issue note` |

## Bot Protection

The agent includes safeguards against automation loops:

- **Bot detection**: Skips issues created by bot accounts
- **Username patterns**: Checks for `[bot]`, `-bot` in usernames  
- **Early exit**: Returns empty outputs for bot-created issues
- **Human override**: Allows manual processing if needed

## Customization

### Project-Specific Labels

```yaml
prompt: |
  ## Custom Label Categories
  
  - **Components**: `auth`, `payments`, `notifications`, `mobile-app`
  - **Teams**: `team-platform`, `team-product`, `team-security`
  - **Customer Impact**: `customer-facing`, `internal-tooling`
  - **Effort Estimation**: `effort/small`, `effort/medium`, `effort/large`
```

### Enhanced Duplicate Detection

```yaml
prompt: |
  ## Advanced Duplicate Logic
  
  ```bash
  # Check for similar stack traces
  if echo "$issue_body" | grep -E "(at [A-Za-z.]+:[0-9]+|File.*line [0-9]+)" > /dev/null; then
    echo "Stack trace detected, checking for similar patterns..."
    # Enhanced comparison logic here
  fi
  
  # Check version-specific patterns  
  version=$(echo "$issue_title $issue_body" | grep -oE "v?[0-9]+\.[0-9]+\.[0-9]+")
  if [ -n "$version" ]; then
    echo "Version $version mentioned, checking version-specific issues..."
  fi
  ```
```

### Integration with External Tools

```yaml
prompt: |
  ## External Integrations
  
  ```bash
  # Check against known issues in external tracker
  if grep -E "JIRA-[0-9]+" "$issue_body" > /dev/null; then
    jira_id=$(echo "$issue_body" | grep -oE "JIRA-[0-9]+")
    echo "Referenced JIRA ticket: $jira_id"
    # Could add logic to check JIRA status
  fi
  
  # Check against support tickets
  if grep -E "support ticket #[0-9]+" "$issue_body" > /dev/null; then
    ticket_id=$(echo "$issue_body" | grep -oE "#[0-9]+")
    echo "Related to support ticket: $ticket_id"
  fi
  ```
```

## Guard Rails

The agent includes important safety measures:

- **Read-only**: Never modifies existing issues, only the new one
- **No auto-close**: Humans decide if duplicates should be closed
- **Conservative labeling**: Only suggests obviously appropriate labels
- **Comment clarity**: Explains reasoning for duplicate detection

## False Positive Handling

When the agent incorrectly identifies duplicates:

1. **Human review**: Always include human verification step
2. **Feedback loop**: Monitor accuracy and adjust detection logic
3. **Override capability**: Humans can remove labels and close notifications
4. **Learning**: Use patterns to improve future detection

## Performance Considerations

- **Backlog limit**: Only checks recent issues (default: 200) for performance
- **Quick comparison**: Uses efficient text matching algorithms
- **Early exit**: Stops processing when clear duplicate found
- **Timeout protection**: Completes within 5-minute limit

## Troubleshooting

### Too many false positives
- Increase similarity threshold for duplicate detection
- Add exclusion patterns for known different issue types
- Reduce the scope of issues checked for duplicates

### Missing actual duplicates  
- Lower similarity threshold (with caution)
- Improve text preprocessing (remove noise, normalize terms)
- Expand the number of issues checked

### Incorrect label suggestions
- Review and refine label categorization logic
- Add project-specific label rules
- Monitor which labels are commonly removed by humans

### Bot detection not working
- Update bot username patterns for your organization
- Add additional bot account patterns
- Check if service accounts need special handling

## Related Agents

This agent works well with:
- **Auto-labeler**: More sophisticated labeling based on content analysis
- **Issue Router**: Assign issues to appropriate teams based on labels
- **Milestone Planner**: Help plan releases based on triaged issues