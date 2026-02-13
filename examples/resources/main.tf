terraform {
  required_providers {
    kaiak = {
      source  = "mutablelogic/kaiak"
      version = ">= 1.6.8"
    }
  }
}

provider "kaiak" {
  # endpoint = "http://localhost:8084/api"  # or set KAIAK_ENDPOINT
  # api_key  = "my-secret-token"           # or set KAIAK_API_KEY
}

data "kaiak_resources" "all" {}

output "provider_name" {
  value = data.kaiak_resources.all.provider_name
}

output "provider_version" {
  value = data.kaiak_resources.all.version
}

output "resources" {
  value = data.kaiak_resources.all.resources
}
