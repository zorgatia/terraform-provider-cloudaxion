# Changelog

All notable changes to this provider are recorded here.
This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.4] - 2026-09-04

### Fixed

- **`examples/rke2-cluster` produced a kubeconfig that could not connect.** `kubeconfig_command`
  rewrites the server URL to the bootstrap node's public address, but the API server certificate
  never carried it — `tls_sans` was empty — so `kubectl` failed with `x509: certificate is valid
  for 127.0.0.1, ::1, 10.2.220.3, 10.43.0.1, not <public address>`.

  The cause was a dependency the module could not express. `cloudaxion_floating_ip.node` was keyed
  on a map of node name to **VM id**, so every address depended on its VM — and a server's
  cloud-init could not carry its own address without forming a cycle. Addresses are now keyed on
  node **names**, derived from `server_count` and `agent_pools` alone, so they are created before
  the VMs; every server then receives every server public address in `tls-san`.

  Attachment order is unchanged: the address is still assigned after the VM boots, so the
  wait-for-connectivity loop in the cloud-init remains necessary (note 19).

  Adopting this replaces the servers — `cloud_init` is `RequiresReplace`. Cheapest on an empty
  cluster.

## [0.1.3] - 2026-09-04

### Fixed

- **`examples/rke2-cluster` no longer leaks the ingress address on destroy.** The load balancer was
  created with `reserve_public_ip = true`, while every node in the same module uses
  `reserve_public_ip = false` plus an explicitly managed `cloudaxion_floating_ip` — for the reason
  the module's own comment gives, and which note 19 confirms applies to load balancers as well: a
  reserved address is **not** released when its resource is destroyed, and goes on billing at the
  higher unassigned rate.

  The load balancer now follows the same pattern, with `cloudaxion_floating_ip.ingress` attached
  through `resource_type = "load_balancer"`. `ingress_address` reads from that address.

  **Worth adopting before the address is published in DNS.** `reserve_public_ip` is
  `RequiresReplace`, so switching later replaces the load balancer and changes its address. Managed
  as its own resource, the address now outlives the load balancer: a cluster can be destroyed and
  rebuilt while the DNS records stay valid.

  Consumers pin this module by git ref, so this reaches them only when they bump `ref=` — no
  provider behaviour changed.

## [0.1.2] - 2026-09-04

### Fixed

- **The `by-id` disk path documented on `cloudaxion_volume_attachment` was wrong.** The resource
  description, the `device` attribute and the example all pointed at
  `/dev/disk/by-id/virtio-<volume_id>` — "which is what CloudAxion guarantees". That path never
  exists. udev builds the link from the virtio-blk *serial* field, which is capped at 20 bytes, so
  the 36-character volume UUID is truncated. The correct interpolation is
  `substr(cloudaxion_block_volume.data.id, 0, 20)`.

  Nothing fails at apply time — the VM is created, the volume is attached, `device` comes back as
  `vdb`, and the plan reports success. The mistake only surfaces inside the guest, when `mkfs` or
  `mount` runs against a path that is not there, which makes it expensive to diagnose.

  Measured on `tun01` on 2026-09-04 and recorded as note 23 in `docs/api-notes.md`. No behaviour
  changes: this is guidance carried in schema descriptions, the example and the generated docs.

## [0.1.1] - 2026-08-28

### Fixed

- **The release manifest is now covered by `SHA256SUMS`.** `.goreleaser.yml` attached
  `terraform-registry-manifest.json` to the GitHub release but never checksummed it, so both
  registries refused the version with `missing SHA256 checksum for [..._manifest.json]` — an error
  that surfaces only after publishing. `checksum.extra_files` now mirrors `release.extra_files`, and
  the checksum file lists thirteen entries instead of twelve.

  **0.1.0 is unusable and should be ignored.** It is not withdrawn, because published versions are
  immutable; it simply never passed registry ingestion, so nothing can have consumed it.

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
