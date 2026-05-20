# Test Runner Agent

## Purpose

Auto-detects project type and runs the appropriate test suite. Supports multiple languages/frameworks and includes conditional dev container usage for complex build environments.

## Configuration

| Field | Value | Notes |
|-------|-------|-------|
| **Timeout** | 1200 seconds (20 minutes) | Allows for comprehensive test suites |
| **Enforcement Mode** | `monitor` | Safe default for new teams |
| **Dev Container** | Conditional | Uses dev container if `$DEV_CONTAINER_HOST` is available |
| **Repository** | `{{trigger.repo_url}}` or `{{trigger.project_url}}` | Clones the repo for testing |

## Required Scopes

### GitHub (`test-runner.yml`)
- `github:read` - Clone repository and read PR details

### GitLab (`test-runner-gitlab.yml`)
- `gitlab:read` - Clone repository and read MR details

## Expected Outputs

| Output | Type | Description |
|--------|------|-------------|
| `passed` | boolean | Whether all tests passed ("true" or "false") |
| `summary` | string | Detailed test results including counts and failures |

## Supported Project Types

The agent auto-detects and runs tests for:

| Language/Framework | Detection | Test Command |
|-------------------|-----------|--------------|
| **Go** | `go.mod` present | `go test ./...` |
| **Node.js** | `package.json` present | `npm test` or `yarn test` |
| **Python** | `requirements.txt`, `pyproject.toml`, or `setup.py` | `python -m pytest` or `python -m unittest` |
| **Rust** | `Cargo.toml` present | `cargo test` |
| **Java (Maven)** | `pom.xml` present | `mvn test` |
| **Java (Gradle)** | `build.gradle` present | `./gradlew test` |

## Dev Container Integration

When `$DEV_CONTAINER_HOST` environment variable is set, the agent:

1. **Health Check**: Verifies dev container is responsive
2. **Conditional Execution**: 
   - If healthy: Runs tests via `POST /exec` endpoint
   - If unhealthy: Falls back to direct execution
3. **Error Handling**: Gracefully handles dev container failures

```bash
# Health check example
curl -s http://$DEV_CONTAINER_HOST/healthz

# Test execution example
curl -s -X POST http://$DEV_CONTAINER_HOST/exec \
  -H "Authorization: Bearer $DEV_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"cmd":"cd /workspace && go test ./...","timeout":600}'
```

## Example Workflow

```yaml
workflow:
  - id: run-tests
    type: agent
    agent: alcove-starter-agents/test-runner  # or test-runner-gitlab
    inputs:
      repo: "{{trigger.repo}}"                # or gitlab_project
      repo_url: "{{trigger.repo_url}}"        # or project_url
      pr_number: "{{trigger.pr_number}}"      # or mr_iid (optional)
    outputs: [passed, summary]
    output_contract:
      required: [passed, summary]
      allowed_values:
        passed: ["true", "false"]
      routing_field: passed
      success_value: "true"
```

## SCM Differences

| Aspect | GitHub | GitLab |
|--------|--------|--------|
| **Repository ID** | `{{trigger.repo}}` | `{{trigger.gitlab_project}}` |
| **Repository URL** | `{{trigger.repo_url}}` | `{{trigger.project_url}}` |
| **PR/MR Reference** | `{{trigger.pr_number}}` | `{{trigger.mr_iid}}` |

## Customization

Common customizations include:

### Project-Specific Setup

```yaml
prompt: |
  ## Custom Setup Steps
  
  ```bash
  # Install dependencies
  npm install
  
  # Setup test database
  docker-compose up -d postgres
  npm run db:migrate:test
  
  # Set environment variables
  export NODE_ENV=test
  export DATABASE_URL=postgresql://test:test@localhost:5432/test_db
  ```
```

### Additional Test Types

```yaml
prompt: |
  ## Extended Test Suite
  
  ```bash
  # Unit tests
  npm test
  
  # Integration tests
  npm run test:integration
  
  # End-to-end tests (if fast enough)
  npm run test:e2e:smoke
  ```
```

### Performance Testing

```yaml
prompt: |
  ## Performance Regression Check
  
  ```bash
  # Run performance tests if they exist
  if [ -f "package.json" ] && grep -q "test:perf" package.json; then
    echo "Running performance tests..."
    timeout 300 npm run test:perf || echo "Performance tests timed out"
  fi
  ```
```

## Multi-Language Projects

For projects with multiple languages, the agent runs tests for all detected types:

```bash
# Example output for a multi-language project
echo "Detected Go and Node.js projects"
echo "Running Go tests..."
go test ./...
echo "Running Node.js tests..."
cd frontend && npm test
```

## Error Handling

The agent handles common test failures gracefully:

- **Build failures**: Reports compilation errors clearly
- **Missing dependencies**: Suggests installation commands
- **Test timeouts**: Reports which tests were running
- **Environment issues**: Provides debugging information

## Troubleshooting

### Tests not running
- Check that the project type detection is working
- Verify repository URL is accessible
- Ensure required dependencies are specified correctly

### Dev container issues
- Check `$DEV_CONTAINER_HOST` is set correctly
- Verify dev container is healthy with manual `curl` call
- Review dev container logs for startup issues

### Performance problems
- Reduce test timeout for faster feedback
- Skip slow integration tests in PR workflows
- Use parallel test execution where possible

### False positives
- Ignore flaky tests temporarily
- Add retry logic for network-dependent tests
- Separate unit tests from integration tests

## Related Agents

This agent works well with:
- **Code Reviewer**: Run tests before code review
- **Documentation Updater**: Update docs if tests reveal API changes
- **Security Reviewer**: Combine with security-focused testing