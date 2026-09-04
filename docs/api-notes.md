# CloudAxion API — recorded contract

CloudAxion publishes **no OpenAPI/Swagger specification**. `/openapi.json`, `/swagger.json`,
`/v1/openapi.json`, `/docs` and `/v1/swagger/index.html` all return 404. The published documentation
is a single Slate HTML page at <https://api.cloudaxion.net/> (~770 KB).

**This file is the contract the Go client is written against.** Keep it current — nothing else records it.

- Source: <https://api.cloudaxion.net/>, read 2026-08-26.
- Status: **partially verified against a live account on 2026-08-26.** Sections marked ✅ were checked
  with real read-only calls. Entries marked ⚠️ remain unverified. Write paths (create/update/delete)
  are still unverified.

## ✅ Verified corrections — the documentation is wrong in ways that matter

Checked against a live account. Each of these would have produced a broken provider.

### 1. "Not found" is often HTTP 400, not 404

| Request | Real status | Real body |
|---|---|---|
| `GET /vm?uuid=<unknown>` | **400** | `{"errors": {"Error": "No such virtual machine exists: …"}}` |
| `GET /network/network/<unknown>` | **400** | `{"message": "Network UUID is invalid."}` |
| `GET /storage/disks/<unknown>` | 404 | `{"message": "Disk not found"}` |
| `GET /network/ip_addresses/<unreserved>` | 404 | `{"message": "Ip address … was not found."}` |

This is the single most consequential finding. Terraform's `Read` must tell "gone" from "broken":
gone drops the resource from state so the next plan rebuilds it, broken must fail loudly. A
status-code-only check would make `tofu plan` fail outright whenever anything was deleted outside
Terraform — the most common drift there is. See `internal/client/notfound.go`.

### 2. There are two error shapes, not one

The documented `{"errors": {"Error": "…"}}` is used by VM endpoints. Network, disk, floating-IP,
routing and auth endpoints use an **undocumented** `{"message": "…"}`. Validation failures use
`{"errors": {"<field>": "…"}}`.

### 3. An unknown location slug answers 404 like a missing resource

`GET /v1/<bad-slug>/network/networks` returns 404 `{"message":"no route and no API found with those
values"}`. Treating that as "gone" would silently erase real resources from state over a typo, so
routing errors are classified separately.

### 4. Auth failures split across two codes

No `apikey` header → **401**. Invalid token → **403** `{"message":"Invalid authentication credentials"}`.

### 5. VM sizing limits live on the host pool, not in the parameters endpoint

`GET /v1/api/parameters/vm` returns **no `vcpu`, `ram` or `disks` entries at all** — the documented
range constraints are absent. The real limits come from `host_pool/list` under `guest_limits`:

```json
"guest_limits": {"cpu": {"min": 1, "max": 56}, "ram_mb": {"min": 512, "max": 131072},
                 "disk_gb": {"min": 20, "max": 1024}}
```

Limits differ per location (cpu max 56 in `tun01`, 16 in `tun02`). Validation must read the pool.

### 6. Real parameter constraints

`os_name`, `os_version`, `username`, `password` and `public_key` are all **`mandatory: false`**, not
mandatory as documented. `os_name`/`os_version` carry `ignore_for: ["disk_uuid", "source_uuid"]`.
`name` has **no regex** on the live API. `public_key` does:
`^(ssh-(rsa|dsa|ecdsa|ed25519)|ecdsa-sha2-nistp[0-9]+) [A-Za-z0-9+/]+[=]{0,3}(\s.*)?$`

The `os_name` enum is far wider than documented: `almalinux, centos, debian, fedora, opensuse, rhel,
rocky, ubuntu, windows` plus an application catalogue (`cpanel, cyberpanel, docker, magento,
mailcoach, mikrotik, moodle, nodejs, pfsense, plesk, waf2py-debian12, wordpress`) and `_custom`.

### 7. Locations, host pools and pricing (this account)

