resource "cloudaxion_private_network" "core" {
  name = "core"
}

# CloudAxion allocates the VLAN and the address range; neither can be requested.
# Read the subnet back rather than planning one in advance.
output "core_subnet" {
  value = cloudaxion_private_network.core.subnet
}

output "core_vlan" {
  value = cloudaxion_private_network.core.vlan_id
}
