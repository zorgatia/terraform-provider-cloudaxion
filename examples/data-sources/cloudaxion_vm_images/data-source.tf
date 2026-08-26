# CloudAxion has no image identifier: a VM picks its image by os_name/os_version.
data "cloudaxion_vm_images" "all" {}

output "ubuntu_versions" {
  value = flatten([
    for image in data.cloudaxion_vm_images.all.images : [
      for version in image.versions : version.os_version if version.published
    ] if image.os_name == "ubuntu"
  ])
}