Slugs are **`tun01`** (default, `is_preferred`) and **`tun02`**, both `country_code: "tun"`.
Locations also carry an undocumented `is_published`. Host pools carry undocumented `guest_limits`,
`storage_pool_uuid`, `is_visible` and `ui_position`.

`GET /v1/pricing/policy` (hourly, per unit):

| Resource | Price/hour |
|---|---|
| CPU (per vCPU) | 0.013699 |
| RAM (per 512 MB) | 0.016438 |
| Storage (per GiB, main and block) | 0.000685 |
| Backup / snapshot (per GiB) | 0.000548 |
| Unassigned floating IP | 0.012329 |
| Load balancer | 0.054795 |

A minimum VM (1 vCPU, 512 MB, 20 GB) therefore costs about **0.044 per hour**.

### 8. `vlan_id` is never returned

The documentation shows `vlan_id` on every private-network payload. The live API **omits the field
entirely** — verified against nine networks in `tun01`, including one created through the provider.
`subnet_ipv6` comes back as `""`. Both are modelled as nullable so state cannot report a VLAN of `0`,
which would read as a real one.

Verified network payload keys: `uuid, name, type, subnet, subnet_ipv6, is_default, vm_uuids,
resources_count, created_at, updated_at`.

### 9. Verified write path: private networks ✅

Full lifecycle exercised against the live API in `tun01` on 2026-08-26 — create, read, idempotent
re-plan, in-place rename, import by `location/uuid`, out-of-band-delete recovery, destroy. Creating a
network in an account that already has one leaves `is_default: false`, so existing VM placement is
untouched. `DELETE` answers **200**, not the documented 204.

### 10. Write operations are synchronous — there is no async model to poll

Measured against the live API on 2026-08-26 in `tun01`, creating the smallest possible VM
(1 vCPU, 512 MB, 20 GB, ubuntu 24.04):

| Operation | Blocking time | Returned status |
|---|---|---|
| `POST /vm` | **33 s** | `running` — fully provisioned |
| `POST /vm/stop` | **22 s** | `stopped` |
| `POST /vm/start` | **4 s** | `running` |
| `DELETE /vm` | 1 s | — |

**No intermediate state was ever observed.** There is no `provisioning`, `starting` or `stopping`;
`status` only ever holds `running` or `stopped`. The absence of a task endpoint is not an omission —
the API simply blocks until the work is done.

Two consequences. First, the HTTP client timeout is a hung-connection backstop, not an operation
budget: 60 s would have been too short, since a Windows VM with a large disk will take far longer
than 33 s. It is now 30 minutes, with real deadlines coming from the context Terraform supplies.
Second, the poll helpers in `poll.go` are a safety net, not the primary mechanism.

### 11. Verified VM payload

Real keys returned by `GET /vm?uuid=…`: `backup, billing_account, created_at, description,
designated_pool_name, designated_pool_uuid, hostname, mac, memory, name, os_name, os_version,
private_ipv4, status, storage, updated_at, user_id, username, uuid, vcpu`.

Absent despite appearing in the documentation: **`id`**, **`tags`**, and `license_type` on Linux
guests. With `reserve_public_ip=false` there is **no `public_ipv4` or `public_ipv6` key at all**
rather than an empty one. Storage entries carry `pool: "n/a"` and `type: "n/a"`, and no `id`.

Repeating `public_keys` works. `network_uuid` places the VM correctly — omitting it would put the
guest in the account's *default* network, which on a populated account means production.

### 12. Verified: SSH keys and firewalls

`POST /user-resource/ssh_keys` answers **200**, not the documented 201, and returns
`{uuid, name, public_key, user_id, created_at}` — there is **no `fingerprint`** field.

`POST /network/firewalls` behaves exactly as documented: rules echo back with server-assigned uuids
and `resources_assigned: []`. `DELETE` answers 204 — the only endpoint tested that actually does.

Delete status codes are inconsistent: firewall 204, VM 200, network 200, SSH key 200.

### 13. Block storage endpoints are form-encoded, not JSON

The documentation shows a JSON body for `POST /storage/disks`. The live API **never parses it**:
a JSON request carrying `billing_account_id` is still rejected with

