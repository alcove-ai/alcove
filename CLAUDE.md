# Alcove — Sandboxed AI Coding Agents on OpenShift/Kubernetes

## What This Is

Alcove runs AI coding agents (Claude Code) in ephemeral, network-isolated
containers. Each session gets a fresh container, a scoped authorization proxy, and
a complete session transcript. No persistent state crosses session boundaries.

**Language:** Go 1.25 | **License:** Apache-2.0

## Components

| Name | Role | Binary | k8s Resource |
|------|------|--------|-------------|
| **Bridge** | Controller, REST API, dashboard, scheduler, agent repo syncer | `cmd/bridge` | Deployment |
| **Skiff** | Ephemeral Claude Code worker | `cmd/skiff-init` | Job / `podman run --rm` |
| **Gate** | Auth proxy sidecar (token swap, LLM proxy, SCM proxy, scope enforcement) | `cmd/gate` | Sidecar in Skiff pod |
| **Shim** | Execution sidecar injected into dev containers (`GET /healthz`, `POST /exec` with NDJSON streaming) | `cmd/shim` | Sidecar in dev container |
| **Hail** | Message bus (NATS) | external | Deployment |
| **Ledger** | Session store (PostgreSQL) | external | Deployment + PVC |

## Architecture

```
Bridge → Hail (NATS) → Skiff Pod [skiff container + gate sidecar] → Gate → External Services
                                ↕ /workspace volume (optional)              → Ledger (PostgreSQL)
                        Dev Container [project image + shim]
```

- Skiff pods are ephemeral: one session, one container, then destroyed
- Gate is a sidecar (shares network namespace with Skiff)
- Gate proxies ALL external traffic: MITM TLS interception for service domains (GitHub, GitLab, Jira) and `/v1/` endpoint for LLM API calls (Skiff has no real credentials)
- Optional dev container runs alongside Skiff with a shared `/workspace` volume, enabling agents to build/test code in project-specific environments; the shim binary is baked into the dev container image (built with `make build-dev`); a simple shell entrypoint starts PostgreSQL, NATS, and the shim without requiring a process supervisor
- On OpenShift, a static `alcove-allow-internal` NetworkPolicy restricts egress (per-task NetworkPolicy is disabled due to OVN-Kubernetes DNS resolution issues); dual-network isolation (`--internal` flag) on podman
- k3s is supported for local Kubernetes development (`make k3s-setup && make k3s-up && make k3s-watch`); Bridge runs on the host, NATS+PostgreSQL run as k8s pods with port-forwards, Skiff Jobs are dispatched into the same k3s cluster

## Design Documents

Read these for full context:

1. `docs/design/implementation-status.md` — **START HERE** — current state, what works, what's next
2. `docs/glossary.md` — **TERMINOLOGY** — canonical definitions for workflow, session, task, etc.
3. `docs/design/architecture.md` — component design, deployment diagrams, network isolation, roadmap
4. `docs/design/architecture-decisions.md` — 24 resolved decisions, CLI design, config format, repo layout
5. `docs/design/problem-statement.md` — why ephemeral agents
6. `docs/design/credential-management.md` — credential storage, encryption, OAuth2 token flow
7. `docs/design/auth-backends.md` — auth backend design (memory, postgres, rh-identity)
8. `docs/design/gate-scm-authorization.md` — SCM MITM proxy, operation taxonomy, security model

## Skills (Slash Commands)

These are available as `/command` in Claude Code sessions. Use them instead of
doing these tasks manually.

| Command | When to Use |
|---------|-------------|
| `/dev-up` | First-time setup or full reset of local dev environment (wipes database) |
| `/dev-restart` | Rebuild and restart Bridge after code changes (preserves database) |
| `/release` | Trigger the automated release pipeline (creates changelog, tags, builds images) |
| `/deploy-staging` | Audit a release for safety and deploy to OpenShift staging via app-interface |

Skill definitions live in `.claude/skills/` — read them for full details.

## Quick Commands

```bash
# Primary dev workflow — hot-reload with full session dispatch
make watch                    # Builds images, starts NATS+PostgreSQL, runs Bridge via Air
                              # Save a .go file → Air rebuilds → Bridge restarts → sessions work
make down                     # Stop everything (Bridge + NATS + PostgreSQL)

# First-time setup or database wipe (then switch to make watch)
make up                       # Build binaries + images, start containerized Bridge + infra
                              # Requires follow-up curl commands to seed credentials (see dev-up skill)

# Build
make build                    # Build all Go binaries to bin/
make build-images             # Build container images with podman (smart rebuild via stamps)
make build-tooling            # Build heavy skiff-tooling base image (only when tools change)
make test                     # Run tests

# Infrastructure
make dev-infra                # Start only NATS + PostgreSQL on podman
make dev-up                   # Start full containerized environment
make dev-down                 # Stop everything
make dev-reset                # Stop + remove volumes (clean slate)

# k3s (Kubernetes backend) — test k8s runtime locally
# Requires sudo for k3s install and image import. Stop podman infra first: make down
make k3s-setup                # Install k3s, configure kubeconfig + firewalld (run once)
make k3s-up                   # Build images, deploy NATS+PostgreSQL to k3s, start port-forwards
make k3s-watch                # Hot-reload Bridge with k8s runtime (Air)
make k3s-down                 # Stop port-forwards, delete k3s namespace
make k3s-reset                # Full reset (namespace + imported images)
make k3s-status               # Show pods, port-forwards, and jobs

```

