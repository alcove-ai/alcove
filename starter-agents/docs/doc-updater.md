# Documentation Updater Agent

## Purpose

Analyzes code changes and updates project documentation when changes affect user-facing APIs, behavior, or configuration. Creates pull/merge requests with documentation updates.

## Configuration

| Field | Value | Notes |
|-------|-------|-------|
| **Timeout** | 900 seconds (15 minutes) | Time for analysis and doc updates |
| **Enforcement Mode** | `monitor` | Safe default for new teams |
| **Repository** | `{{trigger.repo_url}}` or `{{trigger.project_url}}` | Clones repo for doc updates |

## Required Scopes

### GitHub (`doc-updater.yml`)
- `github:read` - Read PR details and repository contents
- `github:write` - Create branches and pull requests

### GitLab (`doc-updater-gitlab.yml`)
- `gitlab:read` - Read MR details and repository contents  
- `gitlab:write` - Create branches and merge requests

## Expected Outputs

| Output | Type | Description |
|--------|------|-------------|
| `summary` | string | Description of documentation updates made, or "No documentation updates needed" |

## Documentation Impact Detection

The agent identifies changes that require documentation updates:

### User-Facing Changes
- **API endpoints**: New routes, changed parameters, response formats
- **CLI commands**: New options, changed syntax, deprecated features
- **Configuration**: New settings, changed defaults, environment variables
- **Installation**: Changed dependencies, setup procedures
- **Breaking changes**: Backward incompatible modifications

### Documentation Locations
- `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`
- `docs/` directory and subdirectories
- API documentation files
- Code comments in public APIs
- Configuration examples

## Example Workflow

```yaml
workflow:
  - id: update-docs
    type: agent
    agent: alcove-starter-agents/doc-updater  # or doc-updater-gitlab
    depends: "merge.Succeeded"  # Run after PR/MR is merged
    inputs:
      pr_number: "{{trigger.pr_number}}"      # or mr_iid for GitLab
      repo: "{{trigger.repo}}"                # or gitlab_project
      repo_url: "{{trigger.repo_url}}"        # or project_url
    outputs: [summary]
```

## Update Process

When documentation updates are needed:

1. **Create branch**: `docs/update-{pr_number}`
2. **Update files**: Modify relevant documentation
3. **Commit changes**: Clear commit message referencing original PR/MR
4. **Create PR/MR**: Link back to the original change
5. **Report summary**: Describe what was updated

## SCM Differences

| Aspect | GitHub | GitLab |
|--------|--------|--------|
| **Repository ID** | `{{trigger.repo}}` | `{{trigger.gitlab_project}}` |
| **PR/MR Number** | `{{trigger.pr_number}}` | `{{trigger.mr_iid}}` |
| **PR/MR Reference** | `#123` | `!123` |
| **Create PR** | `gh pr create` | `glab mr create` |
| **Branch push** | `git push origin branch` | `git push origin branch` |

## Customization

### Project-Specific Documentation

```yaml
prompt: |
  ## Project Documentation Standards
  
  1. **API Changes**: Update OpenAPI spec in `api/openapi.yaml`
  2. **CLI Changes**: Update help text in `cmd/` files and `docs/cli/`
  3. **Configuration**: Update example configs in `examples/`
  4. **Architecture**: Update diagrams in `docs/architecture/`
```

### Automated Documentation Generation

```yaml
prompt: |
  ## Generate Documentation
  
  ```bash
  # Regenerate API docs if needed
  if git diff HEAD~1 HEAD --name-only | grep -E "(api/|handlers/)" > /dev/null; then
    echo "API changes detected, regenerating docs..."
    make generate-api-docs
  fi
  
  # Update CLI help if commands changed
  if git diff HEAD~1 HEAD --name-only | grep "cmd/" > /dev/null; then
    echo "CLI changes detected, updating help docs..."
    ./myapp help > docs/cli-reference.md
  fi
  ```
```

### Multi-Language Documentation

```yaml
prompt: |
  ## Internationalization
  
  ```bash
  # Update all language versions if API docs change
  if [ -f "docs/api.md" ]; then
    echo "Updating API documentation in all languages..."
    for lang in en es fr de; do
      if [ -f "docs/$lang/api.md" ]; then
        echo "TODO: $lang version needs manual review" >> docs/$lang/REVIEW_NEEDED.md
      fi
    done
  fi
  ```
```

## Smart Update Logic

The agent avoids unnecessary updates by:

- **Change impact analysis**: Only updates docs when behavior actually changes
- **Existing documentation check**: Doesn't update if docs are already current
- **Scope filtering**: Ignores internal refactoring and bug fixes
- **Format preservation**: Maintains existing documentation style

## Common Update Patterns

### API Documentation
```markdown
## Before
GET /users/{id}

## After  
GET /users/{id}
Query parameters:
- include_inactive (boolean, optional): Include inactive users
```

### CLI Documentation
```markdown
## Before
myapp deploy [environment]

## After
myapp deploy [environment]
Options:
  --dry-run    Show what would be deployed without making changes
  --timeout    Maximum time to wait for deployment (default: 5m)
```

### Configuration Documentation
```markdown
## Before
database_url: postgresql://...

## After
database_url: postgresql://...
# New optional setting
connection_pool_size: 10  # Maximum database connections (default: 5)
```

## Guard Rails

The agent includes safeguards to prevent issues:

- **Read-only for internal changes**: No docs updates for refactoring
- **Branch protection**: Creates branches, never modifies main directly
- **Change validation**: Verifies documentation changes make sense
- **Minimal updates**: Keeps changes focused and relevant

## Troubleshooting

### Agent creating unnecessary PRs
- Review what triggers documentation updates
- Adjust the change impact detection logic
- Add exclusions for internal-only changes

### Documentation out of sync
- Run the agent manually on recent PRs
- Check if documentation structure changed
- Verify agent can find all documentation files

### Format inconsistencies
- Add style guide rules to the agent prompt
- Use automated formatting tools where possible
- Include format checking in the validation

### Missing updates
- Check that all documentation locations are covered
- Verify the agent detects all types of user-facing changes
- Consider running after CI instead of immediately after merge

## Related Agents

This agent complements:
- **Code Reviewer**: Reviews can suggest documentation needs
- **Test Runner**: Test changes may indicate behavior changes
- **Release Agent**: Documentation updates can trigger release notes