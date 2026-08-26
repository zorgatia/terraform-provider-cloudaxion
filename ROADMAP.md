# Roadmap

Status legend: ✅ done · 🟠 in progress · ⬜ not started · 🔴 blocked on an external input

## Where this fits

This provider is the bottom layer of the **Tunisian cell** for the cloud edition of Elise Automate.
The cell mirrors the Scaleway cell (`neo-sce-prod`) but self-hosts everything CloudAxion does not
offer as a managed service.

```
┌──────────────────────────────────────────────────────────────┐
│ ArgoCD — workloads (automate-webhook, worker, runner)        │
├──────────────────────────────────────────────────────────────┤
│ Helm / OpenTofu 30-services — namespaces, quotas, netpol     │
├──────────────────────────────────────────────────────────────┤
│ In-cluster platform — RKE2, CloudNativePG, MinIO, Keycloak,  │
│ OpenBao, Prometheus/Grafana/Loki, cert-manager, Envoy        │
├──────────────────────────────────────────────────────────────┤
│ THIS PROVIDER — VMs, disks, networks, floating IPs,          │
│ firewalls, L4 load balancers, buckets                        │
├──────────────────────────────────────────────────────────────┤
│ CloudAxion (DataXion, Tier IV, Tunis)                        │
└──────────────────────────────────────────────────────────────┘
```

## M0 — Foundations ✅

Go 1.26.7 and OpenTofu 1.12.5 installed; repository skeleton, `go.mod`, MPL-2.0 licence,
`terraform-registry-manifest.json` (protocol 6), and the three project documents.

## M1 — Client layer ✅

A hand-written Go SDK for the CloudAxion REST API, in `internal/client/`.

- `apikey` header auth, configurable endpoint and location
- **form-encoded and JSON** request bodies, chosen per endpoint
- error mapping from `{"errors":{"Error":"…"}}` to typed errors, with
  `IsNotFound` / `IsUnauthorized` / `IsConflict` classifiers
- retry with exponential backoff on 5xx, 429 and transport failures; 4xx never retried
- `poll.go` — wait-for-status helpers, since the API has no task or job endpoint
- tolerant decoding for fields whose element type the documentation leaves unspecified

Covers VMs, block storage, private networks, floating IPs, firewalls, load balancers, buckets,
S3 keys, SSH keys, locations, images, VM parameters, host pools and billing accounts.

**34 unit tests, no credentials required.** They pin the API's traps: form-vs-JSON bodies, the
uuid travelling as a query parameter, the network name as a query parameter, rule normalisation,
and the load-balancer rule uuid read-back.

Deliverable: **`docs/api-notes.md`** — the recorded API contract, including seven endpoint paths
that differ from what the documentation's section titles imply.

## M2 — v0.1.0 resources ✅

**Ten resources and four data sources, verified against the live API** in a single apply:
plan → apply → idempotent re-plan → in-place update → import round-trip → destroy, with the
production estate digest-checked before and after.

Four provider bugs were found by that run and fixed: block-storage endpoints being form-encoded
rather than JSON, the firewall normalising `port_end`, and `network_uuid` / `reserve_public_ip`
being unrecoverable on import.

The landing zone — everything needed to stand up RKE2 and run Elise Automate.

| Resource | API |
|---|---|
| `cloudaxion_ssh_key` | `/v1/user-resource/ssh_keys` |
| `cloudaxion_vm` | `/v1/{slug}/user-resource/vm` (+ `/start`, `/stop`) |
| `cloudaxion_block_volume` | `/v1/{slug}/storage/disks` |
| `cloudaxion_volume_attachment` | `/vm/storage/attach` · `/detach` |
| `cloudaxion_private_network` | `/v1/{slug}/network/network` |
| `cloudaxion_floating_ip` (+ `_assignment`) | `/v1/{slug}/network/ip_addresses` |
| `cloudaxion_firewall` (+ `_attachment`) | `/v1/{slug}/network/firewalls` |
| `cloudaxion_load_balancer` (+ `_rule`, `_target`) | `/v1/{slug}/network/load_balancers` |
| `cloudaxion_bucket` · `cloudaxion_s3_credentials` | `/v1/storage/…` |

Data sources: `locations`, `vm_images`, `vm_parameters`, `host_pools`, `s3_endpoint`,
`billing_accounts`, plus lookups for `vm`, `private_network`, `floating_ip`.

Every resource: create / read / update / delete / **import**, `timeouts`, drift detection.

## M3 — Documentation and release pipeline ✅

`tfplugindocs` generation wired to `go generate`, runnable examples for every resource,
`.goreleaser.yml`, and GitHub Actions for test and release. See [`RELEASING.md`](RELEASING.md).

Verified by a real GoReleaser run: **12 platform archives**, each containing only the correctly
named plugin binary, plus the `SHA256SUMS` file. The versioned protocol manifest is attached as an
`extra_files` entry — GoReleaser does not know about it, and without it a release uploads cleanly
and the registry still rejects it.

