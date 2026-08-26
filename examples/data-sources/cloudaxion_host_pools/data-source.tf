# Host pools carry the real VM sizing limits, which differ between locations
# and are not reported by the VM parameters endpoint.
data "cloudaxion_host_pools" "tun01" {
  location = "tun01"
}

output "default_pool" {
  value = one([for p in data.cloudaxion_host_pools.tun01.pools : p.uuid if p.is_default])
}
