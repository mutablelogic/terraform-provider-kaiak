# Serve static files from a local directory
#
# Start the Kaiak server with a volume mount:
#
#   docker run -p 8084:8084 -v ./data:/data \
#     ghcr.io/mutablelogic/kaiak:latest run --http.addr=":8084"
#
# Then apply:
#
#   terraform init
#   terraform apply

terraform {
  required_providers {
    kaiak = {
      source = "mutablelogic/kaiak"
    }
  }
}

provider "kaiak" {
  # endpoint = "http://localhost:8084/api"  # or set KAIAK_ENDPOINT
  # api_key  = "my-secret-token"           # or set KAIAK_API_KEY
}

# Serve files from /data at the /files URL path
resource "kaiak_httpstatic" "files" {
  path = "/files"
  dir  = "/data"
}

# Router with the static handler registered
resource "kaiak_httprouter" "main" {
  title    = "Static File Server"
  version  = "1.0.0"
  openapi  = true
  handlers = [kaiak_httpstatic.files.name]
}

# HTTP server on port 9090 using the router
resource "kaiak_httpserver" "main" {
  listen = ":9090"
  router = kaiak_httprouter.main.name
}
