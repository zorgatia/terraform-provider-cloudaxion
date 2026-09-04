output "network_id" {
  description = "UUID of the private network the cluster runs in."
  value       = cloudaxion_private_network.this.id
}

output "network_subnet" {
  description = <<-EOT
    Address range CloudAxion allocated. It cannot be requested, only read back,
    so anything that needs to know the cluster's addressing takes it from here.
  EOT
  value       = cloudaxion_private_network.this.subnet
}

output "server_private_ips" {
  description = "Private addresses of the control plane nodes."
  value       = [for vm in local.servers : vm.private_ipv4]
}

output "node_public_ips" {
  description = "Public address of each node, keyed by node name."
  value       = { for key, ip in cloudaxion_floating_ip.node : key => ip.address }
}

output "egress_addresses" {
  description = <<-EOT
    Every address the cluster can appear as when calling out.

    CloudAxion has no NAT gateway, so this is the whole node list rather than a
    single address — a customer allow-list has to contain all of them, and it
    grows when the cluster does. See the README.
  EOT
  value       = sort([for ip in cloudaxion_floating_ip.node : ip.address])
}

output "agent_private_ips" {
  description = "Private addresses of the worker nodes, keyed by node name."
  value       = { for key, vm in cloudaxion_vm.agent : key => vm.private_ipv4 }
}

output "sandbox_nodes" {
  description = "Worker nodes running the gVisor runtime."
  value       = [for key, node in local.agent_nodes : key if node.gvisor]
}

output "ingress_address" {
  description = "Load balancer address published to clients."
  value       = var.create_load_balancer ? cloudaxion_floating_ip.ingress[0].address : null
}

output "cluster_token" {
  description = "RKE2 join token. Anyone holding it can add a node."
  value       = random_password.token.result
  sensitive   = true
}

output "kubeconfig_command" {
  description = <<-EOT
    How to fetch the kubeconfig.

    RKE2 writes it to /etc/rancher/rke2/rke2.yaml on the server, and CloudAxion
    has no metadata or console API to read it out, so it is fetched over SSH.
    The server address inside the file is 127.0.0.1 and has to be rewritten.
  EOT
  value = format(
    "ssh %s@%s 'sudo cat /etc/rancher/rke2/rke2.yaml' | sed 's|127.0.0.1|%s|' > kubeconfig.yaml",
    var.ssh_username,
    local.kubeconfig_host,
    local.kubeconfig_host,
  )
}
