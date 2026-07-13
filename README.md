# ⚡ Lattice

**A free, open-source inference grid that pools your Ollama/vLLM/LM Studio/MLX/llama.cpp nodes behind one endpoint.**

Lattice connects machines running inference engines into a unified grid. A central server (designed for Kubernetes) accepts OpenAI-compatible requests and routes them to the least-loaded available engine. Agents auto-detect local inference servers and register them with the grid.

No accounts. No paid features. No relay services. Just your hardware, connected.

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│                  Lattice Server (K8s)                        │
│                                                              │
│  ┌────────┐  ┌──────────┐  ┌──────────┐  ┌─────────────┐  │
│  │ Web UI │  │ Registry │  │ Router   │  │ OpenAI API  │  │
│  │  /ui/  │  │ /nodes/* │  │ (load)   │  │ /v1/*       │  │
│  └────────┘  └──────────┘  └──────────┘  └─────────────┘  │
│                                                              │
│  Endpoints: / /grid/info /nodes/discover /v1/models         │
│             /v1/chat/completions /v1/completions             │
│             /v1/media/image/generate /v1/media/video/i2v    │
└──────────────────┬──────────────────┬────────────────────────┘
                   │                  │
        ┌──────────▼──────┐   ┌───────▼──────────┐
        │   Agent #1      │   │   Agent #2       │
        │   auto-detect:  │   │   --at vllm      │
        │   ollama:11434  │   │   --at mlx:8080  │
        ├─────────────────┤   ├──────────────────┤
        │ Ollama          │   │ vLLM / MLX       │
        │ llama3.2, qwen  │   │ mistral, gemma   │
        └─────────────────┘   └──────────────────┘
```

## Quickstart

### Build

```bash
make build
# Produces: bin/lattice-server, bin/lattice-agent, bin/lattice
```

### Run Server

```bash
./bin/lattice-server --port 8090 --name home
```

### Join Engines

```bash
# Auto-detect all running inference engines on this machine
./bin/lattice-agent --server http://your-server:8090 --all

# Or join a specific Ollama
./bin/lattice-agent --server http://your-server:8090 --ollama http://localhost:11434

# Or use the CLI
./bin/lattice join --at http://localhost:11434/v1 -m llama3.2:latest --name my-gpu
```

### Use the Grid

```bash
# List models
./bin/lattice models --verbose

# Chat
./bin/lattice chat -m llama3.2:latest "hello from the grid"

# Info
./bin/lattice info --env
# export OPENAI_BASE_URL="http://192.168.1.10:8090/v1"
# export OPENAI_API_KEY="local-grid"
```

### Point Any App at the Grid

```python
from openai import OpenAI
client = OpenAI(base_url="http://lattice:8090/v1", api_key="local-grid")
client.chat.completions.create(
    model="llama3.2:latest",
    messages=[{"role": "user", "content": "hello"}],
)
```

## API Reference

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Grid info (same as `/grid/info`) |
| `/grid/info` | GET | Grid metadata: id, name, type, engines_online, ttl |
| `/nodes` | POST | Create a node slot |
| `/nodes/{node_id}` | PUT | Register/update an engine (auto-creates if needed) |
| `/nodes/heartbeat` | POST | Refresh node TTL and update load |
| `/nodes/{node_id}` | DELETE | Unregister a node |
| `/nodes/discover` | GET | List active engines (optional `?model=` filter) |
| `/v1/models` | GET | OpenAI-compatible model listing |
| `/v1/chat/completions` | POST | Proxy to best engine (stream supported) |
| `/v1/completions` | POST | Proxy to best engine |
| `/v1/media/image/generate` | POST | Proxy to ComfyUI media engine |
| `/v1/media/image/edit` | POST | Proxy to ComfyUI media engine |
| `/v1/media/video/i2v` | POST | Proxy to ComfyUI media engine |
| `/healthz` | GET | Health check |
| `/ui/` | GET | Web dashboard |

## CLI Commands

| Command | Description |
|---------|-------------|
| `lattice up [name]` | Bring a grid online |
| `lattice down [name]` | Take a grid offline |
| `lattice ls` | List grids |
| `lattice info [--env] [--json]` | Show grid info or export env vars |
| `lattice use <name>` | Set the active grid |
| `lattice join [grid] --at <url> -m <model>` | Join an engine to the grid |
| `lattice join [grid] --all` | Join all detected local engines |
| `lattice leave [grid]` | Leave and unregister |
| `lattice models [--verbose] [--json]` | List live models |
| `lattice engines [--json]` | List live engines |
| `lattice chat -m <model> <message>` | Send a chat message |
| `lattice version` | Print version |

## Routing Algorithm

Engines are sorted by `active_tasks` (ascending), then by freshness of heartbeat. The first match wins — routing to the engine with the least current load that serves the requested model.

When a model is advertised under an alias (`--advertise-as`), the proxy rewrites the model name to the engine's real name before forwarding, using the `upstream` map.

## Engine Auto-Detection

The agent probes well-known local ports in priority order:

| Engine | Port | Detection |
|--------|------|-----------|
| Ollama | 11434 | GET /api/tags |
| LM Studio | 1234 | GET /v1/models |
| vLLM | 8000 | GET /v1/models |
| MLX | 8080 | GET /v1/models |
| llama.cpp | 8081 | GET /v1/models |
| ComfyUI | 8188 | GET /system_stats |

## Kubernetes Deployment

```bash
kubectl apply -f deploy/k8s/server.yaml
kubectl label node gpu-node-1 lattice.io/ollama=true
kubectl apply -f deploy/k8s/agent.yaml
```

## Configuration

### Server

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | 8090 | Listen port |
| `--host` | 0.0.0.0 | Listen host |
| `--name` | home | Grid name |
| `--grid-id` | auto | Grid ID |
| `--node-ttl` | 60 | Seconds before node is reaped |

### Agent

| Flag | Default | Description |
|------|---------|-------------|
| `--server` | http://localhost:8090 | Grid server URL |
| `--ollama` | http://localhost:11434 | Local Ollama URL |
| `--at` | | External engine URL |
| `--all` | false | Detect all local engines |
| `--name` | hostname | Engine display name |
| `--heartbeat-interval` | 15.0 | Heartbeat seconds |

## License

MIT
