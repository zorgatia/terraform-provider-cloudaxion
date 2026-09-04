# RKE2 on CloudAxion, with a gVisor sandbox pool

A self-managed Kubernetes cluster built from CloudAxion VMs, including a tainted node pool that
runs untrusted workloads under [gVisor](https://gvisor.dev/).

CloudAxion has no managed Kubernetes. That is usually a drawback; here it is the point. Because the
nodes are yours, the **container runtime is yours too** — installing `runsc` and declaring a
`RuntimeClass` is an installation step rather than a request to a platform team.

## Verified

Applied against the live API on 2026-08-26 and torn down again. One server and two agents:

```
NAME                   STATUS   ROLES                       VERSION
tfacc-rke2-general-1   Ready    <none>                      v1.31.4+rke2r1
tfacc-rke2-sandbox-1   Ready    <none>                      v1.31.4+rke2r1
tfacc-rke2-server-1    Ready    control-plane,etcd,master   v1.31.4+rke2r1

NAME     HANDLER
gvisor   runsc
```

A pod requesting `runtimeClassName: gvisor` landed on the sandbox node and reported:

```
[   0.000000] Starting gVisor...
Linux sandbox-proof 4.19.0-gvisor #1 SMP x86_64 GNU/Linux
```

That kernel string is the proof: the container is running on gVisor's own kernel, not the host's.

## The containerd config version

Registering `runsc` means extending RKE2's containerd configuration, and the plugin path for a
custom runtime **changed with containerd 2.0**:

| containerd | template file | plugin path |
|---|---|---|
| 1.7 | `config.toml.tmpl` | `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]` |
| 2.x | `config-v3.toml.tmpl` | `[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.'runsc']` |

RKE2 ships containerd 2.0 from **v1.31.6 / v1.32.2** onwards, so a single module has to work on
both sides of that line. It cannot decide from `rke2_version`: that variable accepts a channel name
as readily as a version, and channels move.

So the module writes **both** files. The selection is safe in both directions and documented
upstream — containerd 2.x prefers `config-v3.toml.tmpl` and falls back to the legacy file when it is
absent, while containerd 1.7 does not support v3 configuration and ignores that file entirely.

Both templates start with `{{ template "base" . }}`. That is not cosmetic: a template that does not
extend `base` renders a configuration missing `root`, `state` and `address`, and containerd then
fails to start.

> ⚠️ The end-to-end proof above was produced on RKE2 v1.31.4, i.e. **containerd 1.7**. The v3
> template is written from the documented contract and renders correctly, but it has not yet been
> observed registering `runsc` on a live containerd 2.x node. Until it has, treat `uname -r` inside
> a sandboxed pod as the acceptance test on any cluster running RKE2 v1.31.6 or newer.

## Use

```bash
cp terraform.tfvars.example terraform.tfvars   # then edit it
export CLOUDAXION_API_KEY="…"
tofu init
tofu apply
```

Fetch the kubeconfig with the command the module prints:

```bash
tofu output -raw kubeconfig_command
```

RKE2 writes its kubeconfig to `/etc/rancher/rke2/rke2.yaml` with `127.0.0.1` as the server, so the
command copies it over SSH and rewrites the address. CloudAxion has no metadata or console API to
read it out any other way.

The rewrite verifies because every server carries every server's public address in its API
certificate's `tls-san`. That is why the node addresses are keyed on node *names* rather than VM
ids: an address keyed on a VM id would depend on the VM whose cloud-init needs it.

Scheduling onto the sandbox pool needs both the RuntimeClass and a toleration:

```yaml
spec:
  runtimeClassName: gvisor
  tolerations:
    - key: sandbox
      operator: Equal
      value: gvisor
      effect: NoSchedule
```

## What the platform makes you deal with

These are measured properties of CloudAxion, not guesses. Each one shapes the module.

### There is no NAT gateway

A node without a public address has **no outbound route at all** — not even DNS:

```
curl https://get.rke2.io  ->  could not resolve host
ping 1.1.1.1              ->  100% packet loss
```

A default route and a resolver are handed out by DHCP and neither works. So every node needs its own
public address, and `node_public_ips` defaults to `true`.

Two consequences worth escalating:

- **Egress is a list of addresses, not one address.** A customer allow-list has to contain every
  node, and it changes when the cluster scales. If Elise Automate's decision A6 wants a single
  stable egress, that needs either a NAT node you build and operate, or a managed egress gateway
  from DataXion. Worth asking them for.
- **Cost scales with node count**, since each node holds an address.

### Addresses are attached after the node boots

The module manages each node's address explicitly rather than using the VM's own
`reserve_public_ip`, because an address created that way is **not released when the VM is
destroyed** — it survives and bills at the higher unassigned rate.

The trade-off is ordering: an explicitly managed address is attached *after* cloud-init starts, so
the node may boot with no route out. The bootstrap therefore waits for connectivity before
installing anything. Without that wait the installer's `curl` fails silently and the node comes up
with no RKE2 at all, which is exactly what happened on the first run.

### The load balancer is layer 4 only

No HTTP mode, no TLS termination, no health checks. Terminate TLS in-cluster with Envoy Gateway or
similar and point `ingress_ports` at the ingress controller's node ports. Health checking is the
cluster's job.

### The private network's address range is allocated, not chosen

There is no IPAM and no way to request a CIDR. The module reads the subnet back and derives the
firewall's intra-cluster rules from it, so nothing hardcodes a range.

## Shape

| Resource | Count | Notes |
|---|---|---|
| `cloudaxion_private_network` | 1 | subnet allocated by CloudAxion |
| `cloudaxion_firewall` (+ attachments) | 1 + one per node | RKE2 port set, scoped to the allocated subnet |
| `cloudaxion_vm` server | `server_count` | 1 for development, 3 for production — etcd wants an odd quorum |
| `cloudaxion_vm` agent | sum of `agent_pools[*].count` | `gvisor = true` installs `runsc` and taints the pool |
| `cloudaxion_floating_ip` (+ assignments) | one per node | explicit, so `destroy` releases them |
| `cloudaxion_load_balancer` | 0 or 1 | targets non-sandbox agents only |

The bootstrap server is a separate resource from the joining servers. Terraform forbids a resource
referring to itself, and the dependency is real: the others cannot start until the first owns the
etcd cluster.

## Limits

- **Not production-ready as written.** No etcd backup, no upgrade path, no monitoring. It proves the
  platform supports the architecture; hardening is the next piece of work.
- **Convergence is not represented in state.** Terraform knows a VM exists, not that RKE2 joined.
  A successful apply is not a healthy cluster — check `kubectl get nodes`.
- **The join token lives in state.** Anyone holding it can add a node. Treat state as a secret.
- **`allowed_ssh_cidrs` defaults to empty**, which means no inbound SSH and therefore no way to
  fetch the kubeconfig. Set it to your own range — not `0.0.0.0/0`.
- **No registry mirror configuration.** The module writes no `registries.yaml`, so nodes always pull
  from the registry named in the image reference. A private registry reached through a load balancer
  therefore depends on that load balancer supporting hairpin traffic.
