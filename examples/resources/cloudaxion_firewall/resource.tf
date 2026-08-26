resource "cloudaxion_firewall" "control_plane" {
  name        = "k8s-control-plane"
  description = "Kubernetes API and node-to-node traffic"

  rule {
    protocol           = "tcp"
    direction          = "inbound"
    port_start         = 6443
    port_end           = 6443
    endpoint_spec_type = "ip_prefixes"
    endpoint_spec      = ["10.0.0.0/8"]
  }

  # A single port: omit port_end and the API normalises it to port_start.
  rule {
    protocol           = "tcp"
    direction          = "inbound"
    port_start         = 22
    endpoint_spec_type = "ip_prefixes"
    endpoint_spec      = ["203.0.113.0/24"]
  }
}
