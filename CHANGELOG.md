# Changelog

All notable changes to this provider are recorded here.
This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-08-27

### Added

Initial provider covering the CloudAxion IaaS surface.

**Resources** (12) — `cloudaxion_vm`, `cloudaxion_private_network`, `cloudaxion_block_volume`,
`cloudaxion_volume_attachment`, `cloudaxion_firewall`, `cloudaxion_firewall_attachment`,
`cloudaxion_floating_ip`, `cloudaxion_floating_ip_assignment`, `cloudaxion_load_balancer`,
`cloudaxion_ssh_key`, `cloudaxion_bucket`, `cloudaxion_s3_credentials`.

**Data sources** (5) — `cloudaxion_locations`, `cloudaxion_vm_images`, `cloudaxion_host_pools`,
`cloudaxion_billing_accounts`, `cloudaxion_s3_endpoint`.

Every resource supports create, read, update, delete and import.

**Examples** — including [`examples/rke2-cluster`](examples/rke2-cluster), a self-managed RKE2
cluster with a gVisor sandbox node pool, verified end to end against the live API on RKE2 v1.31.4.

The module registers the `runsc` runtime in **both** containerd configuration formats. RKE2 ships
containerd 2.0 from v1.31.6 / v1.32.2 onwards, where a custom runtime moves from
`[plugins."io.containerd.grpc.v1.cri"…]` in `config.toml.tmpl` to
`[plugins.'io.containerd.cri.v1.runtime'…]` in `config-v3.toml.tmpl`. Writing both is safe in
either direction — containerd 2.x prefers the v3 template, containerd 1.7 ignores it — and avoids
branching on a version the module cannot reliably read, since `rke2_version` accepts a channel name.
The v3 path renders correctly but has not yet been observed on a live containerd 2.x node.

### Notes on the API

CloudAxion publishes no OpenAPI specification, and its documentation diverges from its behaviour in
ways that would each have produced a broken provider. All of them are recorded in
[`docs/api-notes.md`](docs/api-notes.md); the ones most likely to affect you:

- **"Not found" is often HTTP 400, not 404.** Deleting a VM or network outside Terraform is detected
  correctly rather than failing the plan.
- **`username` and a cloud-init `users:` block are mutually exclusive.** Sending both leaves the
  guest with no working login at all. The provider omits `username` and `password` when the
  cloud-init document defines its own users.
- **There is no NAT gateway.** A VM without a public address has no outbound route, not even DNS.
- **`reserve_public_ip` leaks a floating IP on destroy**, for VMs and load balancers alike, and an
  unassigned address bills at a higher rate than an attached one. Manage addresses with
  `cloudaxion_floating_ip` where lifecycle matters.
- **Block storage endpoints are form-encoded**, despite the documentation showing JSON.
- **Firewall rules come back in a different order than they were sent**, and `port_end` is
  normalised to `port_start` when omitted.
- **Write operations are synchronous and slow** — the smallest VM blocks 33 seconds on create.
  Use the `timeouts` block rather than assuming a hang.

### Known limitations

- **Object storage is implemented but untested against a live service.** CloudAxion has not
  provisioned the service on the account used for development: `GET /v1/storage/user/keys` answers
  `404 {"message":"Storage account not found."}` and `GET /v1/storage/api/s3` returns `{"url": "/"}`
  rather than an endpoint. `cloudaxion_bucket`, `cloudaxion_s3_credentials` and
  `cloudaxion_s3_endpoint` are written from the published documentation and exercised against
  `httptest` fakes only. See [`docs/api-notes.md`](docs/api-notes.md) §22.
- **Managed services** (PostgreSQL packages) are not yet implemented — planned for v0.2.
- A VM's password is never returned by the API, so importing a VM whose configuration sets
  `password` will show a replacement. Prefer `public_keys`.
