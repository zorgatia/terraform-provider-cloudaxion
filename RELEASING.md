# Releasing

How to cut a release and get it into a registry. Both registries want the same artifacts, so a
single tag serves both.

> **Released versions are immutable.** Registries pin the checksums at publish time. Never retag,
> amend or re-upload a version that has been published — cut a new patch instead.

## One-time setup

### 1. The signing key

Both registries require every release to be signed, and the signature must be a **detached binary
GPG signature over the checksum file**, made with an **RSA or DSA** key. An ECC key is rejected —
which matters because `gpg --full-generate-key` now defaults to ECC.

Generate it yourself. This is a publishing identity: it needs a passphrase only you know, and the
private key must never enter this repository, a CI log, or a chat window.

```bash
gpg --full-generate-key --expert
```

Choose **RSA and RSA**, **4096** bits, and a real expiry (2 years is a reasonable default —
an expiring key forces a deliberate renewal rather than an indefinite one). Use a name and an
address that will still make sense to whoever inherits this.

Then note the key id and export the public half:

```bash
gpg --list-secret-keys --keyid-format=long
gpg --armor --export <KEY_ID> > cloudaxion-provider-signing-key.pub.asc
```

The public file is safe to share; it is what the registry stores.

### 2. CI secrets

The release workflow signs on GitHub Actions, so it needs the private key there. Export it and
paste it into the repository's **Settings → Secrets and variables → Actions**:

```bash
gpg --armor --export-secret-keys <KEY_ID>
```

| Secret | Contents |
|---|---|
| `GPG_PRIVATE_KEY` | the ASCII-armored private key from the command above |
| `PASSPHRASE` | the passphrase protecting it |

Paste directly into the GitHub UI. Do not write either to a file, a shell command, or anywhere with
a history.

### 3. Registry enrolment

**OpenTofu Registry** (the primary target — the platform runs OpenTofu):

1. Open an issue on [`opentofu/registry`](https://github.com/opentofu/registry/issues/new/choose)
   using the *Submit Provider* template. Namespace `zorgatia`, repository
   `terraform-provider-cloudaxion`.
2. Submit the GPG **public** key through the *Submit GPG Key* template for the same namespace.

The provider is published under a personal account. If it later moves to a Neoledge
organisation, that is a **new namespace and a new provider address** — registry namespaces cannot
be renamed. Everything already published under `zorgatia/cloudaxion` stays there, and existing
configurations keep resolving. Moving means republishing under the new namespace and asking
consumers to change their `source`. Worth settling before this has real dependants.

If the repo does move to an organisation, make your membership of it **public** — the registry
checks that before accepting a submission.

**Terraform Registry** (optional, same artifacts):

1. Sign in to <https://registry.terraform.io> with a GitHub account that can administer the repo.
2. **Publish → Provider**, pick the repository.
3. Add the GPG public key under *Signing Keys* for the namespace.

Both require the repository to be **public**, named `terraform-provider-cloudaxion`, all lowercase.

## Cutting a release

```bash
go test ./...
go generate ./...            # docs/ must be current — CI fails if it is not
git diff --exit-code docs/  # must be empty -- docs/ is generated and committed
                            # (not `git status`: with core.autocrlf on Windows it reports
                            #  a modification whose diff is empty, and cries wolf)

git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag triggers [`.github/workflows/release.yml`](.github/workflows/release.yml), which runs
GoReleaser to build every platform, write `SHA256SUMS`, sign it, and open a **draft** release.

Check the draft has all of:

- one `.zip` per platform, named `terraform-provider-cloudaxion_<version>_<os>_<arch>.zip`
- `terraform-provider-cloudaxion_<version>_SHA256SUMS` — and it must list **13** files: the twelve
  archives *and* the manifest
- `terraform-provider-cloudaxion_<version>_SHA256SUMS.sig`
- `terraform-provider-cloudaxion_<version>_manifest.json`

Then publish it. The registry webhook picks it up from there.

## Verifying before you tag

Build the artifacts locally without publishing or signing:

```bash
goreleaser release --snapshot --clean --skip=publish,sign
ls dist/
```

**Check the manifest is inside `SHA256SUMS`, not merely beside it.** The registry rejects a version
whose checksum file does not cover the manifest — with `missing SHA256 checksum for
[..._manifest.json]`, an error that appears only *after* publishing, on the registry side. It costs
a patch release to fix, so check it here:

```bash
grep -c manifest.json dist/*SHA256SUMS   # must be 1
```

The two `extra_files` blocks in `.goreleaser.yml` are both required and must carry the same
`name_template`: the one under `checksum:` puts the manifest in the checksum file, the one under
`release:` attaches it to the GitHub release. Having only the second is what produced that error.

To check the provider actually loads, install it into a filesystem mirror and point OpenTofu at it —
this is how the provider was verified before any registry existed:

```bash
go build -o terraform-provider-cloudaxion
DEST=~/.terraform.d/plugins/registry.opentofu.org/zorgatia/cloudaxion/0.1.0/linux_amd64
mkdir -p "$DEST" && cp terraform-provider-cloudaxion "$DEST/"
```

`~/.terraformrc`:

```hcl
provider_installation {
  filesystem_mirror {
    path    = "/home/you/.terraform.d/plugins"
    include = ["registry.opentofu.org/zorgatia/cloudaxion"]
  }
  direct {
    exclude = ["registry.opentofu.org/zorgatia/cloudaxion"]
  }
}
```

Then `tofu init` resolves the provider from disk while still fetching everything else normally.

## Version policy

Semantic versioning, and `v0.x` means the schema may still change:

- **patch** — bug fixes, documentation, no schema change
- **minor** — new resources or attributes; anything that changes an existing attribute's meaning
  while still on `v0.x`
- **major** — once at `v1.0.0`, any breaking schema change

State migration is not free. Before renaming or removing an attribute, check whether it can be
deprecated for a release first.
