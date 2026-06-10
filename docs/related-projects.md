# Related Projects

The agentic SDLC space is moving fast. This page compares Alcove to the
projects we're aware of, grouped by what they actually do. If something is
missing or wrong, open a PR.

## Quick Comparison

| Project | License | What It Does | Sandboxed? | Self-Hostable? |
|---------|---------|-------------|-----------|---------------|
| [Alcove](https://github.com/alcove-ai/alcove) | Apache-2.0 | Sandboxed agents on OpenShift/k8s | Yes (container + network + credential) | Yes |
| [Gastown](https://github.com/gastownhall/gastown) | MIT | Multi-agent orchestration (20-30 agents) | No | Yes |
| [Paperclip](https://github.com/paperclipai/paperclip) | MIT | "Company of agents" with org chart and budgets | No | Yes |
| [Botminter](https://github.com/botminter/botminter) | Open source | Convention-driven CLI for agent teams | No | Yes |
| [Fullsend](https://github.com/fullsend-ai/fullsend) | Apache-2.0 | Autonomous agentic engineering for GitHub orgs | No | Yes |
| [Ambient Code](https://github.com/ambient-code/platform) | MIT | K8s-native multi-agent Claude Code workflows | No | Yes |
| [Hummingbird Agent](https://gitlab.com/redhat/hummingbird/tools/-/tree/main/hummingbird-agent) | Internal | Production SDLC agents for container/RPM repos | Partial (no credential injection) | Internal |
| [OpenShell](https://github.com/NVIDIA/OpenShell) | Apache-2.0 | Kernel-enforced sandbox for local AI agents | Yes (Landlock + seccomp + network) | Yes (local only) |
| [devaipod](https://github.com/cgwalters/devaipod) | Apache-2.0 | Local sandboxed agent execution via devcontainers | Yes (Podman + service-gator) | Yes (local only) |
| [Devin](https://devin.ai) | Proprietary | Commercial AI software engineer | Yes (cloud) | No |

## Sandboxed Hosted Platforms

These run agents in isolated containers on shared infrastructure — the same
category as Alcove.

### Hummingbird Agent

Red Hat's Project Hummingbird runs agentic SDLC in production on Hummingbird
container and RPM repositories. Two agents are live today: a Failure Analysis
Agent (investigates CI failures across GitLab CI, Konflux, and Testing Farm)
and a Code Review Agent (reviews every MR). The system produces 1,000+
deterministic commits weekly across 400+ packages.

Hummingbird takes a "no credentials" approach — agents never receive secrets,
eliminating the prompt injection attack surface entirely. This is simpler than
Alcove's Gate proxy model but limits what agents can do autonomously (e.g., no
authenticated API calls from within an agent session).

Hummingbird separates high-volume deterministic work (dependency bumps,
lockfile syncs — auto-merged without agents) from lower-volume agentic work
that always requires human approval before merge.

### Fullsend

An Apache-2.0 Go project exploring fully autonomous agentic engineering for
GitHub-hosted organizations. Has 20+ design documents covering security threat
models, agent architecture, autonomy spectrum, governance, and production
feedback loops.

Fullsend is more design-document-oriented than infrastructure-focused. Its
proving ground is konflux-ci (a Kubernetes-native CI/CD platform). Does not
currently provide container isolation, credential proxying, or network
enforcement — those are described as future work.

### Ambient Code

A Kubernetes-native platform for multi-agent Claude Code workflows, built by
Red Hat engineers as a personal project (not an official Red Hat product).
Uses Go operators, a Next.js frontend, and a Python runner. Supports
specialized AI personas (Product Manager, Staff Engineer, UX Designer, etc.)
with GitHub/GitLab integration and real-time monitoring.

Does not include a credential isolation layer or network sandboxing model.

## Orchestration Platforms

These coordinate multiple agents working in parallel but don't provide
container isolation. They're complementary to sandboxed platforms — an
orchestrator could dispatch work into Alcove sessions.

### Gastown

Created by Steve Yegge. Coordinates 20-30 agents working on a single
codebase using git worktrees for isolation. Has a Mayor/Polecat/Refinery
role hierarchy, a Bors-style merge queue, and a git-backed issue tracker
called Beads for persistent agent memory across sessions.

Runs unsandboxed on the developer's machine — agents have full filesystem
access. No network isolation or credential management. Reports ~$100/hour
burn rate. Has auto-merged failing tests. The author describes it as
"managing a very fast, very junior dev team."

### Paperclip

An MIT-licensed Node.js platform that models agent teams as a company: org
chart, reporting lines, job descriptions, per-agent monthly budgets. You act
as the board of directors. Agents connect via adapters — any tool that can
receive a heartbeat can be "hired." 50k+ GitHub stars since launching March
2026.

The company metaphor provides intuitive governance but adds abstraction for
simpler pipelines. No container isolation or network security model.
Heartbeat-based scheduling adds latency versus event-driven dispatch.

### Botminter

A pre-alpha CLI tool (`bm`) that organizes Claude Code agents into
convention-driven teams with layered knowledge scoping and methodology
profiles (e.g., "scrum" for multi-agent, "agentic-sdlc-minimal" for
single-agent). Uses the Ralph orchestrator as its backend. GitHub board
integration provides visibility into agent decisions.

Very early stage with minimal public presence. No server component, container
isolation, or credential management.

## Local Sandboxing

These run on a single developer machine, providing isolation without shared
infrastructure.

### OpenShell

Founded by NVIDIA with Red Hat as active contributor. Provides
kernel-enforced isolation via Landlock filesystem restrictions, seccomp
syscall filtering, and network namespace isolation with per-binary OPA/Rego
policies. Includes L7 HTTP inspection via TLS interception. Supports five
backends: Kubernetes, Docker, Podman, libkrun microVM, and QEMU VM. Built in
Rust, runs as a lightweight K3s cluster inside a single container.

Currently alpha and single-player. OpenShell is local-first sandboxing;
Alcove is remote-first multi-tenant. The two serve different use cases and
could be complementary — OpenShell for developer desktops, Alcove for shared
CI/CD infrastructure.

### devaipod

Created by Colin Walters (former Red Hat, Rust inventor contributor).
Local-first sandboxed agent execution using Podman and devcontainers. Uses
service-gator for fine-grained credential scoping. Designed to support any
tool that speaks ACP (Agent Client Protocol).

Explicitly local and single-user. Focused on giving the developer full
control over the execution environment. See the
[devaipod comparison page](https://cgwalters.github.io/devaipod/related-projects.html)
for Colin's detailed analysis of this space.

## Where Alcove Fits

Alcove occupies a specific niche: **sandboxed, multi-tenant, remote agent
execution with credential isolation**. The key differentiator is Gate — a
per-session MITM proxy that ensures real credentials never enter the agent
container while still allowing authenticated API calls.

The orchestration platforms (Gastown, Paperclip, Botminter) solve a different
problem — coordinating many agents — and could in principle dispatch work into
Alcove sessions. The local sandboxes (OpenShell, devaipod) serve individual
developers rather than shared infrastructure.

The closest comparison is Hummingbird Agent, which solves the same production
SDLC problem but takes a stricter "no credentials at all" approach. Fullsend
and Ambient Code are in the same space but younger and without isolation
layers.