## Code Conventions

- Standard Go project layout (`cmd/`, `internal/`)
- `net/http` for HTTP servers (no frameworks)
- `github.com/spf13/cobra` for CLI
- `github.com/nats-io/nats.go` for NATS
- `github.com/jackc/pgx/v5` for PostgreSQL
- Containerfiles use multi-stage builds (golang:1.25 → ubi9)
- All container images use `localhost/alcove-<component>:dev` tags locally

## Key Decisions

- **Teams are the ownership unit** — every resource belongs to a team; every user belongs to one or more teams; personal team auto-created on signup; `X-Alcove-Team` header scopes all API requests
- **Gate is a sidecar** per Skiff pod (not shared service) — credential isolation
- **MITM TLS for service domains** — ephemeral CA per session, Gate terminates CONNECT tunnels to service domains (GitHub, GitLab, Jira), inspects requests, injects credentials, and re-encrypts to upstream; tools use standard HTTP_PROXY; CA cert injected into Skiff trust store
- **LLM keys never enter Skiff** — Gate proxies LLM API calls, injects keys; Gate also translates Anthropic API format to Vertex AI format when using Vertex AI (`GATE_VERTEX_PROJECT`, `GATE_VERTEX_REGION`)
- **Fresh git clone per session** — `git clone --depth=1`, no persistent volumes
- **NATS for messaging** — status updates and cancellation only (session config via env vars)
- **PostgreSQL only** for Ledger (no S3 in Phase 1)
- **Podman + k8s** dual runtime via `Runtime` interface in `internal/runtime/`
- **Credential management via Bridge** — Bridge pre-fetches OAuth2 tokens, Gate receives only short-lived tokens
- **Three auth backends** — `AUTH_BACKEND=memory` (default), `postgres`, or `rh-identity` (trusted `X-RH-Identity` header from Red Hat Turnpike, JIT user provisioning, no passwords)
- **SCM and tool APIs proxied through Gate** — MITM TLS interception of CONNECT tunnels to service domains with operation-level scope enforcement, real credentials never enter Skiff
- **Custom migration runner** — embedded SQL files, advisory locking, no external dependencies
- **Per-item catalog granularity** — catalog has a two-level hierarchy: sources (git repos, unit of distribution) and items (plugins, agents, LSPs, MCPs); catalog items are seeded from embedded data at compile time (no runtime cloning of catalog source repos); teams toggle individual items, not whole sources; enabled agents are referenced in workflow steps as `source/item` slugs; workflow definitions are validated at sync time for unknown/disabled agent references
- **Workflow graph with bounded cycles** — workflows support agent steps (Skiff pods) and bridge steps (deterministic `create-pr`/`await-ci`/`merge-pr` actions); `depends` expressions with `&&`/`||`; `max_iterations` prevents infinite loops in review/revision cycles
- **YAML is the single source of truth for schedules, security profiles, and tools** — no API-based creation, update, or deletion; schedules are defined via `schedule:` in `.alcove/agents/*.yml`; security profiles in `.alcove/security-profiles/*.yml`; tools come from catalog or builtin definitions; the API provides read-only access to synced data
- **`alcove.yaml` for infrastructure settings** — config file search order: `ALCOVE_CONFIG_FILE` env var → `./alcove.yaml` → `/etc/alcove/alcove.yaml`; env vars always override; `database_encryption_key` is required (Bridge refuses to start without it); `make up` auto-generates the file for local dev; file is gitignored
- **`.dev-credentials.yaml` for dev credentials** — single source of truth for local dev LLM provider and GitHub PAT; copy `.dev-credentials.yaml.example`, fill in values; `make dev-config` (run by `make up`) merges LLM settings into `alcove.yaml`; the dev-up process reads it to create API credentials in the database; file is gitignored
- **Dev containers are optional sidecars** — agent definitions can declare `dev_container.image` to run a project-provided container alongside Skiff; dev container images include the shim binary baked in (`make build-dev` builds the base image from `build/Containerfile.dev`); `Containerfile.dev` is an all-in-one dev container image that includes PostgreSQL 16, NATS, Go 1.25, and the shim binary; a simple shell entrypoint (`build/dev-entrypoint.sh`) starts PostgreSQL, NATS, and the shim without requiring a process supervisor; Podman creates a shared workspace volume at `/workspace` and mounts it in both containers; the shim provides bearer-auth-protected `POST /exec` for remote command execution with NDJSON streaming; `dev_container.network_access` controls network access (`internal` default, `external` joins both networks on Podman); on Kubernetes, the dev container runs as a native sidecar with emptyDir workspace volume (`DEV_CONTAINER_HOST=localhost:9090`); `--security-opt label=disable` handles SELinux compatibility on Podman; see architecture decision #20 in `docs/design/architecture-decisions.md` for the full design
- **Multi-repo support** — agent definitions use `repos:` (a list of `RepoSpec` with `name`, `url`, `ref` fields) instead of a single `repo:` string; Skiff receives a `REPOS` JSON env var and clones each repo into `/workspace/<name>/`; database migration `031_multi_repo.sql` replaces the `repo TEXT` column with `repos JSONB`; see architecture decision #21 in `docs/design/architecture-decisions.md` for the full design
- **CLAUDE.md injection** — Claude Code runs with `--bare` which disables native CLAUDE.md discovery; skiff-init reads `CLAUDE.md` from cloned repos and appends the content to the end of the agent prompt, so agents read their task first and project instructions provide supporting context; see architecture decision #22 in `docs/design/architecture-decisions.md` for the full design
- **Triple Team mode** — `triple_team: true` on agent definitions (or `--triple-team` CLI flag) prepends a prompt engineering wrapper that instructs Claude Code to use three phases of parallel sub-agents: Workers (3 diverse specialists), Evaluators (3 independent reviewers), and Integrators (3 synthesis agents); the wrapper text appears literally in the session prompt; defined in `internal/bridge/triple_team.go`
- **k3s for local Kubernetes testing** — `make k3s-setup` installs k3s with `--disable=traefik --disable=servicelb --bind-address=127.0.0.1`; `make k3s-up` builds images, imports them via `podman save | sudo k3s ctr images import`, deploys PostgreSQL+NATS as Deployments with PVC, creates a headless `alcove-bridge` Service with manual Endpoints pointing to the host IP; Bridge runs on the host with `RUNTIME=kubernetes` connecting via `~/.kube/k3s-config`; Skiff/Gate pods reach Bridge via the `alcove-bridge` k8s Service; port-forwards expose PostgreSQL (5432) and NATS (4222) to localhost; requires `sudo` for k3s install and image import; `make k3s-down` deletes the namespace but keeps k3s installed; see architecture decision #23 in `docs/design/architecture-decisions.md`

