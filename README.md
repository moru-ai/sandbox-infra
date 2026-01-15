# Moru Sandbox Infrastructure

This is the infrastructure powering [Moru](https://moru.io)'s sandbox compute platform.

## About

Moru Sandbox Infrastructure provides secure, isolated execution environments using Firecracker microVMs. It enables running untrusted code safely in the cloud with full process isolation.

**Repository**: [github.com/moru-ai/sandbox-infra](https://github.com/moru-ai/sandbox-infra)

## Self-hosting

Read the [self-hosting guide](./self-host.md) to learn how to set up the infrastructure on your own. The infrastructure is deployed using Terraform.

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

## Acknowledgement

This project is a fork of [E2B Infrastructure](https://github.com/e2b-dev/infra).

## License

See [LICENSE](./LICENSE) file.