```
400 {"message":"'billing_account_id' is required if using a global API token"}
```

The same request as `application/x-www-form-urlencoded` succeeds. `PATCH /storage/disks/{uuid}` is
the same. This was caught only by applying against the real API — the error message points at a
field that *was* present, which makes it badly misleading.

Verified disk payload: `{uuid, user_id, billing_account_id, status: "Active", size_gb,
source_image_type: "EMPTY", created_at, updated_at}`.

Note also that timestamps are **not consistent across endpoints**: disks return ISO-8601 with an
offset (`2026-08-26T12:06:48.608+0000`) while VMs return a space-separated form
(`2026-08-26 11:36:03`). Treat every timestamp as an opaque string.

### 14. The firewall normalises `port_end`

Sending a rule with `port_start: 22` and no `port_end` stores `port_end: 22`. A Terraform attribute
that is optional-but-server-filled has to be `Computed` as well as `Optional`, or the apply fails
with "provider produced inconsistent result after apply".

`endpoint_spec` comes back as `null` when `endpoint_spec_type` is `any`.

### 15. `resources_assigned` carries objects, not identifier strings

A firewall attached to a VM reports:

```json
"resources_assigned": [{"resource_type": "vm", "resource_uuid": "6c772f89-…"}]
```

The documentation only ever shows this empty, so the element type was unknowable from it. The
tolerant decoding in `jsonutil.go` handles this shape and several others.

### 16. Two fields are unrecoverable from a read, and both break import

- **A VM payload has no `network_uuid`.** Membership is only visible from the other side, in each
  network's `vm_uuids`, so the provider resolves it from the networks list when state has no value.
  Without that, importing a VM proposes replacing it.
- **A load balancer payload does not report `reserve_public_ip`**, only whether a public address
  exists. The provider infers the flag from the address.
- **A VM password is never returned at all**, and nothing can recover it. Importing a VM whose
  configuration sets `password` will always show a replacement; use `public_keys`, or
  `lifecycle { ignore_changes = [password] }`.

### 17. `username` and a cloud-init `users:` block are mutually exclusive

Sending `username` alongside a cloud-init document that declares its own `users`
leaves the guest with **no working login at all** — not the configured user, not the
default one.

Proven with three otherwise identical VMs on 2026-08-26:

| Request | Result |
|---|---|
| `cloud_init` only | SSH works |
| `cloud_init` + `username` | every key rejected |
| `cloud_init` + `username` + `backup` + `description` | every key rejected |

The documentation says only that cloud-init "will overwrite `username` and `password`",
which badly undersells it: the outcome is an unreachable machine. The client therefore
omits `username` and `password` whenever the cloud-init document defines users. The API
also stops echoing `username` in that case, so the configured value is kept in state
rather than being nulled.

### 18. There is no NAT gateway — a node without a public address is fully isolated

Measured from a VM created with `reserve_public_ip=false`, reached over a jump host:

```
curl https://get.rke2.io   -> could not resolve host
ping 1.1.1.1               -> 2 packets transmitted, 0 received, 100% loss
ip route                   -> default via 10.66.9.1  (present but dead)
```

A default route and a resolver are configured by DHCP, and neither works. So a private-only
node cannot install packages, pull images, or reach anything at all.

Two consequences for a Kubernetes cell:

- **Every node needs its own public address**, or a NAT node you build and route through
  yourself. CloudAxion offers no managed alternative.
- **Decision A6's "static egress IPs" becomes a list, not an address.** With one address per
  node, a customer allow-list has to contain all of them and changes whenever the cluster
  scales. Worth raising with DataXion: a managed NAT or egress gateway would collapse this
  back to one entry.

### 19. `reserve_public_ip` leaks a floating IP on destroy

Setting `reserve_public_ip=true` creates a floating IP and assigns it. Deleting the
resource releases the VM or load balancer but **leaves the address behind, unassigned** —
where it bills at the *higher* unassigned rate (0.012329/hour against nothing at all when
attached). Confirmed for both VMs and load balancers.

