resource "oci_objectstorage_bucket" "releases" {
  access_type           = "NoPublicAccess"
  auto_tiering          = "Disabled"
  bucket_scope          = "NAMESPACE"
  compartment_id        = var.compartment_ocid
  is_bucket_key_enabled = false
  name                  = "trachuan-releases"
  namespace             = data.oci_objectstorage_namespace.current.namespace
  object_events_enabled = false
  storage_tier          = "Standard"
  versioning            = "Disabled"
  lifecycle {
    prevent_destroy = true
  }
}

resource "oci_objectstorage_object_lifecycle_policy" "releases" {
  for_each  = { corpus = oci_objectstorage_bucket.releases, frameworks = oci_objectstorage_bucket.frameworks_releases }
  bucket    = each.value.name
  namespace = each.value.namespace

  rules {
    action      = "ABORT"
    is_enabled  = true
    name        = "abort-stale-multipart-uploads"
    target      = "multipart-uploads"
    time_amount = 2
    time_unit   = "DAYS"
  }

  depends_on = [oci_identity_policy.release_lifecycle]
}

resource "oci_objectstorage_bucket" "backups" {
  access_type           = "NoPublicAccess"
  auto_tiering          = "Disabled"
  bucket_scope          = "NAMESPACE"
  compartment_id        = var.compartment_ocid
  is_bucket_key_enabled = false
  name                  = "trachuan-backups"
  namespace             = data.oci_objectstorage_namespace.current.namespace
  object_events_enabled = false
  storage_tier          = "Standard"
  versioning            = "Disabled"
  lifecycle {
    prevent_destroy = true
  }
}

resource "oci_objectstorage_bucket" "frameworks_releases" {
  access_type           = "NoPublicAccess"
  auto_tiering          = "Disabled"
  bucket_scope          = "NAMESPACE"
  compartment_id        = var.compartment_ocid
  is_bucket_key_enabled = false
  name                  = "trachuan-frameworks-releases"
  namespace             = data.oci_objectstorage_namespace.current.namespace
  object_events_enabled = false
  storage_tier          = "Standard"
  versioning            = "Disabled"
  lifecycle {
    prevent_destroy = true
  }
}
