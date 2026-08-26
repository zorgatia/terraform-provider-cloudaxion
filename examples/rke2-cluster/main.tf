# A self-managed RKE2 Kubernetes cluster on CloudAxion, including a gVisor
# sandbox node pool.
#
# CloudAxion has no managed Kubernetes, so the cluster is built from plain VMs
# and bootstrapped by cloud-init. That is not only a workaround: because the
# nodes are yours, the container runtime is yours too, which is what makes a
# gVisor RuntimeClass possible without asking the platform for anything.

locals {
  # gVisor pools get a label the RuntimeClass selects on, and a taint so nothing
  # else drifts onto sandbox capacity.
  agent_pools = {
    for name, pool in var.agent_pools : name => merge(pool, {
      labels = merge(
        pool.labels,
        { "neoledge.com/agent-pool" = name },
        pool.gvisor ? { "neoledge.com/sandbox" = "true" } : {},
      )
      taints = concat(
        pool.taints,
        pool.gvisor ? ["sandbox=gvisor:NoSchedule"] : [],
      )
    })
  }

  # Flatten the pools into one node per element so a single for_each drives all
  # the agent VMs.
  agent_nodes = merge([
    for pool_name, pool in local.agent_pools : {
      for index in range(pool.count) :
      "${pool_name}-${index + 1}" => merge(pool, { pool_name = pool_name })
    }
  ]...)

  any_gvisor = anytrue([for pool in var.agent_pools : pool.gvisor])

  # Pinning is opt-in: an unset version tracks the installer's stable channel,
  # which is fine for a scratch cluster and wrong for a durable one.
  rke2_version_env = var.rke2_version == null ? "" : "INSTALL_RKE2_VERSION=${var.rke2_version}"

  # Every server node, bootstrap and joiners together, so downstream resources
  # can treat the control plane as one collection.
  servers = concat(
    [cloudaxion_vm.server_init],
    [for vm in cloudaxion_vm.server_join : vm],
  )

  # Where to fetch the kubeconfig from: the bootstrap server's public address
  # when it has one, otherwise its private address for use from inside.
  kubeconfig_host = var.node_public_ips ? cloudaxion_floating_ip.node["server-1"].address : cloudaxion_vm.server_init.private_ipv4

  # Every node, keyed for the floating-IP fan-out.
  all_nodes = merge(
    { for index, vm in local.servers : "server-${index + 1}" => vm.id },
    { for key, vm in cloudaxion_vm.agent : key => vm.id },
  )
}

# The cluster join token. Held in state and marked sensitive; anyone with it can
# add a node, so treat state as a secret.
resource "random_password" "token" {
  length  = 48
  special = false
}

resource "cloudaxion_private_network" "this" {
  name     = var.name
  location = var.location
}

# ---------------------------------------------------------------- firewall
#
# Ports come from the RKE2 requirements. Node-to-node traffic is allowed within
# the network's own subnet, which CloudAxion allocates, so the rules reference
# it rather than a hardcoded range.

resource "cloudaxion_firewall" "this" {
  name               = "${var.name}-nodes"
  description        = "RKE2 cluster ${var.name}"
  location           = var.location
  billing_account_id = var.billing_account_id

  # Kubernetes API.
  dynamic "rule" {
    for_each = length(var.allowed_api_cidrs) > 0 ? [1] : []
    content {
      protocol           = "tcp"
      direction          = "inbound"
      port_start         = 6443
      endpoint_spec_type = "ip_prefixes"
      endpoint_spec      = var.allowed_api_cidrs
    }
  }

  dynamic "rule" {
    for_each = length(var.allowed_ssh_cidrs) > 0 ? [1] : []
    content {
      protocol           = "tcp"
      direction          = "inbound"
      port_start         = 22
      endpoint_spec_type = "ip_prefixes"
      endpoint_spec      = var.allowed_ssh_cidrs
    }
  }

  # Intra-cluster: API, supervisor, etcd, kubelet, CNI health and NodePorts.
  dynamic "rule" {
    for_each = toset([6443, 9345, 2379, 2380, 10250, 9099])
    content {
      protocol           = "tcp"
      direction          = "inbound"
      port_start         = rule.value
      endpoint_spec_type = "ip_prefixes"
      endpoint_spec      = [cloudaxion_private_network.this.subnet]
    }
  }

  # Canal/Flannel VXLAN overlay.
  rule {
    protocol           = "udp"
    direction          = "inbound"
    port_start         = 8472
    endpoint_spec_type = "ip_prefixes"
    endpoint_spec      = [cloudaxion_private_network.this.subnet]
  }

  # NodePort range, so the load balancer can reach the ingress controller.
  rule {
    protocol           = "tcp"
    direction          = "inbound"
    port_start         = 30000
    port_end           = 32767
    endpoint_spec_type = "ip_prefixes"
    endpoint_spec      = [cloudaxion_private_network.this.subnet]
  }

  # Nodes must reach the internet to install RKE2 and pull images.
  rule {
    protocol           = "tcp"
    direction          = "outbound"
    endpoint_spec_type = "any"
  }

  rule {
    protocol           = "udp"
    direction          = "outbound"
    endpoint_spec_type = "any"
  }
}

# ----------------------------------------------------------------- servers
#
# The first server initialises etcd; the others join it. Terraform cannot
# express "wait until RKE2 is converged", only "the VM exists", so the join is
# ordered with depends_on and RKE2's own retry loop does the rest.