Manage addresses explicitly with `cloudaxion_floating_ip` and
`cloudaxion_floating_ip_assignment` so `destroy` releases them.

Note the ordering consequence: an explicitly managed address is attached *after* the VM
boots, so cloud-init may start with no route out. Bootstrap scripts must wait for
connectivity rather than assuming it — the RKE2 example does exactly that.

### 20. Firewall rules come back in a different order

The API returns rules in its own order, not the order they were sent. Terraform compares
list elements positionally, so writing the API's order into state fails the apply. Rules
here are genuinely unordered — every rule is evaluated, there is no precedence — so the
provider re-sorts the response to match the configured order.

### 21. Managed services: PostgreSQL 14.0 only, and it is nearly end-of-life

Verified 2026-08-27 by creating a real package and probing the rest.

**The catalogue is one item.** `postgresql` is the only service type; `mysql`, `mariadb`, `redis`,
`mongodb`, `rabbitmq`, `elasticsearch`, `kafka` and `minio` all answer
`Service '<name>' not found`. There is no endpoint that lists what is on offer — the only way to
enumerate is to guess a name and read the error.

**One version: `14.0`.** Every other string tested was rejected, including `13.0`, `15.0`, `16.0`,
`17.0`, `18.0` and point releases like `14.1`, `14.5`, `14.10`. Note the shape: bare `14` fails,
`14.0` succeeds.

⚠️ **PostgreSQL 14 reaches end-of-life on 12 November 2026**, and CloudAxion offers no newer
version and no visible upgrade path. Anything durable built on this managed service is on an
unsupported database within months of writing. Raise it with DataXion before committing.

**Creation is fast and synchronous** — the package came back `status: "active"` immediately, with a
VM and a virtual_ip already allocated.

**The admin user is not a superuser.** `wadmin` carries `createrole` and `createdb`, but not
`rolsuper` and not `rolreplication`.

**Schema-per-tenant works**, which is what decision A4 needs. The full sequence succeeds:
`CREATE DATABASE`, `CREATE ROLE`, `CREATE SCHEMA … AUTHORIZATION`, `GRANT USAGE, CREATE ON SCHEMA`,
`GRANT CONNECT ON DATABASE`, `ALTER ROLE … SET search_path`, `REVOKE ALL ON SCHEMA public FROM
PUBLIC`, `CREATE TABLE`, `CREATE EXTENSION pgcrypto`.

One wrinkle, and it is ordinary PostgreSQL rather than a platform limit: a non-superuser cannot
create a schema owned by a role it is not a member of. `CREATE SCHEMA … AUTHORIZATION tenant` fails
with `must be member of role "tenant"` unless preceded by `GRANT tenant TO wadmin`. Any onboarding
tool has to issue that grant first.

**Tenant isolation holds.** With two tenants provisioned, the second reading the first's schema is
refused with `permission denied for schema` — the same negative test Elise Automate's chantier 3
used to prove isolation on its own PostgreSQL.

**Reaching it.** The service is private-only: `properties.service_ip` is an address on the account's
**default network** (there is no documented way to place it elsewhere), and nothing public is bound
at creation. Two things make it reachable:

- `POST …/whitelist_addresses` with `{"ip_address": "<addr>"}` — note the field name; `ip`, `cidr`
  and `address` are all rejected with `IP address must be specified`.
- A floating IP assigned with `assigned_to_resource_type: "service"` and the **package** uuid as
  `assigned_to`. Port 5432 opens on it within seconds.

### 22. Object storage is not provisioned on this account

`GET /v1/storage/user/keys` → 404 `{"message":"Storage account not found."}`, while
`GET /v1/storage/bucket/list` → 200 `[]`. The bucket and S3-key resources cannot be exercised until
object storage is enabled. `GET /v1/storage/api/s3` returns `{"url": "/"}` rather than an endpoint.

### 23. The `by-id` disk path truncates the UUID to 20 characters

`/dev/disk/by-id/virtio-<volume_id>` — the path this provider's own documentation recommended until
2026-09-04 — **never exists**. udev builds that link from the virtio-blk *serial* field, which is
capped at **20 bytes**, so the 36-character volume UUID is cut short.

