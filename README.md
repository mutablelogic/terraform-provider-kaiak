# terraform-provider-kaiak

[Kaiak](https://github.com/mutablelogic/go-server) is a platform of composable
software resources that can be assembled into custom server applications. This
[Terraform](https://www.terraform.io/) provider lets you manage those resources
and their configuration as infrastructure-as-code. Resource schemas are discovered
dynamically from a running server at plan/apply time, so any resource registered
with the server is automatically available in Terraform.

Kaiak is designed to be extensible — you can create your own resource types and
register them with the server. Once registered, they are immediately available
for management through this Terraform provider without any changes to the provider
itself. For more information on how to create custom resources, see the
[Kaiak repository](https://github.com/mutablelogic/go-server).

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.25 (to build the provider)
- A running Kaiak server with the provider API enabled

## Running the Kaiak Server

The easiest way to run a Kaiak server is with Docker:

```sh
docker pull ghcr.io/mutablelogic/kaiak:latest
docker run -p 8084:8084 ghcr.io/mutablelogic/kaiak:latest
```

The base image includes a set of built-in resource types (HTTP server, static
file serving, logging, etc.), but the platform is designed to be extended with
your own custom resource types. Once you register a new resource type with the
server, it becomes automatically available through this Terraform provider.

Multi-arch images (amd64, arm64) are available. For more information on
configuring the server and creating custom resources, see the
[Kaiak documentation](https://github.com/mutablelogic/go-server).

## Deployment

### From the Terraform Registry

```hcl
terraform {
  required_providers {
    kaiak = {
      source = "mutablelogic/kaiak"
    }
  }
}
```

### Building from source

```sh
git clone https://github.com/mutablelogic/terraform-provider-kaiak.git
cd terraform-provider-kaiak
go build -o terraform-provider-kaiak
```

To use a locally built provider, add a `dev_overrides` block to your
`~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "mutablelogic/kaiak" = "/path/to/your/build/directory"
  }
  direct {}
}
```

## Configuration

### Provider

```hcl
provider "kaiak" {
  endpoint = "http://localhost:8084/api"  # optional
  api_key  = "my-secret-token"           # optional, sensitive
}
```

| Attribute  | Description | Default | Environment Variable |
|------------|-------------|---------|---------------------|
| `endpoint` | Base URL of the Kaiak server API | `http://localhost:8084/api` | `KAIAK_ENDPOINT` |
| `api_key`  | Bearer token for authentication | _(none)_ | `KAIAK_API_KEY` |

Both attributes can be set via environment variables instead of (or in addition to)
the provider block. Config values take precedence over environment variables.

### Resources

Resources are discovered dynamically from the running server. Each resource type
registered with the server becomes available as `kaiak_<resource_type>`. For example,
if the server has an `httpserver` resource type:

```hcl
resource "kaiak_httpserver" "main" {
  name = "main"
  listen = ":8080"
}
```

Every resource has two fixed attributes:

| Attribute | Description |
|-----------|-------------|
| `name`    | Instance label (e.g. `"main"`). Changing this forces recreation. |
| `id`      | Fully qualified instance name (`resource_type.label`). Computed. |

Additional attributes are determined by the server's resource schema. Dotted
attribute names (e.g. `tls.cert`) are mapped to nested blocks in Terraform:

```hcl
resource "kaiak_httpserver" "secure" {
  name   = "secure"
  listen = ":8443"

  tls {
    cert = "/path/to/cert.pem"
    key  = "/path/to/key.pem"
  }
}
```

### Data Sources

#### `kaiak_resources`

Lists available resource types and their instances on the server.

```hcl
data "kaiak_resources" "all" {}

data "kaiak_resources" "servers" {
  type = "httpserver"
}
```

| Attribute       | Description |
|-----------------|-------------|
| `type`          | Filter by resource type name (optional) |
| `provider_name` | Provider name (computed) |
| `version`       | Provider version (computed) |
| `resources`     | List of resource types with their attributes and instances (computed) |

### Importing Resources

Resources can be imported using their fully qualified name:

```sh
terraform import kaiak_httpserver.main httpserver.main
```

## License

Copyright © 2026 David Thorpe. All rights reserved.

Licensed under the [Apache License, Version 2.0](LICENSE). You may use, distribute,
and modify this software under the terms of the license. The software is provided
"as is", without warranty of any kind. See the [LICENSE](LICENSE) file for the full
license text.