## Alcove CLI — Monitoring the SDLC on Staging

Alcove's development SDLC runs on the HCMAI staging server. Use the `alcove` CLI to monitor sessions, workflows, and pipeline progress.

### CLI Setup

```bash
alcove login --server https://alcove-bridge-pulp-stage.apps.rosa.hcmais01ue1.s9m2.p3.openshiftapps.com --username <user>
alcove version                       # verify client/server versions match
alcove teams list                    # list available teams
```

**Important:** All commands below require `--team "Alcove Development"` to target the right team. The SDLC pipelines, agents, and workflows all belong to the Alcove Development team.

### Monitoring Sessions

```bash
alcove list --team "Alcove Development"                    # all sessions
alcove list --team "Alcove Development" --status running    # only running sessions
alcove list --team "Alcove Development" --since 24h        # last 24 hours
alcove status <session-id> --team "Alcove Development"     # detailed session info
alcove logs <session-id> --team "Alcove Development"       # view session transcript
alcove logs <session-id> --proxy --team "Alcove Development"  # view Gate proxy log
alcove cancel <session-id> --team "Alcove Development"     # cancel a running session
```

### Monitoring Workflows

```bash
alcove workflows list --team "Alcove Development"                  # list workflow definitions
alcove workflows runs --team "Alcove Development"                  # list workflow runs (pipeline executions)
alcove workflows runs --team "Alcove Development" --status running  # only active runs
alcove workflows run <workflow-id> --team "Alcove Development"     # manually trigger a workflow
alcove workflows cancel <run-id> --team "Alcove Development"       # cancel a running workflow
alcove workflows export <run-id> --team "Alcove Development"       # export run data with transcripts for analysis
```

### Other Commands

```bash
alcove agents list --team "Alcove Development"        # synced agent definitions
alcove agents repos --team "Alcove Development"       # connected agent repos
alcove credentials list --team "Alcove Development"   # team credentials
```

## SDLC Pipelines and Label Behaviors

Alcove runs 6 automated pipelines defined in `.alcove/workflows/`. They are triggered by GitHub issue labels (via polling every 2 minutes) or by schedule.

### Label Reference

