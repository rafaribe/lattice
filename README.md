# ⚡ Beagrid

**A free, open-source inference grid that pools your Ollama nodes behind one endpoint.**

Beagrid connects multiple machines running [Ollama](https://ollama.com) into a unified inference grid. A central server (designed for Kubernetes) accepts OpenAI-compatible requests and routes them to the best available node using a priority-based algorithm. Agents on each machine discover local Ollama instances and advertise their models to the grid.

No accounts. No paid features. Just your hardware, connected.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Beagrid Server (K8s)                │
│                                                       │
│  ┌─────────┐  ┌──────────┐  ┌────────────────────┐  │
│  │ Web UI  │  │ Registry │  │ Priority Router    │  │
│  └─────────┘  └──────────┘  └────────────────────┘  │
│                      ▲                                │
│          /v1/chat/completions                         │
└──────────────┬──────────────────┬────────────────────┘
               │                  │
        ┌──────▼──────┐    ┌──────▼──────┐
        │  Agent #1   │    │  Agent #2   │
        │  (daemon)   │    │  (daemon)   │
        ├─────────────┤    ├─────────────┤
        │ Ollama      │    │ Ollama      │
        │ llama3.2    │    │ mistral     │
        │ codellama   │    │ qwen2.5     │
        └─────────────┘    └─────────────┘
```

## Features

- **OpenAI-compatible endpoint** — drop-in replacement for any tool that speaks the OpenAI chat API
- **Priority-based routing** — weighted algorithm considering priority, active load, error rate, and latency
- **Auto-discovery** — agents automatically discover and advertise models from local Ollama instances
- **Heartbeat + auto-recovery** — nodes marked offline after missed heartbeats, agents re-register automatically
- **Web dashboard** — real-time grid topology, node status, and model availability
- **Kubernetes-native** — Deployment for the server, DaemonSet for agents, all manifests included
- **Streaming support** — full SSE streaming passthrough for chat completions
- **Zero dependencies** — single static Go binaries for both server and agent

## Quickstart

### Binary

```bash
# Build
make build

# Run the server
./bin/beagrid-server --port 8080

# On each Ollama machine, run the agent
./bin/beagrid-agent --server http://your-server:8080 --ollama http://localhost:11434 --name my-gpu-node --priority 1
```

### Docker

```bash
# Build images
make docker

# Run server
docker run -p 8080:8080 beagrid-server:dev

# Run agent (on Ollama machines)
docker run --network host beagrid-agent:dev \
  --server http://your-server:8080 \
  --ollama http://localhost:11434
```

### Kubernetes

```bash
# Deploy server
kubectl apply -f deploy/k8s/server.yaml

# Label nodes running Ollama
kubectl label node gpu-node-1 beagrid.io/ollama=true

# Deploy agent DaemonSet
kubectl apply -f deploy/k8s/agent.yaml
```

## Usage

Once agents have joined, send requests to the server's OpenAI-compatible endpoint:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3.2:latest",
    "messages": [{"role": "user", "content": "Hello from the grid!"}]
  }'
```

Point any OpenAI-compatible tool at the grid:

```bash
export OPENAI_BASE_URL="http://beagrid-server:8080/v1"
export OPENAI_API_KEY="not-needed"
```

## API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | OpenAI-compatible inference (routes to best node) |
| `/api/v1/nodes` | GET | List all registered nodes |
| `/api/v1/nodes/{id}` | GET | Get a specific node |
| `/api/v1/nodes/register` | POST | Register a new node |
| `/api/v1/nodes/heartbeat` | POST | Send node heartbeat |
| `/api/v1/nodes/{id}` | DELETE | Remove a node |
| `/api/v1/grid/info` | GET | Grid statistics |
| `/healthz` | GET | Health check |

## Routing Algorithm

The priority router scores each candidate node using:

| Factor | Weight | Description |
|--------|--------|-------------|
| Priority | 40% | Configurable per-node (lower = better) |
| Active Load | 30% | Current concurrent requests |
| Error Rate | 20% | Historical error percentage |
| Latency | 10% | Average response time |

Lowest score wins.

## Web UI

Access the dashboard at `http://your-server:8080/`. It shows:

- Grid topology visualization with live connection lines
- Node cards with status, priority, models, and request stats
- Real-time stats (online nodes, unique models, total requests)
- Auto-refreshes every 5 seconds

## Configuration

### Server

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | 8080 | Listen port |
| `--heartbeat-timeout` | 30s | Duration before marking a node offline |

### Agent

| Flag | Default | Description |
|------|---------|-------------|
| `--server` | http://localhost:8080 | Beagrid server URL |
| `--ollama` | http://localhost:11434 | Local Ollama URL |
| `--name` | hostname | Node display name |
| `--priority` | 10 | Routing priority (lower = preferred) |

## Project Structure

```
beagrid/
├── cmd/
│   ├── server/          # Server binary + embedded web UI
│   └── agent/           # Agent binary
├── internal/
│   ├── domain/          # Core types and port interfaces
│   ├── server/          # Registry, router, HTTP handlers
│   ├── agent/           # Daemon, Ollama adapter
│   └── proxy/           # Inference proxy to Ollama
├── deploy/
│   ├── k8s/             # Kubernetes manifests
│   ├── Dockerfile.server
│   └── Dockerfile.agent
└── Makefile
```

## License

MIT
