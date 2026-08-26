# Layer 4 only: TLS terminates in-cluster, this is the front door.
resource "cloudaxion_load_balancer" "ingress" {
  name       = "ingress"
  network_id = cloudaxion_private_network.cluster.id

  rule {
    source_port = 443
    target_port = 30443 # ingress controller node port
  }

  rule {
    source_port = 80
    target_port = 30080
  }

  target {
    id = cloudaxion_vm.node.id
  }
}
