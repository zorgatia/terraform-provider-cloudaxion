# Import by UUID, using the provider's default location.
terraform import cloudaxion_vm.node 6c772f89-d00c-4d41-8acd-0202599c4ed3

# Or qualify the location.
terraform import cloudaxion_vm.node tun01/6c772f89-d00c-4d41-8acd-0202599c4ed3

# Note: the API never returns a VM's password, so importing a machine whose
# configuration sets `password` will show a replacement. Use `public_keys`, or
# add `lifecycle { ignore_changes = [password] }`.