Measured on `tun01`, 2026-09-04, on a VM with a 20 GiB data volume attached:

| | |
|---|---|
| volume UUID | `42876465-0857-4cc7-966a-c183f80d06ef` |
| link actually created | `virtio-42876465-0857-4cc7-9` (20 characters of UUID) |
| boot disk, same rule | `virtio-11edc97a-60e6-4824-8` |

The correct interpolation is therefore:

```hcl
"/dev/disk/by-id/virtio-${substr(cloudaxion_block_volume.data.id, 0, 20)}"
```

**Why this one bites.** Nothing fails at apply time: the VM is created, the volume is attached,
`device` comes back as `vdb`, and OpenTofu reports success. The failure happens later and elsewhere —
`mkfs` inside cloud-init, against a path that does not exist — and the only visible symptom is a
service that never starts and a port that refuses connections.

Two volumes would have to share a 20-character prefix to collide, which is not a practical risk;
`device` remains the authoritative value if an exact match is ever needed.

## Basics

| | |
|---|---|
| Base URL | `https://api.cloudaxion.net/v1/` |
| Location-scoped | `https://api.cloudaxion.net/v1/{slug}/…` |
| Auth | header `apikey: <token>` — tokens are created in the CloudAxion web UI |
| Unauthenticated call | `401` with `application/json` (verified live) |
| Error body | `{"errors": {"Error": "…"}}` **or** `{"message": "…"}` — see verified correction 2 |
| Status codes seen | 200, 201, 204, 400 (incl. "not found"), 401, 403, 404, 409 |
| Pagination | **none** — list endpoints return whole arrays |
| Async operations | **no task/job endpoint** — poll the resource's `status` field |

### Locations

`GET /v1/config/locations` returns an array of
`{display_name, is_default, is_preferred, description, order_nr, slug, country_code}`.

Calls without a slug act on the `is_default` location. To target another, insert the slug directly
after the version: `/v1/{slug}/user-resource/vm`. **There is no endpoint that returns resources across
all locations** — each location must be queried separately.

Doc examples use placeholder slugs (`cyc01` "Cycletown", `bus02` "Busburg"); the real Tunisian slugs
must be read from the live endpoint.

Location-scoped: virtual machines, `billing_resources`, `resource_billing`, floating IPs, block
storage, private networks, firewalls, load balancers. Not location-scoped: user, tokens, SSH keys,
object storage, config/parameters, billing/payment, managed services.

## Content types — mixed, per endpoint

This is the single most error-prone part of the API.

| Body style | Endpoints |
|---|---|
| `application/x-www-form-urlencoded` | VM create/modify/delete and all `/vm/*` actions; attached-disk operations; token operations; profile |
| `application/json` | SSH keys, block storage disks, floating IPs, firewalls, load balancers, managed service packages |
| Query string only | private network create (`POST /network/network?name=…`), most reads |

## Identity is not always in the path

`GET`/`PATCH`/`DELETE /v1/{slug}/user-resource/vm` carry the VM `uuid` as a **query or form
parameter**, not a path segment. Floating IPs are addressed by their **IPv4 address**, not a UUID.
Model every request explicitly; do not infer REST shape from the resource name.

---

## Compute — VM

`POST /v1/{slug}/user-resource/vm` (form-encoded)

| Field | Type | Notes |
|---|---|---|
| `name` | string | required. ✅ The live API applies **no** regex, unlike the docs |
| `os_name` / `os_version` | string | **images are identified by this pair, not by an image id**; `_custom` / `_` for an empty disk |
| `disks` | int (GB) | boot disk size. ✅ Real limits come from the host pool's `guest_limits`, not from the parameters endpoint |
| `vcpu` | int | ✅ host pool `guest_limits.cpu` (1–56 in `tun01`, 1–16 in `tun02`) |
| `ram` | int (MB) | ✅ host pool `guest_limits.ram_mb` (512–131072) |
| `username` / `password` | string | password must contain lower + upper + digit, min 8 chars |
| `public_key` / `public_keys` | string / repeated | OpenSSH lines; `public_keys` repeats the parameter |
| `cloud_init` | JSON or YAML | overrides platform values; setting `users` overrides `username`/`password` |
| `network_uuid` | uuid | default network if omitted |
| `designated_pool_uuid` | uuid | server class — see `host_pool/list` |
| `billing_account_id` | int | required unless the token is restricted to one account |
| `reserve_public_ip` | bool | default **true** |
| `backup` | bool | default false |
| `source_uuid` + `source_replica` | uuid | create as a copy of a snapshot/backup |
| `disk_uuid` | uuid | boot from an existing unattached disk; `disks` then has no effect |
| `description` | string | |

