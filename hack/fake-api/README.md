# Local fake CloudAxion API

A stand-in for the CloudAxion API, enough to drive a real `tofu plan`/`apply`/
`import`/`destroy` cycle against the provider **without a live account or a
billable resource**.

It reproduces the quirks recorded in [`docs/api-notes.md`](../../docs/api-notes.md):
`apikey` header auth, location-scoped paths, private-network creation taking its
name as a *query* parameter, and CloudAxion allocating the VLAN and subnet itself.

It is a development aid, not a simulator. It does not validate inputs the way the
real API does, so a configuration that works here can still be rejected upstream.
Acceptance tests against the real API remain the source of truth.

## Use

```bash
python hack/fake-api/fake_api.py   # listens on 127.0.0.1:8099
```

Build the provider and point OpenTofu at it with `dev_overrides`:

```bash
go build -o terraform-provider-cloudaxion .
```

`dev.tfrc`:

```hcl
provider_installation {
  dev_overrides { "zorgatia/cloudaxion" = "/absolute/path/to/this/repo" }
  direct {}
}
```

`main.tf`:

```hcl
provider "cloudaxion" {
  api_key            = "fake-token"
  endpoint           = "http://127.0.0.1:8099/v1"
  location           = "tun1"
  billing_account_id = 12
}

data "cloudaxion_locations" "all" {}

resource "cloudaxion_private_network" "core" {
  name = "core"
}
```

Then, with `dev_overrides` active, skip `init` and run `plan` directly:

```bash
TF_CLI_CONFIG_FILE=dev.tfrc tofu plan
```

## Coverage

Implemented: `GET /v1/config/locations`, and full CRUD for
`/v1/{slug}/network/network`.

Everything else answers 404. Extend it alongside each new resource so the
lifecycle stays verifiable offline.
