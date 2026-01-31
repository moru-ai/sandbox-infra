# ============================================================================
# Volumes Redis Cluster Module
# Dedicated Valkey/Redis cluster for JuiceFS volume metadata
# Separate from main app Redis to allow independent enabling
# ============================================================================

# Enable the Service Networking API (needed for PSC)
resource "google_project_service" "service_networking" {
  service            = "servicenetworking.googleapis.com"
  disable_on_destroy = false
}

resource "time_sleep" "service_networking_api_wait" {
  depends_on = [google_project_service.service_networking]
  create_duration = "60s"
}

# Enable the Memorystore API
resource "google_project_service" "memorystore" {
  service            = "memorystore.googleapis.com"
  disable_on_destroy = false
}

resource "time_sleep" "memorystore_api_wait" {
  depends_on = [google_project_service.memorystore]
  create_duration = "60s"
}

# Get the default network subnetwork
resource "google_compute_subnetwork" "default" {
  name                     = var.network_name
  region                   = var.gcp_region
  network                  = "projects/${var.gcp_project_id}/global/networks/${var.network_name}"
  private_ip_google_access = true
}

# PSC policy for Valkey on default VPC
resource "google_network_connectivity_service_connection_policy" "volumes_valkey" {
  name          = "${var.prefix}volumes-valkey-connection-policy"
  location      = var.gcp_region
  service_class = "gcp-memorystore"
  description   = "Connection policy for volumes Redis cluster"
  network       = "projects/${var.gcp_project_id}/global/networks/${var.network_name}"
  psc_config {
    subnetworks = [google_compute_subnetwork.default.id]
  }
}

# Volume Metadata Redis Cluster
# Dedicated cluster for JuiceFS volume metadata - failure isolated from app cache
resource "google_memorystore_instance" "volumes" {
  project     = var.gcp_project_id
  location    = var.gcp_region
  instance_id = "${var.prefix}volume-meta"

  engine_version = "VALKEY_8_0"
  mode           = "CLUSTER"

  desired_auto_created_endpoints {
    network    = "projects/${var.gcp_project_id}/global/networks/${var.network_name}"
    project_id = var.gcp_project_id
  }

  # Start small - 1 shard, 1 replica for HA
  shard_count             = 1
  replica_count           = 1
  node_type               = "SHARED_CORE_NANO" # ~$27/mo per shard+replica
  transit_encryption_mode = "SERVER_AUTHENTICATION"
  authorization_mode      = "AUTH_DISABLED"

  zone_distribution_config {
    mode = "MULTI_ZONE" # Required when replica_count > 0
  }

  deletion_protection_enabled = true

  maintenance_policy {
    weekly_maintenance_window {
      day = "SUNDAY"
      start_time {
        hours = 1
      }
    }
  }

  persistence_config {
    mode = "AOF"
    aof_config {
      append_fsync = "EVERY_SEC"
    }
  }

  depends_on = [
    google_network_connectivity_service_connection_policy.volumes_valkey,
    google_project_service.memorystore,
    time_sleep.memorystore_api_wait
  ]
}

locals {
  redis_connection = google_memorystore_instance.volumes.endpoints[0].connections[0].psc_auto_connection[0]
}

# Store Redis URL in Secret Manager
resource "google_secret_manager_secret_version" "volumes_redis_url" {
  secret      = var.volumes_redis_url_secret_version.secret
  secret_data = "${local.redis_connection.ip_address}:${local.redis_connection.port}"
}

# Store TLS CA in Secret Manager
resource "google_secret_manager_secret_version" "volumes_redis_tls_ca_base64" {
  secret      = var.volumes_redis_tls_ca_base64_secret_version.secret
  secret_data = base64encode(join("\n", google_memorystore_instance.volumes.managed_server_ca[0].ca_certs[0].certificates))
}