Response and `GET …/vm?uuid=…`: `{uuid, id, name, status, vcpu, memory, hostname, mac, os_name,
os_version, private_ipv4, public_ipv6, username, description, backup, billing_account,
designated_pool_uuid, designated_pool_name, license_type, tags, storage[], created_at, updated_at,
user_id}`.

`storage[]` entries: `{uuid, id, name (e.g. "sda"), size, type, pool, primary, shared, replica[],
created_at}`.

- `status`: `running` | `stopped`. **This is the only progress signal** — poll it after create/start/stop.
- ⚠️ The response sample shows `public_ipv6` but no `public_ipv4` field. Confirm how a reserved public
  IPv4 surfaces on read — it determines the `cloudaxion_vm` attribute set.
- ⚠️ `tags` appears in responses (null in samples) but is not documented as a create parameter.

Other VM endpoints: `GET /vm/list`, `PATCH /vm` (name/vcpu/ram), `POST /vm/start`, `POST /vm/stop`,
`POST /vm/reinstall`, `POST /vm/clone`, `POST /vm/rebuild`, `POST|DELETE /vm/ip/public`,
`POST|GET|DELETE /vm/replica`, `POST /vm/backup`, `POST /vm/boot_iso_media`, `PATCH /vm/user`,
`GET /user-resource/host_pool/list`.

### Parameters and images

- `GET /v1/api/parameters/vm` — **not** `/v1/config/vm_parameters`. Returns machine-readable
  constraints: `{parameter, type, constraint: range|regexp|enum, mandatory, min, max, expression,
  values[], limited_by, limits[]}`. Worth surfacing as a data source and using for plan-time validation.
- `GET /v1/config/vm_images` returns `[{os_name, display_name, ui_position, is_default,
  is_app_catalog, icon, versions: [{os_version, display_name, published}]}]`; also
  `/vm_images/plain_os` and `/vm_images/app_catalog`.
- `GET /v1/config/boot_images` — **not** `/config/bootable_media_images`.

## Block storage

`POST /v1/{slug}/storage/disks` (JSON): `{size_gb, billing_account_id, display_name,
source_image_type: EMPTY|OS_BASE|DISK|SNAPSHOT, source_image}`.
`GET|PATCH|DELETE /v1/{slug}/storage/disks/{disk_uuid}`; `PATCH` takes
`{billing_account_id, display_name, read_only_bootable}`. Disk `status` seen: `Active` (⚠️ capitalised —
confirm the full value set).

Attach and detach are **VM** endpoints, form-encoded:
`POST /v1/{slug}/user-resource/vm/storage/attach` and `/detach`, both `{uuid (VM), storage_uuid (disk)}`.
Attach returns the device `name` (e.g. `vdb`); the docs note a `/dev/disk/by-id/virtio-` prefix.

`POST|PATCH|DELETE /v1/{slug}/user-resource/vm/storage` manages a disk inline on a VM — overlapping
functionality with `/storage/disks`. Prefer the standalone disk resource plus an attachment resource.

## Private networks

- `POST /v1/{slug}/network/network?name=MyNetwork` — **name is a query parameter**, no body.
- `GET /v1/{slug}/network/network/{uuid}` returns `{uuid, name, vlan_id, subnet, subnet_ipv6, type,
  is_default, vm_uuids[], resources_count, created_at, updated_at}`
