"""Read-only probe of a real CloudAxion account.

Answers the verification checklist in docs/api-notes.md, which exists because
CloudAxion publishes no OpenAPI specification: the client is written from prose
documentation and needs checking against a live account before its schemas are
frozen.

Every call is a GET. Nothing is created, changed or deleted, so running this
costs nothing.

The token is read from a file and never printed. Endpoints that return personal
data or credentials are either skipped or field-filtered — see SAFE_FIELDS.

Usage:
    python hack/probe-api/probe.py [--token-file PATH] [--json OUT]
"""

import argparse
import json
import os
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("CLOUDAXION_ENDPOINT", "https://api.cloudaxion.net/v1")

# Billing payloads carry addresses, tax identifiers and contact details. Only
# what the provider actually needs is shown.
SAFE_FIELDS = {
    "billing_accounts": ("id", "name", "is_default", "currency", "type"),
}


def get(path, token):
    """Perform one GET, returning (status, parsed_body_or_text)."""
    req = urllib.request.Request(BASE + path, headers={
        "apikey": token,
        "Accept": "application/json",
        "User-Agent": "cloudaxion-provider-probe",
    })
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode("utf-8", "replace")
            try:
                return resp.status, json.loads(raw)
            except json.JSONDecodeError:
                return resp.status, raw
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        try:
            return e.code, json.loads(raw)
        except json.JSONDecodeError:
            return e.code, raw
    except Exception as e:  # network failure, DNS, TLS
        return 0, str(e)


def pick(rows, fields):
    """Keep only the named fields from each row."""
    return [{k: r.get(k) for k in fields} for r in rows if isinstance(r, dict)]


def show(title, status, body, limit=2500):
    print("\n" + "=" * 72)
    print(f"{title}   [HTTP {status}]")
    print("=" * 72)
    text = body if isinstance(body, str) else json.dumps(body, indent=2, ensure_ascii=False)
    print(text[:limit] + ("\n… truncated" if len(text) > limit else ""))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--token-file",
                    default=os.path.expanduser("~/.cloudaxion/token"))
    ap.add_argument("--json", help="write the collected findings to this file")
    args = ap.parse_args()

    try:
        with open(args.token_file, encoding="utf-8") as f:
            token = f.read().strip()
    except OSError as e:
        sys.exit(f"cannot read the token file: {e}")
    if not token:
        sys.exit(f"{args.token_file} is empty")

    findings = {}

    # 1. Locations — the real slugs, and which one is default.
    status, locations = get("/config/locations", token)
    findings["locations"] = locations
    show("1. locations", status, locations)

    if status == 401:
        sys.exit("\nThe token was rejected (401). Check the file contents.")
    if status != 200 or not isinstance(locations, list):
        sys.exit("\nCould not list locations; stopping.")

    slugs = [l.get("slug") for l in locations if isinstance(l, dict)]
    default_slug = next(
        (l["slug"] for l in locations if isinstance(l, dict) and l.get("is_default")),
        slugs[0] if slugs else "",
    )
    print(f"\n-> slugs: {slugs}   default: {default_slug!r}")

    # 2. VM creation constraints — this is what tells us the cheapest possible VM.
    status, params = get("/api/parameters/vm", token)
    findings["vm_parameters"] = params
    show("2. VM parameters (constraints)", status, params, limit=4000)

    if isinstance(params, list):
        floor = {}
        for p in params:
            if isinstance(p, dict) and p.get("min") is not None:
                floor[p.get("parameter")] = p.get("min")
        if floor:
            print(f"\n-> documented minimums: {floor}")

    # 3. Images.
    status, images = get("/config/vm_images", token)
    findings["vm_images"] = images
    if isinstance(images, list):
        summary = [
            {"os_name": i.get("os_name"),
             "versions": [v.get("os_version") for v in i.get("versions", [])
                          if v.get("published")]}
            for i in images if isinstance(i, dict)
        ]
        show("3. VM images (published versions)", status, summary, limit=2000)
    else:
        show("3. VM images", status, images)

    # 4. Billing accounts — field-filtered, they carry contact details.
    status, accounts = get("/payment/billing_account/list", token)
    if isinstance(accounts, list):
        accounts = pick(accounts, SAFE_FIELDS["billing_accounts"])
    findings["billing_accounts"] = accounts
    show("4. billing accounts (filtered)", status, accounts)

    # 5. Pricing — read this BEFORE creating anything billable.
    status, pricing = get("/pricing/policy", token)
    findings["pricing"] = pricing
    show("5. pricing policy", status, pricing, limit=3000)

    # 6-9. Location-scoped inventory. Read-only; existing resources are listed
    # so a test never collides with something real.
    for slug in slugs:
        prefix = f"/{slug}" if slug else ""

        status, pools = get(f"{prefix}/user-resource/host_pool/list", token)
        findings[f"host_pools.{slug}"] = pools
        show(f"6. host pools [{slug}]", status, pools, limit=1500)

        status, networks = get(f"{prefix}/network/networks", token)
        findings[f"networks.{slug}"] = networks
        show(f"7. existing private networks [{slug}]", status, networks, limit=1500)

        status, vms = get(f"{prefix}/user-resource/vm/list", token)
        findings[f"vms.{slug}"] = vms
        if isinstance(vms, list):
            summary = [{"uuid": v.get("uuid"), "name": v.get("name"),
                        "status": v.get("status"), "vcpu": v.get("vcpu"),
                        "memory": v.get("memory")}
                       for v in vms if isinstance(v, dict)]
            show(f"8. existing VMs [{slug}] — DO NOT TOUCH", status, summary, limit=1500)
        else:
            show(f"8. existing VMs [{slug}]", status, vms, limit=800)

    # 10. Object storage endpoint.
    status, s3 = get("/storage/api/s3", token)
    findings["s3"] = s3
    show("10. S3 endpoint", status, s3, limit=800)

    if args.json:
        with open(args.json, "w", encoding="utf-8") as f:
            json.dump(findings, f, indent=2, ensure_ascii=False)
        print(f"\nfindings written to {args.json}")


if __name__ == "__main__":
    main()
