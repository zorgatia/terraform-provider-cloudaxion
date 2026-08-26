# CloudAxion has no endpoint that lists resources across locations, so the
# location slug matters: every scoped resource lives in exactly one.
data "cloudaxion_locations" "all" {}

output "available_locations" {
  value = [for l in data.cloudaxion_locations.all.locations : l.slug]
}

output "default_location" {
  value = data.cloudaxion_locations.all.default_slug
}
