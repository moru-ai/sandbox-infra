# Moru

An open-source runtime for AI agents that runs each session in an isolated Firecracker microVM.

Run agent harnesses like Claude Code or Codex in the cloud, giving each session its own isolated microVM with filesystem and shell access. From outside, you talk to the VM through the Moru CLI or TypeScript/Python SDK. Inside, it's just Linux—run commands, read/write files, anything you'd do on a normal machine.

## Why Moru?

When an agent needs to solve complex problems, giving it filesystem + shell access works well because it (1) handles large data without pushing everything into the model context window, and (2) reuses tools that already work (Python, Bash, etc.).

Now models run for hours on real tasks. As models get smarter, the harness should give models more autonomy, but with safe guardrails. Moru helps developers focus on building agents, not the underlying runtime and infra.

## Quickstart

### 1. Get Your API Key

Sign up at [moru.io/dashboard](https://moru.io/dashboard) and create an API key.

### 2. Install the SDK

```bash
# Python
pip install moru

# JavaScript/TypeScript
npm install @moru-ai/core
```

### 3. Create a Sandbox

**Python:**
```python
from moru import Sandbox

sandbox = Sandbox.create()
result = sandbox.commands.run("echo 'Hello from Moru!'")
print(result.stdout)
sandbox.kill()
```

**TypeScript:**
```ts
import Sandbox from '@moru-ai/core'

const sandbox = await Sandbox.create()
const result = await sandbox.commands.run("echo 'Hello from Moru!'")
console.log(result.stdout)
await sandbox.kill()
```

## Examples

### Run Commands

```python
from moru import Sandbox

sandbox = Sandbox.create()

# Run any shell command
result = sandbox.commands.run("python3 --version")
print(result.stdout)
print(f"Exit code: {result.exit_code}")

sandbox.kill()
```

### Read and Write Files

```python
from moru import Sandbox

sandbox = Sandbox.create()

# Write a file
sandbox.files.write("/tmp/data.txt", "Hello from Moru!")

# Read it back
content = sandbox.files.read("/tmp/data.txt")
print(content)

sandbox.kill()
```

### Use a Custom Template

Define a Dockerfile, and Moru builds it into a template you can spawn instantly.

```python
from moru import Sandbox

# Use a template with your dependencies pre-installed
sandbox = Sandbox.create("my-template")
result = sandbox.commands.run("my-custom-tool --version")
print(result.stdout)
sandbox.kill()
```

## Self-hosting

Read the [self-hosting guide](./self-host.md) to run Moru on your own infrastructure. Deployed using Terraform.

Supported cloud providers:
- GCP
- AWS (in progress)

## Architecture

The infrastructure consists of several core services:

- **API** - REST API for sandbox management
- **Orchestrator** - Firecracker microVM orchestration
- **Envd** - In-VM daemon for process and filesystem management
- **Client Proxy** - Edge routing layer

See [CLAUDE.md](./CLAUDE.md) for detailed architecture documentation.

## Development

```bash
# Setup environment
make switch-env ENV=staging
make login-gcloud
make init

# Run tests
make test

# Build and deploy
make build-and-upload
make plan
make apply
```

## How It Works

Each VM is a snapshot of a Docker build. You define a Dockerfile, CPU, and memory limits—Moru runs the build inside a Firecracker VM, then pauses and saves the exact state: CPU, dirty memory pages, and changed filesystem blocks.

When you spawn a new VM, it resumes from that template snapshot. Memory snapshots are lazy-loaded via userfaultfd, which helps sandboxes start within a second.

Each VM runs on Firecracker with KVM isolation and a dedicated kernel. Network uses namespaces for isolation and iptables for access control.

## Acknowledgement

Moru started as a fork of [E2B](https://github.com/e2b-dev/infra), and most of the low-level Firecracker runtime is still from upstream.

## License

See [LICENSE](./LICENSE) file.
