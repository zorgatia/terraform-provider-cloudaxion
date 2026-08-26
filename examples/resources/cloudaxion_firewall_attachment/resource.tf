resource "cloudaxion_firewall_attachment" "control_plane" {
  firewall_id = cloudaxion_firewall.control_plane.id
  vm_id       = cloudaxion_vm.node.id
}