Installation from a **local filesystem mirror** is verified: it is how the RKE2 example was
exercised before any registry existed.

CI is green on the published repository — build, unit tests, and a job that fails the build if
generated docs are stale.

`v0.1.0` is tagged locally and **deliberately not pushed**: the tag triggers the release workflow,
which needs the signing key in place first.

## M4 — Publication 🟠

Done:

- ✅ Public repository at <https://github.com/zorgatia/terraform-provider-cloudaxion>, MPL-2.0,
  pushed and CI green. Scanned before publishing: no API token, no production hostnames, no real
  addresses or account identifiers.
- ✅ Provider address fixed as `registry.opentofu.org/zorgatia/cloudaxion`, and the Go module path
  moved to match so `go get` resolves.

Remaining — these need your signing identity and your GitHub account, so they are yours to do:

1. Generate the RSA-4096 signing key and add `GPG_PRIVATE_KEY` and `PASSPHRASE` to the repository
   secrets ([`RELEASING.md`](RELEASING.md) §1–2).
2. `git push origin v0.1.0` — the tag is already created locally.
3. Check the draft release has 12 archives, `SHA256SUMS`, `SHA256SUMS.sig` and the manifest,
   then publish it.
4. Submit to the OpenTofu Registry, and optionally the Terraform Registry.

> **The namespace is permanent.** Registry namespaces cannot be renamed. Publishing under a personal
> account is fine for now, but if this becomes infrastructure Neoledge depends on, moving it to an
> organisation later means a new provider address and a migration for every consumer. Worth settling
> before it has real dependants.

## M5 — Prove it end to end ✅

[`examples/rke2-cluster/`](examples/rke2-cluster/) — an OpenTofu module that builds a working
cluster on CloudAxion, **verified end to end against the live API on 2026-08-26**:

private network → firewall (RKE2 port set) → control-plane and worker VMs bootstrapped by
cloud-init → RKE2 v1.31.4 → managed floating IPs → L4 load balancer over the ingress node ports.

Measured result: three nodes `Ready`, a `gvisor` RuntimeClass mapped to `runsc`, a tainted sandbox
pool, and a pod scheduled onto it reporting

```
[   0.000000] Starting gVisor...
Linux sandbox-proof 4.19.0-gvisor #1 SMP x86_64 GNU/Linux
```

— the container running on gVisor's own kernel rather than the host's.

> **This closes input B3** of the Elise Automate programme. The gVisor node pool is the
> critical-path blocker on Scaleway, where it has *"no precedent on the platform"*. On
> self-managed nodes it is an installation step, not a vendor request.

Five further API behaviours were found by this run and are recorded in
[`docs/api-notes.md`](docs/api-notes.md) — most consequentially that `username` and a cloud-init
`users:` block together produce an unreachable machine, and that there is no NAT gateway at all.

## M6 — The Tunisian cell ⬜ (separate repository)

Out of scope for this repo, listed so the dependency chain is visible: the `30-services/automate`
OpenTofu layer, Helm and ArgoCD definitions, CloudNativePG or the managed PostgreSQL package, MinIO,
Keycloak, observability, and the container image build/push CI stage that does not yet exist in
`Neoledge.Elise.Automate`.

---

## Open items

| # | Item | Effect | Handling |
|---|---|---|---|
| 1 | ~~No API token~~ ✅ **resolved 2026-08-26** | — | Verified against a live account. Thirteen corrections recorded in `docs/api-notes.md`, three of which were provider bugs: not-found arriving as HTTP 400, `vlan_id` never being returned, and a 60 s HTTP timeout too short for synchronous VM creation |
| 2 | No OpenAPI spec upstream | The client can silently drift when CloudAxion changes | `docs/api-notes.md` + acceptance tests as the regression net |
| 3 | Managed PostgreSQL may not grant `CREATE SCHEMA` / `CREATE ROLE` | `tenantctl onboard` needs it for schema-per-tenant | Fall back to CloudNativePG in-cluster — already the recorded working assumption for this cell |
| 4 | S3 conditional-write support unknown | OpenTofu `use_lockfile` state locking may not work | Test early; otherwise single-writer CI locking, as `neo-sce-prod` already enforces |
| 5 | Publishing requires a **public** GitHub repo | Neoledge code lives on Azure DevOps | The provider carries no Neoledge business logic; a filesystem mirror unblocks all work until this is decided |
| 6 | No DNS API at CloudAxion | Wildcard `*.automate.<domain>` and DNS-01 TLS | Keep the zone at OVH/Cloudflare; cert-manager has webhooks for both |
| 7 | Self-managed Kubernetes is an operational load | Upgrades, etcd, backups | Accepted (B11.2) — confirm the Tunisia team's scope is the whole stack, not just the VMs |
| 8 | Only PostgreSQL is confirmed among managed services | v0.2 may be thinner than hoped | Enumerate the real service types with a token before committing |
