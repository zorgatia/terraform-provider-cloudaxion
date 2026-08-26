# A stable egress address the customer can add to their allow-list.
resource "cloudaxion_floating_ip" "egress" {
  name = "cluster-egress"
}
