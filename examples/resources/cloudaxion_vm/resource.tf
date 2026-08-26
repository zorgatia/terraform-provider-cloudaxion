data "cloudaxion_vm_images" "all" {}

resource "cloudaxion_private_network" "cluster" {
  name = "cluster"
}

resource "cloudaxion_vm" "node" {
  name       = "k8s-worker-1"
  os_name    = "ubuntu"
  os_version = "24.04"

  vcpu    = 4
  ram     = 8192 # MB
  disk_gb = 80

  # Always set the network. Omitting it places the guest in the account's
  # default network, which on a populated account usually means production.
  network_uuid = cloudaxion_private_network.cluster.id

  # Cluster nodes reach the internet through a shared egress address rather
  # than each holding a public IP of their own.
  reserve_public_ip = false

  username    = "ops"
  public_keys = [file("~/.ssh/id_ed25519.pub")]
  cloud_init  = file("${path.module}/cloud-init.yaml")

  # Creation blocks until the guest is running: 33 seconds for the smallest
  # possible VM, considerably longer for a large Windows guest.
  timeouts {
    create = "45m"
  }
}
