---
page_title: "Dynamic Resources"
---

# Dynamic Resources

The Kaiak provider discovers resource types dynamically from a running server.
Unlike most Terraform providers, you don't need to update the provider when new
resource types are added — they are automatically available once registered with
the server.

## How It Works

During `terraform plan` and `terraform apply`, the provider connects to the
Kaiak server and queries its API for available resource types and their schemas.
Each resource type becomes a Terraform resource named `kaiak_<resource_type>`.

For example, if the server has `httpserver`, `httpstatic`, and `logger` resource
types, you can manage them as:

```hcl
resource "kaiak_httpserver" "main" {
  listen = ":8080"
}

resource "kaiak_httpstatic" "docs" {
  path       = "/docs"
  dir        = "/var/www/docs"
  httpserver = kaiak_httpserver.main.id
}

resource "kaiak_logger" "default" {
}
```

## Fixed Attributes

Every dynamic resource has one fixed attribute:

* `id` - (Computed) The fully qualified instance name (`resource_type.label`),
  for example `"httpserver.main"`. A unique label is auto-generated on creation.

All other attributes are determined by the server's resource schema.

## Nested Blocks

Dotted attribute names from the server (e.g. `tls.cert`) are mapped to nested
blocks in Terraform:

```hcl
resource "kaiak_httpserver" "secure" {
  listen = ":8443"

  tls {
    cert = file("/path/to/cert.pem")
    key  = file("/path/to/key.pem")
  }
}
```

## Importing

Resources can be imported using their fully qualified name:

```hcl
import {
  to = kaiak_httpserver.main
  id = "httpserver.main"
}
```

Or using the CLI:

```sh
terraform import kaiak_httpserver.main httpserver.main
```

The import ID must match the resource type of the target block. For example,
importing `httpstatic.docs` into a `kaiak_httpserver` block will produce an error.

## Discovering Resources

Use the [`kaiak_resources`](/docs/data-sources/resources) data source to discover
available resource types and their schemas:

```hcl
data "kaiak_resources" "all" {}

output "available_types" {
  value = data.kaiak_resources.all.resources[*].name
}
```
