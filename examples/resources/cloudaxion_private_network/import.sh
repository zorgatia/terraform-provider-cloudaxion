# Import by UUID, using the provider's default location.
terraform import cloudaxion_private_network.core 2e8cd389-27fe-45ce-a63d-d536068659e5

# Or qualify the location when the network is not in the provider default.
terraform import cloudaxion_private_network.core tun1/2e8cd389-27fe-45ce-a63d-d536068659e5
