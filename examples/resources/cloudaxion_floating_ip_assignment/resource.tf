resource "cloudaxion_floating_ip_assignment" "egress" {
  address     = cloudaxion_floating_ip.egress.address
  resource_id = cloudaxion_vm.node.id
  # resource_type defaults to "virtual_machine"
}
