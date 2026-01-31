variable "gcp_project_id" {
  type        = string
  description = "GCP project ID"
}

variable "gcp_region" {
  type        = string
  description = "GCP region"
}

variable "gcp_zone" {
  type        = string
  description = "GCP zone"
}

variable "prefix" {
  type        = string
  description = "Resource name prefix"
}

variable "network_name" {
  type        = string
  default     = "default"
  description = "VPC network name"
}

variable "volumes_redis_url_secret_version" {
  description = "Secret version resource for volumes Redis URL"
}

variable "volumes_redis_tls_ca_base64_secret_version" {
  description = "Secret version resource for volumes Redis TLS CA"
}
