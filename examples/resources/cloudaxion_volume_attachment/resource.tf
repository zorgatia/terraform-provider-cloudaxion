resource "cloudaxion_volume_attachment" "data" {
  volume_id = cloudaxion_block_volume.data.id
  vm_id     = cloudaxion_vm.node.id
}

# The guest device name is not stable across reboots. Mount by id instead.
output "stable_device_path" {
  value = "/dev/disk/by-id/virtio-${cloudaxion_block_volume.data.id}"
}