resource "cloudaxion_vm" "server_init" {
  name       = "${var.name}-server-1"
  location   = var.location
  os_name    = var.os_name
  os_version = var.os_version

  vcpu    = var.server.vcpu
  ram     = var.server.ram
  disk_gb = var.server.disk_gb

  network_uuid       = cloudaxion_private_network.this.id
  billing_account_id = var.billing_account_id
  reserve_public_ip  = false

  username    = var.ssh_username
  public_keys = var.ssh_public_keys

  cloud_init = templatefile("${path.module}/templates/server.yaml.tftpl", {
    ssh_username        = var.ssh_username
    ssh_public_keys     = var.ssh_public_keys
    token               = random_password.token.result
    bootstrap           = true
    first_server_ip     = ""
    tls_sans            = []
    rke2_version_env    = local.rke2_version_env
    gvisor_runtimeclass = local.any_gvisor
  })

  # Creation blocks until the guest is running: 33 seconds for the smallest
  # possible VM, longer for these.
  timeouts {
    create = "45m"
  }
}

# Additional control plane nodes. Kept separate from the bootstrap node because
# Terraform forbids a resource referring to itself, and because the dependency
# is real: these cannot start until the first server owns the etcd cluster.
resource "cloudaxion_vm" "server_join" {
  count = var.server_count - 1

  name       = "${var.name}-server-${count.index + 2}"
  location   = var.location
  os_name    = var.os_name
  os_version = var.os_version

  vcpu    = var.server.vcpu
  ram     = var.server.ram
  disk_gb = var.server.disk_gb

  network_uuid       = cloudaxion_private_network.this.id
  billing_account_id = var.billing_account_id
  reserve_public_ip  = false

  username    = var.ssh_username
  public_keys = var.ssh_public_keys

  cloud_init = templatefile("${path.module}/templates/server.yaml.tftpl", {
    ssh_username        = var.ssh_username
    ssh_public_keys     = var.ssh_public_keys
    token               = random_password.token.result
    bootstrap           = false
    first_server_ip     = cloudaxion_vm.server_init.private_ipv4
    tls_sans            = [cloudaxion_vm.server_init.private_ipv4]
    rke2_version_env    = local.rke2_version_env
    gvisor_runtimeclass = local.any_gvisor
  })

  timeouts {
    create = "45m"
  }
}

resource "cloudaxion_firewall_attachment" "server" {
  count = var.server_count

  firewall_id = cloudaxion_firewall.this.id
  vm_id       = local.servers[count.index].id
  location    = var.location
}

# ------------------------------------------------------------------ agents

resource "cloudaxion_vm" "agent" {
  for_each = local.agent_nodes

  name       = "${var.name}-${each.key}"
  location   = var.location
  os_name    = var.os_name
  os_version = var.os_version

  vcpu    = each.value.vcpu
  ram     = each.value.ram
  disk_gb = each.value.disk_gb

  network_uuid       = cloudaxion_private_network.this.id
  billing_account_id = var.billing_account_id
  reserve_public_ip  = false

  username    = var.ssh_username
  public_keys = var.ssh_public_keys

  cloud_init = templatefile("${path.module}/templates/agent.yaml.tftpl", {
    ssh_username     = var.ssh_username
    ssh_public_keys  = var.ssh_public_keys
    token            = random_password.token.result
    first_server_ip  = cloudaxion_vm.server_init.private_ipv4
    node_labels      = each.value.labels
    node_taints      = each.value.taints
    gvisor           = each.value.gvisor
    rke2_version_env = local.rke2_version_env
  })

  timeouts {
    create = "45m"
  }

  depends_on = [cloudaxion_vm.server_init]
}

resource "cloudaxion_firewall_attachment" "agent" {
  for_each = local.agent_nodes

  firewall_id = cloudaxion_firewall.this.id
  vm_id       = cloudaxion_vm.agent[each.key].id
  location    = var.location
}

# ----------------------------------------------------------------- ingress
#
# Layer 4 only. TLS terminates in-cluster; this publishes node ports.
# gVisor pools are deliberately excluded: sandbox capacity runs tenant code and
# should not be serving ingress.

resource "cloudaxion_load_balancer" "ingress" {
  count = var.create_load_balancer ? 1 : 0

  name               = "${var.name}-ingress"
  location           = var.location
  network_id         = cloudaxion_private_network.this.id
  billing_account_id = var.billing_account_id
  reserve_public_ip  = true

  dynamic "rule" {
    for_each = var.ingress_ports
    content {
      source_port = tonumber(rule.key)
      target_port = rule.value
    }
  }

  dynamic "target" {
    for_each = {
      for key, node in local.agent_nodes : key => node if !node.gvisor
    }
    content {
      id = cloudaxion_vm.agent[target.key].id
    }
  }
}

# ----------------------------------------------------------- node addresses
#
# Every node gets an explicitly managed floating IP rather than the VM's own
# `reserve_public_ip`, for two measured reasons:
#
#   1. CloudAxion has **no NAT gateway**. A node with no public address has no
#      outbound route at all — not even DNS — so it cannot install RKE2 or pull
#      images. Verified: ICMP and HTTPS to 1.1.1.1 both fail from such a node.
#   2. An address created by `reserve_public_ip` is a floating IP that is **not
#      released when the VM is destroyed**. It survives and keeps billing at the
#      higher unassigned rate. Managed here, `tofu destroy` releases it.

resource "cloudaxion_floating_ip" "node" {
  for_each = var.node_public_ips ? local.all_nodes : {}

  name               = "${var.name}-${each.key}"
  location           = var.location
  billing_account_id = var.billing_account_id
}

resource "cloudaxion_floating_ip_assignment" "node" {
  for_each = var.node_public_ips ? local.all_nodes : {}

  address     = cloudaxion_floating_ip.node[each.key].address
  resource_id = each.value
  location    = var.location
}
