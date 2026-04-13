# Contributing to cluster-api-provider-ovhcloud

Thank you for your interest in contributing! This document describes the process
for contributing code, documentation, and bug reports to the project.

## Code of Conduct

This project follows the [CNCF Code of Conduct](CODE_OF_CONDUCT.md). By
participating, you agree to abide by its terms.

## Developer Certificate of Origin (DCO)

All commits must be signed off using the Developer Certificate of Origin (DCO):

```bash
git commit -s -m "Your commit message"
```

This adds a `Signed-off-by:` trailer to your commit, certifying that you have
the right to submit the contribution under the project's open source license.

## Development setup

### Prerequisites

- Go 1.24+
- Docker or Podman
- kubectl 1.31+
- A Kubernetes management cluster (kind, k3d, or any RKE2/K8s cluster) for
  testing. The cluster must have CAPI core installed (e.g. via
  [Rancher Turtles](https://turtles.docs.rancher.com/) or `clusterctl init`).
- An OVH Public Cloud project with API credentials. See
  [docs/ovh-credentials-guide.md](docs/ovh-credentials-guide.md).

### Build

```bash
make build           # local binary
make docker-build    # container image
```

### Test

```bash
make test            # unit + envtest
make lint            # golangci-lint
make verify          # all checks (modules, generated code, manifests, lint)
```

### Run a controller locally

You can run the controller from your machine, talking to a remote management
cluster:

```bash
export KUBECONFIG=~/.kube/config-mgmt
make install         # install CRDs into the cluster
make run             # run the controller against the cluster
```

## Pull request process

1. **Fork** the repository to your GitHub account
2. **Branch** from `main` with a descriptive name (e.g. `feature/floating-ip`,
   `fix/orphan-lb-cleanup`)
3. **Commit** with DCO sign-off (`git commit -s`). Use clear, atomic commits.
4. **Test** locally: `make verify test`
5. **Push** to your fork
6. **Open a PR** against `main`. Describe the change, why it's needed, and how
   it was tested. Link any related issues.
7. **Address review feedback**. Squash fixup commits before merge.

## Commit message conventions

- First line: imperative mood, < 72 chars (e.g. "Add floating IP support")
- Body: explain *why* the change is needed, not just *what*
- Reference issues with `Fixes #123` / `Refs #456`
- Always sign off with `git commit -s`

## Code style

- Follow standard Go conventions; `gofmt`, `goimports`, and `gci` are enforced.
- See `.golangci.yml` for the full lint configuration.
- Imports order: stdlib → blank → dot → default → k8s.io → sigs.k8s.io/cluster-api → local
- Maximum line length: 150 characters.
- Functions: max 110 lines / 60 statements.

## Reporting issues

When opening an issue, please include:

- CAPIOVH version (or commit SHA if built from source)
- Kubernetes management cluster version
- CAPI core version
- OVH region used
- Steps to reproduce
- Expected vs actual behaviour
- Logs from the CAPIOVH controller (`kubectl -n capiovh-system logs deploy/capiovh-controller-manager`)

For security-related issues, please follow [SECURITY.md](SECURITY.md) instead.

## Documentation contributions

Documentation lives under `docs/` and as Markdown in the repo. Doc-only PRs do
not require sign-off (DCO) but should follow the same review process.
