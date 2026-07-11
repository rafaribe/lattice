# Beagrid — Project Context

## What is this?

Beagrid is a free, open-source inference grid that pools Ollama/vLLM/LM Studio/MLX/llama.cpp nodes behind one OpenAI-compatible endpoint. A central server (designed for Kubernetes) accepts requests and routes them to the least-loaded available engine. Agents auto-detect local inference servers and register them with the grid.

**Repo**: github.com/rafaribe/beagrid
**Language**: Go 1.26
**Owner**: rafaribe

## Architecture (Hexagonal / Ports & Adapters)

```
internal/
  domain/                           # Core types, value objects (zero external deps)
    types.go                        # Node, Load, Role, GridInfo, OpenAI types, etc.
    ports.go                        # Doc-only (interfaces live in application/)

  application/                      # Outbound port interfaces
    ports.go                        # NodeRegistry, EngineProxy, EngineDetector,
                                    # OllamaClient, GridServer, OllamaModel

  adapters/
    inbound/
      http/handler.go               # HTTP inbound adapter (all API endpoints)
                                    # Depends on: application.NodeRegistry, application.EngineProxy
    outbound/
      registry/memory.go            # In-memory node registry (implements NodeRegistry)
      registry/memory_test.go       # 7 tests (TTL, load routing, CRUD, info)
      engine/proxy.go               # HTTP proxy to inference engines (implements EngineProxy)
      engine/ollama.go              # Ollama API client (implements OllamaClient)
      engine/detect.go              # Multi-engine local detector (implements EngineDetector)
      engine/gridclient.go          # Agent→server HTTP client (implements GridServer)

  agent/
    daemon.go                       # Agent use-case: detect → register → heartbeat loop
                                    # Accepts all outbound ports via constructor injection

cmd/
  server/main.go                    # Composition root: registry + proxy → handler
  server/web/index.html             # Embedded web UI (go:embed, served at /ui/)
  agent/main.go                     # Composition root: detector + ollama + gridclient → daemon
  beagrid/main.go                   # CLI: up, down, ls, info, join, leave, models, engines, chat
```

## Key Design Decisions

- **Dependency direction**: Always inward (adapters → application → domain)
- **No global singletons**: All wiring in cmd/*/main.go composition roots
- **Handler depends on interfaces**: `application.NodeRegistry` and `application.EngineProxy`
- **Agent receives ports via constructor**: `NewDaemon(cfg, detector, ollama, gridClient, logger)`
- **UI bundled in server only**: CLI has no web assets (`//go:embed all:web` only in cmd/server)

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Grid info (same as `/grid/info`) |
| `/grid/info` | GET | Grid metadata |
| `/version` | GET | Server version + uptime |
| `/nodes` | POST | Create node slot (returns 201) |
| `/nodes/{node_id}` | PUT | Register/update engine (auto-creates) |
| `/nodes/heartbeat` | POST | Refresh TTL + update load |
| `/nodes/{node_id}` | DELETE | Unregister node |
| `/nodes/discover` | GET | List active engines (?model= filter) |
| `/v1/models` | GET | OpenAI model listing |
| `/v1/chat/completions` | POST | Proxy to best engine (stream supported) |
| `/v1/completions` | POST | Proxy to best engine |
| `/v1/media/image/generate` | POST | Proxy to ComfyUI |
| `/v1/media/image/edit` | POST | Proxy to ComfyUI |
| `/v1/media/video/i2v` | POST | Proxy to ComfyUI |
| `/healthz` | GET | Liveness probe |
| `/readyz` | GET | Readiness probe (checks registry) |
| `/ui/` | GET | Web dashboard |

### API Features
- Body size limits: 1MB (node ops), 10MB (completions)
- X-Request-ID middleware (generates UUID if not provided)
- Proper OpenAI error format on all error paths
- Nil-safe: discover always returns `[]`, never `null`

## CI / CD

### Workflows (.github/workflows/)

1. **ci.yaml** — Triggers on push to main and tags
   - Test job: `go test ./... -race`
   - Build Server Image: multi-arch (amd64/arm64) → GHCR
   - Build Agent Image: multi-arch (amd64/arm64) → GHCR
   - Uses `svu` for automatic semver from commit history
   - Tags: `{version}`, `{version}-{sha}`, `latest`

2. **release-cli.yaml** — Triggers on push to main (path-filtered to code changes)
   - Installs `svu`, calculates next version
   - Cross-compiles CLI for 6 platforms (linux/darwin/windows × amd64/arm64)
   - Creates git tag `v{version}`
   - Creates GitHub Release with binaries + checksums

### Versioning Strategy (svu)
- Conventional commits drive version bumps:
  - `feat:` → minor (0.1.0 → 0.2.0)
  - `fix:` → patch (0.2.0 → 0.2.1)
  - `feat!:` or `BREAKING CHANGE:` → major
- No manual tagging needed — CI handles it automatically

### Container Images
- `ghcr.io/rafaribe/beagrid-server:{version}`
- `ghcr.io/rafaribe/beagrid-agent:{version}`

## Deployment (Home-ops)

### Location in home-ops repo
```
kubernetes/apps/ai/beagrid/
  ks.yaml                   → Flux Kustomization (targets ai namespace)
  app/
    helmrelease.yaml        → app-template HelmRelease (server + agent controllers)
    kustomization.yaml
```

### Architecture
- Server: `ghcr.io/rafaribe/beagrid-server` on port 8090
- Agent: `ghcr.io/rafaribe/beagrid-agent` → connects to `http://10.0.0.98:11434` (Windows Ollama)
- Route: `beagrid.rafaribe.com` via envoy-internal
- Persistence: VolSync (1Gi)
- Monitoring: Gatus subdomain

### To deploy: add `- ./beagrid/ks.yaml` to `kubernetes/apps/ai/kustomization.yaml`

## Local Development

### Prerequisites
- Go 1.26+
- mise (optional, for task runner)
- Docker (for compose stack)

### Common commands
```bash
mise run build          # Build all 3 binaries
mise run test           # go test ./... -race
mise run test:cover     # Coverage report → coverage.html
mise run lint           # golangci-lint
mise run run            # Start server locally on :8090
mise run docker:up      # Full stack (server + ollama + agent)
mise run ci             # Full CI pipeline locally
```

### Docker Compose
```bash
docker compose up                     # server + ollama + agent
docker compose --profile gpu up       # with NVIDIA GPU passthrough
```

## Skills (Kiro Global — ~/.kiro/skills/)

| Skill | Purpose |
|-------|---------|
| `fable-mode` | Staged execution discipline: plan → delegate → verify → self-critique |
| `hexagonal-architecture` | Ports & Adapters design, Go module layout, refactoring playbook |
| `build-mcp-server` | MCP server scaffolding (TypeScript) |

## Known Weaknesses / Tech Debt

1. **EngineProxy port leaks HTTP**: Takes `http.ResponseWriter` + `*http.Request` — acceptable since only HTTP transport exists, but impure
2. **No persistence**: Registry is in-memory; nodes lost on restart (by design for ephemeral grid)
3. **No auth**: LAN-only, permissionless — intentional for home use
4. **race between release-cli and CI**: Both run on push to main. Path filters prevent collision in practice but could theoretically race on tag creation
5. **Agent tests**: No unit tests for daemon.go yet — covered by integration testing against real Ollama
