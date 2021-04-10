terraform {
  backend "gcs" {
    bucket      = "pv-migrate-terraform-backend"
    credentials = ".serviceaccount.json"
  }
}

provider "google" {
  credentials = file(".serviceaccount.json")
  project     = var.gcp_project_id
  region      = var.gcp_region
  zone        = var.gcp_zone
}

resource "google_container_cluster" "cluster_1" {
  name               = "pv-migrate-test-1"
  location           = var.gcp_zone
  initial_node_count = 1
  logging_service    = "none"
  monitoring_service = "none"
  cluster_autoscaling {
    enabled = false
  }
  vertical_pod_autoscaling {
    enabled = false
  }
  node_config {
    machine_type = "e2-micro"
    disk_size_gb = 16
  }
}

resource "google_container_cluster" "cluster_2" {
  name               = "pv-migrate-test-2"
  location           = var.gcp_zone
  initial_node_count = 1
  logging_service    = "none"
  monitoring_service = "none"
  cluster_autoscaling {
    enabled = false
  }
  vertical_pod_autoscaling {
    enabled = false
  }
  node_config {
    machine_type = "e2-micro"
    disk_size_gb = 16
  }
}
