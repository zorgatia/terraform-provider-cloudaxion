variable "name" {
  description = "Prefix for every resource this module creates."
  type        = string
  default     = "rke2"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.name))
    error_message = "Use lowercase letters, digits and hyphens, starting with a letter (2-21 characters)."
  }
}

variable "location" {
  description = "CloudAxion location slug. Defaults to the provider's location."
  type        = string
  default     = null
}

variable "billing_account_id" {
  description = "Billing account. Defaults to the provider's billing_account_id."
  type        = number
  default     = null
}

variable "rke2_version" {
  description = <<-EOT
    RKE2 version channel or exact version, for example "v1.31.4+rke2r1".
    Leave null to take the installer's stable channel — pin it for production.
  EOT
  type        = string
  default     = null
}

variable "os_name" {
  description = "Base image. RKE2 is tested here against Ubuntu."
  type        = string
  default     = "ubuntu"
}

variable "os_version" {
  description = "Base image version."
  type        = string
  default     = "24.04"
}

variable "ssh_public_keys" {
  description = <<-EOT
    OpenSSH public keys installed on every node.

    There is no other way in: CloudAxion has no serial console API, and the
    kubeconfig has to be fetched over SSH. Supply at least one key.
  EOT
  type        = list(string)

  validation {
    condition     = length(var.ssh_public_keys) > 0
    error_message = "At least one SSH public key is required — otherwise the cluster is unreachable."
  }
}

variable "ssh_username" {
  description = "Login user created on each node."
  type        = string
  default     = "ops"
}

variable "server_count" {
  description = <<-EOT
    Number of RKE2 server (control plane) nodes.

    Use 1 for development and 3 for production: etcd needs an odd number and
    tolerates one failure at 3. Two is worse than one.
  EOT
  type        = number
  default     = 1

  validation {
    condition     = contains([1, 3, 5], var.server_count)
    error_message = "etcd requires an odd quorum: use 1, 3 or 5."
  }
}

variable "server" {
  description = "Sizing for server nodes. Limits come from the host pool's guest_limits."
  type = object({
    vcpu    = number
    ram     = number # MB
    disk_gb = number
  })
  default = {
    vcpu    = 2
    ram     = 4096
    disk_gb = 40
  }
}

variable "agent_pools" {
  description = <<-EOT
    Worker pools, keyed by name.

    Set `gvisor = true` to install the gVisor runtime on the pool and taint it,
    so only workloads that ask for the `gvisor` RuntimeClass land there. That is
    the sandbox boundary for untrusted tenant code.
  EOT
  type = map(object({
    count   = number
    vcpu    = number
    ram     = number # MB
    disk_gb = number
    gvisor  = optional(bool, false)
    labels  = optional(map(string), {})
    taints  = optional(list(string), [])
  }))
  default = {
    general = {
      count   = 2
      vcpu    = 2
      ram     = 4096
      disk_gb = 40
    }
    sandbox = {
      count   = 1
      vcpu    = 2
      ram     = 4096
      disk_gb = 40
      gvisor  = true
      labels  = { "neoledge.com/pool" = "sandbox" }
    }
  }
}

variable "allowed_api_cidrs" {
  description = <<-EOT
    Networks allowed to reach the Kubernetes API on 6443.

    Defaults to nothing, which means the API is reachable only from inside the
    cluster network. Widen it deliberately.
  EOT
  type        = list(string)
  default     = []
}

variable "allowed_ssh_cidrs" {
  description = "Networks allowed to reach SSH on port 22. Empty means no inbound SSH."
  type        = list(string)
  default     = []
}

variable "node_public_ips" {
  description = <<-EOT
    Whether the module gives each node a managed floating IP.

    CloudAxion has **no NAT gateway**: a node with no public address has no
    outbound route at all, not even DNS, so it cannot install RKE2 or pull
    images. This was measured, not assumed.

    Turn it off only if you are running your own NAT node and have adjusted the
    routing yourself.
  EOT
  type        = bool
  default     = true
}

variable "ingress_ports" {
  description = <<-EOT
    Port mappings published by the load balancer, as source_port => node_port.

    The load balancer is layer 4 only: terminate TLS in-cluster and point these
    at the ingress controller's node ports.
  EOT
  type        = map(number)
  default = {
    443 = 30443
    80  = 30080
  }
}

variable "create_load_balancer" {
  description = "Whether to create a load balancer in front of the agent nodes."
  type        = bool
  default     = true
}


