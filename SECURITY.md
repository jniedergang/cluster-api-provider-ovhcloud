# Security Policy

## Reporting a vulnerability

If you discover a security vulnerability in cluster-api-provider-ovhcloud,
please **do not open a public issue**. Instead, report it privately so we can
fix it before it becomes widely known.

### How to report

- Email the maintainers (see [MAINTAINERS.md](MAINTAINERS.md))
- Or use GitHub's private vulnerability disclosure feature on the repository

Please include:

- A description of the vulnerability and its impact
- Steps to reproduce
- The version (or commit SHA) affected
- Any suggested mitigations

We will acknowledge receipt within 7 days and aim to provide a fix or
mitigation plan within 30 days.

## Security model

### OVH API credentials

The provider authenticates to OVH using an
[Application Key + Application Secret + Consumer Key](docs/ovh-credentials-guide.md).
These credentials are stored in a Kubernetes Secret referenced by the
`OVHCluster.spec.identitySecret` field.

**Best practices**:

- The Consumer Key MUST be scoped to a single Public Cloud project, not the
  entire OVH account. See the credentials guide for the exact `accessRules`.
- Store the Secret in the same namespace as the `OVHCluster` resource, and use
  RBAC to restrict access to that namespace.
- Rotate the Consumer Key periodically (default OVH expiration is configurable
  at credential creation time).

### Network isolation

The provider creates an OVH private network (vRack) per cluster by default.
Cross-cluster traffic is isolated at the network layer.

For internet-facing API server access, use the floating IP support
(`spec.loadBalancerConfig.floatingIPNetwork`). The control plane endpoint will
then be a public IP attached to the OVH Octavia load balancer.

### Webhook validation

When deployed with `webhooks.enabled=true` and `certManager.enabled=true`, the
provider runs validating admission webhooks that reject malformed `OVHCluster`
and `OVHMachine` resources. This is recommended for production deployments.

### Container image

The controller manager image runs as a non-root user (UID 65532) on a minimal
SUSE BCI base image. CGO is disabled to avoid dynamic linking.

## Supported versions

Until v1.0.0, only the latest tagged release is supported. After v1.0.0, we
plan to support the latest two minor releases.
