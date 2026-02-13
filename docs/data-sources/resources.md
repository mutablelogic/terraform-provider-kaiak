---
page_title: "kaiak_resources Data Source"
---

# kaiak_resources Data Source

Lists available resource types and their instances on a running Kaiak server.
Use this data source to discover what resource types the server supports and
which instances already exist.

## Example Usage

### List all resources

```hcl
data "kaiak_resources" "all" {}

output "resource_types" {
  value = data.kaiak_resources.all.resources[*].name
}
```

### Filter by type

```hcl
data "kaiak_resources" "servers" {
  type = "httpserver"
}
```

## Argument Reference

* `type` - (Optional) Filter by resource type name (e.g. `"httpserver"`).
  When omitted, all resource types are returned. Must be a known value at
  plan time — computed values from other resources are not supported.

## Attribute Reference

* `provider_name` - The provider name reported by the server.
* `version` - The provider version reported by the server.
* `resources` - A list of resource types. Each element contains:
  * `name` - The resource type name.
  * `attributes` - Schema attributes for this resource type. Each element contains:
    * `name` - Attribute name.
    * `type` - Attribute type (e.g. `"string"`, `"int"`, `"bool"`, `"[]string"`, `"map[string]string"`).
    * `description` - Human-readable description.
    * `required` - Whether the attribute is required.
    * `readonly` - Whether the attribute is read-only (computed by the server).
    * `sensitive` - Whether the attribute contains sensitive data.
    * `reference` - Whether the attribute references another resource.
  * `instances` - Existing instances of this resource type. Each element contains:
    * `name` - The fully qualified instance name (e.g. `"httpserver.main"`).
    * `resource` - The resource type name.
    * `readonly` - Whether the instance is read-only.
