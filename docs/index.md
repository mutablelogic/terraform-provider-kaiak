---
page_title: "Provider: Kaiak"
---

# Kaiak Provider

The [Kaiak](https://github.com/mutablelogic/go-server) provider manages resources
on a running Kaiak server as infrastructure-as-code. Resource schemas are discovered
dynamically from the server at plan/apply time, so any resource type registered with
the server is automatically available in Terraform — no provider changes required.

Kaiak is a platform of composable software resources that can be assembled into
custom server applications. For more information on creating custom resource types,
see the [Kaiak repository](https://github.com/mutablelogic/go-server).

## Running the Kaiak Server

The easiest way to run a Kaiak server is with Docker:

```sh
docker pull ghcr.io/mutablelogic/kaiak:latest
docker run -p 8084:8084 ghcr.io/mutablelogic/kaiak:latest
```

The base image includes a set of built-in resource types (HTTP server, static
file serving, logging, etc.), but the platform is designed to be extended with
your own custom resource types. Multi-arch images (amd64, arm64) are available.

For more information on configuring the server and creating custom resources,
see the [Kaiak documentation](https://github.com/mutablelogic/go-server).

## Example Usage

```hcl
provider "kaiak" {
  endpoint = "http://localhost:8084/api"
  api_key  = "my-secret-token"
}

resource "kaiak_httpserver" "main" {
  name   = "main"
  listen = ":8080"
}
```

## Authentication

The provider authenticates using a bearer token. Set the `api_key` attribute in
the provider block or the `KAIAK_API_KEY` environment variable.

## Argument Reference

* `endpoint` - (Optional) Base URL of the Kaiak server API. Defaults to
  `http://localhost:8084/api`. Can also be set with the `KAIAK_ENDPOINT`
  environment variable.

* `api_key` - (Optional, Sensitive) Bearer token for authenticating with the
  Kaiak server. Can also be set with the `KAIAK_API_KEY` environment variable.

Config values take precedence over environment variables.

## Debugging

### Debug Mode

Start the provider in debug mode for use with a debugger or `TF_REATTACH_PROVIDERS`:

```sh
terraform-provider-kaiak -debug
```

### HTTP Request Tracing

Set the `KAIAK_TRACE` environment variable to log HTTP requests and responses between
the provider and the Kaiak server to stderr:

```sh
# Log request/response headers
export KAIAK_TRACE=true

# Log headers and response bodies
export KAIAK_TRACE=verbose
```