- `GET /v1/{slug}/network/networks`, `DELETE …/network/{uuid}`,
  `PATCH …/network/{uuid}` with `{"name": "…"}`, `PUT …/network/{uuid}/default`

**`vlan_id` and `subnet` are allocated by CloudAxion and cannot be requested.** There is no IPAM or
CIDR control. The first network a user creates becomes the default. This constrains cluster network
planning: the RKE2 module must *read* the subnet rather than choose it.

## Floating IPs

Addressed by **IPv4 address**, not UUID.

- `POST /v1/{slug}/network/ip_addresses` (JSON) `{billing_account_id (required), name}`
- `GET|PATCH|DELETE /v1/{slug}/network/ip_addresses/{address}`; `GET …/ip_addresses` to list
- `POST …/{address}/assign` (JSON) `{assigned_to, assigned_to_resource_type}` where the type is
  `virtual_machine` | `service` | `load_balancer`
- `POST …/{address}/unassign`

Response: `{id, address, user_id, billing_account_id, type, network_id, name, enabled, is_deleted,
is_virtual, assigned_to, assigned_to_resource_type, assigned_to_private_ip, created_at, updated_at}`.

⚠️ The delete example URL is written `/v1/ip_addresses/{address}` without `/network/` — likely a docs
typo. Verify before relying on it.

## Firewalls

`POST /v1/{slug}/network/firewalls` (JSON) `{display_name, billing_account_id, rules[]}`.

`FirewallRule`: `{uuid, protocol ("tcp"|"udp"|"icmp"), direction ("inbound"|"outbound"),
port_start, port_end, endpoint_spec_type ("any"|"ip_prefixes"), endpoint_spec: [...]}`.

Documented validation: ports 1–65535; `port_start` may be `null` meaning **all ports**; `port_end`
null implies it equals `port_start`; `port_start` must not exceed `port_end`; `endpoint_spec` must be
valid IPs or CIDRs when the type is `ip_prefixes`.

`GET /firewalls`, `PUT /firewalls/{uuid}` (`name`, `description`, `rules[]` — a **full replace**),
`DELETE /firewalls/{uuid}` returns 204, `POST|DELETE /firewalls/{uuid}/vms?vm_uuid=…`.

⚠️ Create takes `display_name`; update takes `name`. Read responses show `display_name`. Confirm.

## Load balancers (L4)

`POST /v1/{slug}/network/load_balancers` (JSON):
`{display_name, billing_account_id, network_uuid, reserve_public_ip, rules[], targets[]}` where
`rules[] = {source_port, target_port}` and `targets[] = {target_uuid, target_type: "vm"}`.

Read returns `{uuid, display_name, network_uuid, user_id, billing_account_id, private_address,
is_deleted, forwarding_rules[], targets[], created_at, updated_at}`; `forwarding_rules[]` entries carry
`{uuid, protocol ("TCP"), source_port, target_port, settings:{connection_limit, session_persistence}}`
and `targets[]` carry `{target_uuid, target_type, target_ip_address, created_at}`.

Sub-resource paths — **these differ from what the section titles suggest**:

| Operation | Path |
|---|---|
| add target | `POST …/load_balancers/{uuid}/targets` |
| remove target | `DELETE …/load_balancers/{uuid}/targets/{target_uuid}` |
| add rule | `POST …/load_balancers/{uuid}/forwarding_rules` |
| remove rule | `DELETE …/load_balancers/{uuid}/forwarding_rules/{rule_uuid}` |
| change billing | `PATCH …/load_balancers/{uuid}/billing_account` |

- **TCP only.** Protocol, connection limit and session persistence appear in responses but are not
  documented as create inputs — no HTTP mode, no TLS termination, no health checks. TLS terminates
  in-cluster (Envoy Gateway); this load balancer is a plain L4 front door.
- **Rule creation does not return the rule `uuid`** (the response is only `{source_port, target_port}`),
  but deletion needs it — re-read the load balancer after creating a rule to learn its uuid.
- ⚠️ The create example sends `target_port: 80` and the response echoes `target_port: 8080`. One of the
  two is a docs error. Verify.

