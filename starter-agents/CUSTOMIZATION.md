# Customizing Starter Agents

This guide explains how to fork and adapt the starter agents for your specific project needs.

## Quick Customization Workflow

1. **Fork this repository** to your organization
2. **Customize agent prompts** for your project conventions
3. **Add project-specific logic** where needed
4. **Update your Alcove skill repo** to point to your fork
5. **Maintain your fork** with updates from upstream

## Common Customizations

### Code Review Standards

The default `code-reviewer.yml` focuses on general code quality. You might want to add:

```yaml
prompt: |
  # Add after the existing prompt
  
  ## Project-Specific Checks
  
  - **API Design**: Ensure new endpoints follow REST conventions
  - **Error Handling**: Check for proper error wrapping with context
  - **Security**: Look for SQL injection, XSS, and authentication bypasses
  - **Performance**: Flag N+1 queries, missing database indexes
  
  ## Coding Standards
  
  - Functions must have docstrings
  - No hardcoded secrets or URLs
  - Use project logging framework, not print/console.log
```

### Language-Specific Test Runners

For specialized test setups, customize `test-runner.yml`:

```yaml
prompt: |
  # Add before the test discovery section
  
  ## Project Test Setup
  
  1. **Database Setup** (if needed):
     ```bash
     docker-compose up -d postgres
     npm run db:migrate
     ```
  
  2. **Environment Variables**:
     ```bash
     export NODE_ENV=test
     export DATABASE_URL=postgresql://test:test@localhost:5432/test_db
     ```
```

### Issue Classification

Enhance `backlog-triage.yml` with project-specific labels:

```yaml
prompt: |
  # Add to the label suggestions section
  
  ## Project Labels
  
  - **Components**: frontend, backend, database, auth, api
  - **Teams**: team-platform, team-product, team-infra
  - **Customer Impact**: customer-facing, internal-tools
  - **Effort**: effort/small, effort/medium, effort/large
```

## Advanced Customizations

### Multi-Repository Projects

For monorepos or multi-service projects:

```yaml
# In agent definitions, add logic to detect subprojects
prompt: |
  ## Detect Affected Services
  
  ```bash
  # Check which services are affected by the changes
  affected_services=$(git diff --name-only HEAD~1 HEAD | cut -d'/' -f1 | sort -u)
  
  for service in $affected_services; do
    if [ -f "$service/package.json" ]; then
      echo "Running tests for Node.js service: $service"
      cd "$service" && npm test && cd ..
    elif [ -f "$service/go.mod" ]; then
      echo "Running tests for Go service: $service"
      cd "$service" && go test ./... && cd ..
    fi
  done
  ```
```

### Custom Security Checks

Add security-specific logic to code reviewers:

```yaml
prompt: |
  ## Security Review Checklist
  
  1. **Secrets Detection**:
     ```bash
     # Check for potential secrets
     grep -r "password\|secret\|key\|token" . --exclude-dir=.git | grep -v "REDACTED"
     ```
  
  2. **Dependency Vulnerabilities**:
     ```bash
     # Run security audits
     npm audit || true
     go mod tidy && nancy sleuth || true
     ```
```

### Integration with External Tools

Connect to your project's specific tools:

```yaml
# For Jira integration
prompt: |
  ## Update Jira Ticket
  
  If this PR closes an issue, update the corresponding Jira ticket:
  ```bash
  # Extract Jira ticket from PR title or branch name
  jira_ticket=$(echo "{{trigger.pr_title}}" | grep -oE "[A-Z]+-[0-9]+")
  if [ -n "$jira_ticket" ]; then
    curl -X POST "https://your-company.atlassian.net/rest/api/3/issue/$jira_ticket/transitions" \
      -H "Authorization: Bearer $JIRA_TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"transition":{"id":"31"}}'  # Transition to "In Review"
  fi
  ```
```

## Maintenance Best Practices

### Keeping Up with Upstream

1. **Add upstream remote**:
   ```bash
   git remote add upstream https://github.com/alcove-ai/alcove-starter-agents.git
   ```

2. **Periodic updates**:
   ```bash
   git fetch upstream
   git checkout main
   git merge upstream/main
   # Resolve any conflicts with your customizations
   git push origin main
   ```

3. **Track your changes**:
   - Keep customizations in clearly marked sections
   - Document why each change was made
   - Consider creating project-specific agents alongside the defaults

### Version Control Strategy

**Option 1: Separate Files**
```
.alcove/agents/
├── code-reviewer.yml           # Upstream version
├── code-reviewer-myproject.yml # Your customized version
├── test-runner.yml             # Upstream version
└── test-runner-myproject.yml   # Your customized version
```

**Option 2: Branching**
- `main` branch: sync with upstream
- `customized` branch: your modifications
- Regularly rebase `customized` onto updated `main`

### Testing Your Changes

1. **Validate YAML**:
   ```bash
   yamllint .alcove/agents/your-agent.yml
   ```

2. **Test in development**:
   - Add your fork as a skill repo in a dev Alcove instance
   - Run test workflows with your customized agents
   - Check agent outputs match expected format

3. **Gradual rollout**:
   - Start with one team or project
   - Monitor agent performance and feedback
   - Iterate based on real usage patterns

## Examples by Language/Framework

### Python/Django Projects

```yaml
# Custom test-runner for Django
prompt: |
  ## Django Test Setup
  
  ```bash
  # Set up test database
  python manage.py test --settings=myproject.settings.test --keepdb
  
  # Run additional checks
  python manage.py check --deploy
  python manage.py makemigrations --check --dry-run
  
  # Security checks
  bandit -r . -f json
  safety check --json
  ```
```

### React/TypeScript Frontend

```yaml
# Custom code-reviewer for React
prompt: |
  ## Frontend Code Review
  
  1. **React Best Practices**:
     - Check for proper key props in lists
     - Verify component memo usage for performance
     - Look for direct DOM manipulation (should use refs)
  
  2. **TypeScript**:
     - Ensure no `any` types without justification
     - Check for proper prop type definitions
     - Verify generic constraints are meaningful
  
  3. **Accessibility**:
     - Check for aria-labels on interactive elements
     - Verify semantic HTML usage
     - Test keyboard navigation patterns
```

### Microservices Architecture

```yaml
# Service-aware documentation updater
prompt: |
  ## Service Documentation
  
  ```bash
  # Determine which service was modified
  changed_service=$(git diff --name-only HEAD~1 HEAD | cut -d'/' -f1 | head -1)
  
  case "$changed_service" in
    "auth-service")
      # Update API documentation
      cd auth-service && npm run generate-docs
      ;;
    "payment-service")
      # Update payment flow diagrams
      cd docs && make update-payment-diagrams
      ;;
  esac
  ```
```

## Getting Help

- 📖 [Main Alcove Documentation](https://github.com/alcove-ai/alcove/tree/main/docs)
- 💬 [GitHub Discussions](https://github.com/alcove-ai/alcove/discussions)
- 🐛 [Report Issues](https://github.com/alcove-ai/alcove/issues)
- 📧 Consider sharing useful customizations back to the community!

---

Remember: The goal is to adapt these agents to your workflow, not force your workflow to match the agents.