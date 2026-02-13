package main

import (
	"fmt"
	"log"
	"strings"

	// Packages
	attr "github.com/hashicorp/terraform-plugin-framework/attr"
	diag "github.com/hashicorp/terraform-plugin-framework/diag"
	tfschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	planmodifier "github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	stringplanmodifier "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	schema "github.com/mutablelogic/go-server/pkg/provider/schema"
)

///////////////////////////////////////////////////////////////////////////////
// TYPES

// attrInfo maps a single kaiak attribute to its terraform representation.
type attrInfo struct {
	kaiakName string           // original kaiak name, e.g. "tls.cert"
	tfBlock   string           // terraform block name, empty for top-level
	tfField   string           // field name within block (or top-level name)
	attr      schema.Attribute // original kaiak attribute metadata
}

///////////////////////////////////////////////////////////////////////////////
// PUBLIC METHODS

// buildResourceSchema converts kaiak resource attributes into a terraform
// resource schema. Dotted attribute names (e.g. "tls.cert") are grouped
// into SingleNestedAttribute blocks. The fixed "name" and "id" attributes
// are prepended.
func buildResourceSchema(resourceName string, kaiakAttrs []schema.Attribute) (tfschema.Schema, []attrInfo, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Build attrInfo list and detect naming collisions. Two kaiak
	// attributes could map to the same terraform field when dots are
	// converted to underscores (e.g. "tls.cert_key" and "tls.cert.key"
	// both become block "tls", field "cert_key").
	var infos []attrInfo
	seen := map[string]string{}  // "block/field" → original kaiak name
	reserved := map[string]bool{ // top-level names reserved for internal use
		"name": true,
		"id":   true,
	}
	for _, a := range kaiakAttrs {
		info := newAttrInfo(a)
		if info.tfBlock == "" && reserved[info.tfField] {
			diags.AddError("Reserved attribute name",
				fmt.Sprintf("Resource %q: attribute %q conflicts with reserved terraform attribute %q",
					resourceName, a.Name, info.tfField))
			continue
		}
		key := info.tfBlock + "/" + info.tfField
		if prev, ok := seen[key]; ok {
			diags.AddError("Attribute naming collision",
				fmt.Sprintf("Resource %q: attributes %q and %q both map to terraform field %q (block %q)",
					resourceName, prev, a.Name, info.tfField, info.tfBlock))
			continue
		}
		seen[key] = a.Name
		infos = append(infos, info)
	}

	if diags.HasError() {
		return tfschema.Schema{}, nil, diags
	}

	// Separate top-level attributes from block members
	tfAttrs := map[string]tfschema.Attribute{
		"name": tfschema.StringAttribute{
			Description: "Instance label (e.g. \"main\").",
			Required:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"id": tfschema.StringAttribute{
			Description: "Fully qualified instance name (resource_type.label).",
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
	}

	// Group block members by prefix
	blocks := map[string]map[string]tfschema.Attribute{}

	for _, info := range infos {
		tfAttr := kaiakAttrToTF(info.attr)
		if info.tfBlock != "" {
			if blocks[info.tfBlock] == nil {
				blocks[info.tfBlock] = map[string]tfschema.Attribute{}
			}
			blocks[info.tfBlock][info.tfField] = tfAttr
		} else {
			tfAttrs[info.tfField] = tfAttr
		}
	}

	// Convert grouped block members to SingleNestedAttribute
	for blockName, blockAttrs := range blocks {
		tfAttrs[blockName] = tfschema.SingleNestedAttribute{
			Attributes: blockAttrs,
			Optional:   true,
		}
	}

	return tfschema.Schema{
		Description: fmt.Sprintf("Manages a %s resource instance on a running Kaiak server.", resourceName),
		Attributes:  tfAttrs,
	}, infos, diags
}

///////////////////////////////////////////////////////////////////////////////
// ATTRIBUTE TYPE HELPERS

// kaiakTypeToAttrType returns the terraform attr.Type for a kaiak type string.
func kaiakTypeToAttrType(t string) attr.Type {
	switch t {
	case "bool":
		return types.BoolType
	case "int":
		return types.Int64Type
	default:
		return types.StringType
	}
}

// kaiakValueToTF converts a kaiak state value to a terraform attr.Value.
func kaiakValueToTF(v any, t string) attr.Value {
	if v == nil {
		return kaiakNullValue(t)
	}
	switch t {
	case "bool":
		if b, ok := v.(bool); ok {
			return types.BoolValue(b)
		}
	case "int":
		switch n := v.(type) {
		case float64:
			return types.Int64Value(int64(n))
		case int:
			return types.Int64Value(int64(n))
		}
	}

	// Value does not match its declared type — fall back to string but
	// log the mismatch so server-side data issues are not silently hidden.
	if t != "string" {
		log.Printf("[WARN] kaiak attribute type mismatch: declared %q but got %T (%v); coercing to string", t, v, v)
	}
	return types.StringValue(fmt.Sprintf("%v", v))
}

// kaiakNullValue returns a typed null for the given kaiak type.
func kaiakNullValue(t string) attr.Value {
	switch t {
	case "bool":
		return types.BoolNull()
	case "int":
		return types.Int64Null()
	default:
		return types.StringNull()
	}
}

///////////////////////////////////////////////////////////////////////////////
// PRIVATE METHODS

// newAttrInfo derives terraform naming from a kaiak attribute.
// Dots split into block + field (e.g. "tls.cert" → block "tls", field "cert").
func newAttrInfo(a schema.Attribute) attrInfo {
	info := attrInfo{kaiakName: a.Name, attr: a}
	if parts := strings.SplitN(a.Name, ".", 2); len(parts) == 2 {
		info.tfBlock = parts[0]
		info.tfField = strings.ReplaceAll(parts[1], ".", "_")
	} else {
		info.tfField = a.Name
	}
	return info
}

// kaiakAttrToTF converts a single kaiak attribute to a terraform schema attribute.
// Optional attributes are marked Computed so the server can supply defaults
// without Terraform flagging an inconsistent result after apply.
func kaiakAttrToTF(a schema.Attribute) tfschema.Attribute {
	opt := !a.Required && !a.ReadOnly
	computed := a.ReadOnly || opt // server may fill in defaults for optional attrs
	switch a.Type {
	case "bool":
		return tfschema.BoolAttribute{
			Description: a.Description,
			Required:    a.Required,
			Optional:    opt,
			Computed:    computed,
			Sensitive:   a.Sensitive,
		}
	case "int":
		return tfschema.Int64Attribute{
			Description: a.Description,
			Required:    a.Required,
			Optional:    opt,
			Computed:    computed,
			Sensitive:   a.Sensitive,
		}
	default:
		return tfschema.StringAttribute{
			Description: a.Description,
			Required:    a.Required,
			Optional:    opt,
			Computed:    computed,
			Sensitive:   a.Sensitive,
		}
	}
}