## Object storage

Two APIs. **Bucket lifecycle is CloudAxion's API; everything inside a bucket is the S3 API.**

- `GET /v1/storage/api/s3` returns the S3 endpoint to point an S3 client at
- `PUT /v1/storage/bucket` create, `GET /v1/storage/bucket?name=…`, `PATCH` (billing account),
  `DELETE`, `GET /v1/storage/bucket/list`
- `GET|POST|DELETE /v1/storage/user/keys` for S3 access key pairs

ACLs, versioning, SSE, object lock and bucket policies are **not** in this API — use the `aws` provider
against the S3 endpoint. ⚠️ Whether the S3 implementation supports **conditional writes** is unverified;
OpenTofu's `use_lockfile` state locking depends on it.

## Managed services (DBaaS)

`POST /v1/user-resource/service/package` (JSON) — **not** `/v1/managed-services/packages`:
`{billing_account_id, service, version, display_name, vm_cpu, vm_ram, vm_disk_gb, package_parameters
(JSON string, e.g. {"location":"dc1"}), is_multi_node}`.

Documented example: `"service": "postgresql", "version": "14.0"`.

Response: `{uuid, service, version, display_name, status ("active"), is_multi_node, is_deleted,
billing_account_id, user_id, properties: {service_ip, location, sql_user, port}, resources[]
(virtual_ip and vm entries with allocations), prices[], created_at, updated_at}`.

Also: `GET /service/packages`, `GET|PATCH|DELETE /service/package/{uuid}`,
`GET /service/package/{uuid}/secrets`,
`GET|POST|DELETE /service/package/{uuid}/whitelist_addresses`.

⚠️ **PostgreSQL is the only service type shown in the docs.** Enumerate the real list with a token
before committing v0.2 scope.
⚠️ **Verify that the provisioned PostgreSQL grants `CREATE SCHEMA`, `CREATE ROLE` and `GRANT`** from
the admin user. Elise Automate's `tenantctl` needs all three for schema-per-tenant onboarding. If they
are not granted, the cell uses CloudNativePG in-cluster instead.

## Deliberately not modelled

Billing accounts, invoices, credit cards, payments, pricing and usage
(`/v1/payment/*`, `/v1/credit/*`, `/v1/pricing/policy`, `/v1/charging/usage`) are account operations,
not infrastructure. The one exception is a **read-only** billing-accounts data source, because
`billing_account_id` is a required create field almost everywhere:
`GET /v1/payment/billing_account/list`.

## Endpoint paths corrected against the raw documentation

Several paths differ from what the navigation titles imply. Recorded so they are not "fixed" back:

| Wrong assumption | Actual |
|---|---|
| `/v1/config/vm_parameters` | `/v1/api/parameters/vm` |
| `/v1/config/bootable_media_images` | `/v1/config/boot_images` |
| `/v1/user-resource/billing_accounts` | `/v1/payment/billing_account/list` |
| `/v1/managed-services/packages` | `/v1/user-resource/service/package` |
| `…/load_balancers/{uuid}/rules` | `…/load_balancers/{uuid}/forwarding_rules` |
| `…/load_balancers/{uuid}/billing` | `…/load_balancers/{uuid}/billing_account` |
| `…/whitelist` | `…/whitelist_addresses` |

## Verification checklist (run once a token exists)

1. `GET /v1/config/locations` — real Tunisian slugs, which one is default
2. `GET /v1/api/parameters/vm` — real min/max, real `os_name` enum
3. `GET /v1/config/vm_images` — available images and versions
4. `GET /v1/payment/billing_account/list` — the account id to use
5. `GET /v1/{slug}/user-resource/host_pool/list` — server classes
6. Create and delete one VM by curl: record the `status` transitions and their timing
7. Trigger an error (bad name, missing field): record the exact error body and status code
8. Create a bucket and test S3 conditional writes (`If-None-Match`) for `use_lockfile`
9. Create a `postgresql` package: check `CREATE ROLE` / `CREATE SCHEMA` / `GRANT`
