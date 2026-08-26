# CLAUDE.md — `terraform-provider-cloudaxion`

Guidance for Claude Code when working in this repository.

## What this repo is

A **Terraform / OpenTofu provider for CloudAxion** — the Tunisian cloud operated by DataXion
(Tier IV datacenter, Tunis). It lets OpenTofu manage CloudAxion infrastructure declaratively.

**Why it exists.** Neoledge is standing up an *isolated Tunisian cell* for the cloud edition of
Elise Automate (decision **A1** in `Neoledge.Elise.Automate/docs/cloud-migration/`). That cell needs
IaC below the Kubernetes layer, and CloudAxion exposes a proprietary REST API with no existing
provider. Without this repo the recorded fallback was *"bootstrap the VMs outside Terraform (Ansible
or manual) and start the IaC at the Kubernetes layer"* — see **B11.1** in
`07-decisions-and-inputs-needed.md:335`. This provider replaces that fallback.

`03-target-architecture.md:464` already names the deliverable: the `30-services/automate` layer is
*"instantiated per cell (Scaleway provider / **Cloudaxion provider**)"*.

## Scope — read this before adding a resource

This is a **pure IaaS provider**. It models what the CloudAxion API actually exposes, nothing more.

**In scope:** VMs, block volumes, private networks, floating IPs, firewalls, L4 load balancers,
SSH keys, object-storage buckets + S3 credentials, managed service packages (PostgreSQL).

**Deliberately out of scope — do not add these, they do not exist in the API:**

| Not offered by CloudAxion | Where it goes instead |
|---|---|
| Managed Kubernetes | self-managed RKE2/k3s, via an OpenTofu **module** on top of `cloudaxion_vm` |
| Secret manager | OpenBao/Vault or sealed-secrets, in-cluster |
| DNS zones/records | the zone stays at OVH/Cloudflare; cert-manager DNS-01 against those |
| Container registry | Harbor/Zot in-cluster, or a public registry / pull-through mirror |
| Message queue | **not needed** — Elise Automate uses a Postgres-backed transport (decision A5) |
| Observability platform | Prometheus/Grafana/Loki, in-cluster |
| IAM applications/policies | CloudAxion has personal API tokens only |

**Also out of scope:** billing accounts, invoices, credit cards, pricing, usage. They are account
operations, not infrastructure — they do not belong in a state file.

A composite `cloudaxion_k8s_cluster` that SSHes into VMs to bootstrap RKE2 was considered and
rejected: it is configuration management wearing a provider costume, it fights `plan`, and it is not
publishable. If someone asks for it, propose the module instead.

## The API — what you must know before writing client code

Base `https://api.cloudaxion.net/v1/`, location-scoped `/v1/{slug}/…`. Auth is an **`apikey` header**.
Public docs: <https://api.cloudaxion.net/> (Slate, ~770 KB, one page).

**There is no OpenAPI/Swagger spec.** `/openapi.json`, `/swagger.json`, `/docs` all 404. The client
is hand-written. `docs/api-notes.md` is the recorded contract — **keep it current**; it is the only
reference that exists.

Seven traps, all verified against the published docs:

1. **Mixed content types.** VMs, disks and most endpoints take
   `application/x-www-form-urlencoded`; firewalls, load balancers, SSH keys and service packages take
   JSON. The client supports both, chosen per endpoint. Never assume.
2. **Identity is not always in the path.** `GET`/`PATCH`/`DELETE /v1/{slug}/user-resource/vm` take
   the uuid as a **query or form parameter**. Model each request explicitly.
3. **No async task API.** No `/tasks`, no `/jobs`. Resources carry `status` (`running`, `stopped`,
   `active`). Every create/delete polls — use the helpers in `internal/client/poll.go` and expose a
   `timeouts` block.
4. **No pagination.** List endpoints return whole arrays.
5. **Errors** are `{"errors": {"Error": "…"}}`. Map to diagnostics in `internal/client/errors.go`;
   never surface raw JSON to the user.
6. **`billing_account_id` is required on create** for VMs, disks, firewalls, load balancers and
   service packages. It is a provider-level setting with a per-resource override.
7. **Object storage is two APIs.** Bucket lifecycle is CloudAxion's API. ACL, versioning, SSE and
   policies are the **S3 API** — use the `aws` provider against the endpoint from
   `GET /v1/storage/api/s3`. Do not reimplement S3 here.

## Layout

```
main.go                    provider server (plugin protocol 6)
internal/provider/         provider schema, Configure, resource/datasource registration
internal/client/           hand-written Go SDK — client.go, errors.go, poll.go, one file per API area
internal/resources/        one file per resource + _test.go
internal/datasources/      one file per data source
examples/                  runnable examples; also tfplugindocs input
docs/                      tfplugindocs OUTPUT — generated, committed, do not hand-edit
                           (exception: docs/api-notes.md is hand-written)
templates/                 tfplugindocs templates
```

## Conventions

- **Docs in English** — the repo is public and registry-facing. (Sibling Neoledge repos use French
  for architecture docs; that does not apply here.) Explanations to the PO stay in French.
- **terraform-plugin-framework** (not SDKv2), protocol 6.
- Every resource implements create/read/update/delete **and `ImportState`**. Import IDs are
  `location/uuid` for location-scoped resources, plain `uuid` otherwise.
- Unit tests use `httptest` fakes and need no credentials. Acceptance tests are gated behind
  `TF_ACC=1` and need `CLOUDAXION_API_KEY`.
- **Never commit an API key.** Config comes from `CLOUDAXION_API_KEY`, `CLOUDAXION_LOCATION`,
  `CLOUDAXION_BILLING_ACCOUNT_ID`.
- Released versions are immutable — never retag or amend a published release; the registry pins
  checksums.
- **Provider address is `registry.opentofu.org/zorgatia/cloudaxion`**, Go module
  `github.com/zorgatia/terraform-provider-cloudaxion`, published from
  <https://github.com/zorgatia/terraform-provider-cloudaxion>. Registry namespaces cannot be
  renamed, so moving to a Neoledge organisation later means a new address and a consumer migration.
- Release process is in [`RELEASING.md`](RELEASING.md). Never put private key material in this
  repository, a shell command, or a chat window.

## Commands

```bash
go build ./...
go test ./...                                    # unit, no credentials needed
TF_ACC=1 go test ./internal/... -v -timeout 120m # acceptance, needs CLOUDAXION_API_KEY
go generate ./...                                # tfplugindocs -> docs/
```

Local iteration uses `dev_overrides` — see README. `tofu init` is *not* run under `dev_overrides`.

## Related repositories

| Repo | Relevance |
|---|---|
| `C:\git\Neoledge.Elise.Automate` | the workload this cell hosts; `docs/cloud-migration/` is the design authority (A1, A5, A6, B3, B11) |
| `C:\git\neo-sce-prod` | the Scaleway cell — the 4-layer OpenTofu pattern this cell mirrors (ADR 0005), and the naming/tagging conventions (ADR 0004) |
