resource "cloudaxion_volume_attachment" "data" {
  volume_id = cloudaxion_block_volume.data.id
  vm_id     = cloudaxion_vm.node.id
}

# The guest device name is not stable across reboots. Mount by id instead.
#
# The UUID is TRUNCATED to its first 20 characters: udev builds the link from
# the virtio-blk serial field, which is capped at 20 bytes. Interpolating the
# full 36-character UUID yields a path that never exists, and the failure is
# silent until mkfs or mount runs. Verified on tun01, 2026-09-04.
output "stable_device_path" {
  value = "/dev/disk/by-id/virtio-${substr(cloudaxion_block_volume.data.id, 0, 20)}"
}
