# Terraform / OpenTofu provider for CloudAxion

Manage [CloudAxion](https://dataxion.com/cloudaxion/) infrastructure — the Tunisian cloud operated by
DataXion — declaratively with OpenTofu or Terraform.

> **Status: pre-release (v0.x).** The API surface is being implemented milestone by milestone.
> See [ROADMAP.md](ROADMAP.md).

## What it manages

| Resource | Purpose |
|---|---|
| `cloudaxion_vm` | virtual machines (vCPU / RAM / disk, cloud-init, SSH keys) |
| `cloudaxion_block_volume` · `cloudaxion_volume_attachment` | block disks and their attachment |
| `cloudaxion_private_network` | VLAN-backed private networks |
| `cloudaxion_floating_ip` · `cloudaxion_floating_ip_assignment` | static public IPs |
| `cloudaxion_firewall` · `cloudaxion_firewall_attachment` | stateful L4 firewalls |
| `cloudaxion_load_balancer` (+ `_rule`, `_target`) | L4 network load balancers |
| `cloudaxion_ssh_key` | account SSH public keys |
| `cloudaxion_bucket` · `cloudaxion_s3_credentials` | S3-compatible object storage |
| `cloudaxion_managed_service` | managed service packages (PostgreSQL) — *v0.2* |

Data sources: `locations`, `vm_images`, `vm_parameters`, `host_pools`, `s3_endpoint`,
`billing_accounts`, plus lookups for existing VMs, networks and floating IPs.

**Not included, because CloudAxion does not offer them:** managed Kubernetes, DNS, secret manager,
container registry, message queues, observability. See [CLAUDE.md](CLAUDE.md) for where each of those
belongs instead — in short, they are self-hosted on top of the compute this provider creates.

## Usage

```hcl
terraform {
  required_providers {
    cloudaxion = {
      source  = "zorgatia/cloudaxion"
      version = "~> 0.1"
    }
  }
}

provider "cloudaxion" {
  # api_key            = "..."  # prefer CLOUDAXION_API_KEY
  location             = "tun1" # or CLOUDAXION_LOCATION
  billing_account_id   = 12     # or CLOUDAXION_BILLING_ACCOUNT_ID
}

resource "cloudaxion_private_network" "core" {
  name = "core" # subnet and VLAN are allocated by CloudAxion
}

resource "cloudaxion_vm" "node" {
  name       = "k8s-node-1"
  os_name    = "ubuntu"   # see data.cloudaxion_vm_images
  os_version = "24.04"
  vcpu       = 4
  ram        = 8192       # MB
  disks      = 80         # GB, boot disk

  network_uuid = cloudaxion_private_network.core.id
  public_keys  = [file("~/.ssh/id_ed25519.pub")]
  cloud_init   = file("${path.module}/cloud-init.yaml")
}
```

### Credentials

Create an API token in the CloudAxion user interface, then:

```bash
export CLOUDAXION_API_KEY="your-token"
export CLOUDAXION_LOCATION="tun1"
export CLOUDAXION_BILLING_ACCOUNT_ID="12"
```

The token is sent as an `apikey` header. Never commit it.

## Installing

### From a registry (after v0.1.0 is published)

Declare the `required_providers` block above and run `tofu init`.

### From a local filesystem mirror (works today)

```bash
go build -o terraform-provider-cloudaxion
DEST=~/.terraform.d/plugins/registry.opentofu.org/zorgatia/cloudaxion/0.1.0/linux_amd64
mkdir -p "$DEST" && cp terraform-provider-cloudaxion "$DEST/"
```

On Windows the path is `%APPDATA%\terraform.d\plugins\...\windows_amd64\`.

## Developing

Requires **Go 1.24+** and **OpenTofu 1.9+** (or Terraform 1.8+).

```bash
go build ./...
go test ./...        # unit tests, no credentials needed
go generate ./...    # regenerate docs/ with tfplugindocs
```

For local iteration without publishing, add a `dev_overrides` block to `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides { "zorgatia/cloudaxion" = "/absolute/path/to/this/repo" }
  direct {}
}
```

With `dev_overrides` active, skip `tofu init` and run `tofu plan` directly.

Acceptance tests create and destroy **real, billable** infrastructure:

```bash
TF_ACC=1 CLOUDAXION_API_KEY=... go test ./internal/... -v -timeout 120m
```

## Documentation

- [`docs/`](docs/) — generated resource and data-source reference
- [`docs/api-notes.md`](docs/api-notes.md) — the recorded CloudAxion API contract. CloudAxion
  publishes no OpenAPI spec, so this file is the reference the client is written against.
- [`examples/`](examples/) — runnable configurations, including a full RKE2 cluster
- [`CLAUDE.md`](CLAUDE.md) — architecture, scope boundaries and API traps
- [`ROADMAP.md`](ROADMAP.md) — milestones and status

Upstream API documentation: <https://api.cloudaxion.net/>

## Contributing

Issues and pull requests are welcome. Before adding a resource, read the scope section of
[CLAUDE.md](CLAUDE.md) — several capabilities are intentionally absent.

## License

[MPL-2.0](LICENSE). This is a community provider; it is not affiliated with or endorsed by DataXion.