| Label | Trigger | Pipeline | What Happens |
|-------|---------|----------|--------------|
| `ready-for-dev` | GitHub issue labeled | **Full SDLC Pipeline** | Legacy autonomous pipeline. Bot claims the issue, implements the change, creates PR, waits for CI, runs code + security review, rebases, and merges. |
| `ready-for-sdlc-v2` | GitHub issue labeled | **SDLC v2 Pipeline** | Opt-in pipeline. Bot claims the issue, implements the change, creates PR, waits for CI, runs code + security review, and notifies maintainers. The PR requires human review and manual merge. Do not apply both trigger labels. |
| `ready-for-dev` | GitLab issue labeled | **GitLab SDLC Pipeline** | Same as above but using GitLab MRs, pipelines, and issue management. |
| `ready-for-dev` | JIRA issue labeled | **JIRA SDLC Pipeline** | Same as above but triggered from JIRA, code goes to GitHub, closes JIRA issue and posts comment when done. |
| `needs-planning` | GitHub issue labeled | **Milestone Planner** | Runs a 7-perspective planning committee (architect, security, devops, QA, UX, tech writer, performance). Produces an implementation plan in the issue body. Responds to follow-up comments on the issue. |
| `immediate-release` | GitHub issue labeled | **Release Pipeline** | Generates changelog, creates PR, waits for CI, merges, tags release, waits for container image build, deploys to staging via app-interface MR. |
| *(new issue opened)* | GitHub issue opened | **Backlog Triage** | Checks the new issue against all open issues for duplicates. If found, adds `possible-duplicate` label and comments. |

### SDLC Pipeline Steps (all three platforms share this structure)

```
1. claim-issue      → assign bot, remove trigger label (bridge action)
2. implement        → Autonomous Developer agent writes code, validates locally (go build/vet/test)
3. create-pr        → create PR or MR (bridge action, auto-detects platform)
4. await-ci         → poll for CI to pass (auto-recovers if checks don't appear)
5. ci-fix           → if CI fails, agent fixes and pushes (up to 3 iterations)
6. code-review      → PR Reviewer agent posts review (parallel with security-review)
7. security-review  → Security Reviewer agent reviews for vulnerabilities
8. revision         → if either review rejects, agent addresses feedback (up to 3 iterations)
9. rebase           → merge latest main into PR branch before merging
10. conflict-resolve → if rebase has conflicts, agent resolves them (up to 2 iterations)
11. merge           → merge the PR (serialized with advisory lock)
```

The JIRA pipeline adds two steps after merge: `close-jira` (transition to "Done") and `comment-jira` (post PR link).

The opt-in SDLC v2 pipeline omits rebase, conflict resolution, and merge. It
ends with `notify-ready` and `label-ready`; use `ready-for-sdlc-v2` when human
review and manual merge are required.

### Reliability Features

- **Pre-push validation**: agents must pass `go build`, `go vet`, and `go test` in the dev container before pushing
- **CI-fix loop**: if CI fails, the pipeline re-dispatches the agent with failure logs (up to 3 iterations)
- **Await-CI recovery**: if GitHub doesn't create check suites within 60s, Bridge pushes an empty commit; at 120s, closes and reopens the PR
- **Rebase before merge**: updates the PR branch to latest main, preventing merge conflicts
- **Merge serialization**: PostgreSQL advisory lock prevents concurrent merge races
- **Output contracts**: review steps define required outputs (`approved`, `comments`) with allowed values; invalid outputs trigger retry

## Dev Container Limitations

The dev container provides PostgreSQL, NATS, and Go for running tests.
It does NOT support dispatching Skiff/Gate sessions — container deployment
is validated by CI after the PR is created. The agent should use
`go test ./...`, `go build ./...`, and `go vet ./...` via the dev
container's shim endpoint, but should NOT attempt to test session dispatch.

## Dev Container Usage

When a dev container is available (`$DEV_CONTAINER_HOST` is set), use it for all build, test, and lint commands instead of running them directly. The dev container has the full project toolchain.

```bash
# Check dev container health
curl -s http://$DEV_CONTAINER_HOST/healthz

# Run tests
curl -s -X POST http://$DEV_CONTAINER_HOST/exec \
  -H "Authorization: Bearer $DEV_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"cmd":"cd /workspace && make test","timeout":300}'

# Build
curl -s -X POST http://$DEV_CONTAINER_HOST/exec \
  -H "Authorization: Bearer $DEV_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"cmd":"cd /workspace && go build ./...","timeout":120}'

# Run go vet
curl -s -X POST http://$DEV_CONTAINER_HOST/exec \
  -H "Authorization: Bearer $DEV_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"cmd":"cd /workspace && go vet ./...","timeout":120}'
```

Do not run build/test commands directly when a dev container is available -- always use the dev container via `POST /exec`.
