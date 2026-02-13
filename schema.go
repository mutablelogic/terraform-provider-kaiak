package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	// Packages
	attr "github.com/hashicorp/terraform-plugin-framework/attr"
	diag "github.com/hashicorp/terraform-plugin-framework/diag"
	tfschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	planmodifier "github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	stringplanmodifier "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	tflog "github.com/hashicorp/terraform-plugin-log/tflog"
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

	// Convert grouped block members to SingleNestedAttribute.
	// Mark the block Required when any nested attribute is required.
	for blockName, blockAttrs := range blocks {
		required := false
		for _, a := range blockAttrs {
			switch ta := a.(type) {
			case tfschema.StringAttribute:
				if ta.Required {
					required = true
				}
			case tfschema.BoolAttribute:
				if ta.Required {
					required = true
				}
			case tfschema.Int64Attribute:
				if ta.Required {
					required = true
				}
			case tfschema.Float64Attribute:
				if ta.Required {
					required = true
				}
			case tfschema.ListAttribute:
				if ta.Required {
					required = true
				}
			case tfschema.MapAttribute:
				if ta.Required {
					required = true
				}
			}
		}
		tfAttrs[blockName] = tfschema.SingleNestedAttribute{
			Attributes: blockAttrs,
			Required:   required,
			Optional:   !required,
			Computed:   !required, // server may populate defaults for optional blocks
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
	switch {
	case t == "bool":
		return types.BoolType
	case t == "int" || t == "uint":
		return types.Int64Type
	case t == "float":
		return types.Float64Type
	case strings.HasPrefix(t, "[]"):
		return types.ListType{ElemType: kaiakTypeToAttrType(t[2:])}
	case strings.HasPrefix(t, "map["):
		return types.MapType{ElemType: kaiakMapElemType(t)}
	default:
		return types.StringType
	}
}

// kaiakMapElemType extracts the value type from a kaiak map type string
// like "map[string]int" and returns the corresponding terraform attr.Type.
func kaiakMapElemType(t string) attr.Type {
	if idx := strings.Index(t, "]"); idx >= 0 && idx+1 < len(t) {
		return kaiakTypeToAttrType(t[idx+1:])
	}
	return types.StringType
}

// kaiakValueToTF converts a kaiak state value to a terraform attr.Value.
func kaiakValueToTF(ctx context.Context, v any, t string) attr.Value {
	if v == nil {
		return kaiakNullValue(t)
	}
	switch {
	case t == "bool":
		if b, ok := v.(bool); ok {
			return types.BoolValue(b)
		}
	case t == "int" || t == "uint":
		switch n := v.(type) {
		case float64:
			return types.Int64Value(int64(n))
		case int:
			return types.Int64Value(int64(n))
		}
	case t == "float":
		switch n := v.(type) {
		case float64:
			return types.Float64Value(n)
		case int:
			return types.Float64Value(float64(n))
		}
	case strings.HasPrefix(t, "[]"):
		return kaiakSliceToTF(ctx, v, t)
	case strings.HasPrefix(t, "map["):
		return kaiakMapToTF(ctx, v, t)
	case t == "time":
		// The server marshals time.Time as RFC 3339 via JSON.
		if s, ok := v.(string); ok {
			if parsed, err := time.Parse(time.RFC3339, s); err == nil {
				return types.StringValue(parsed.Format(time.RFC3339))
			}
			return types.StringValue(s)
		}
	}

	// Value does not match its declared type — fall back to string but
	// log the mismatch so server-side data issues are not silently hidden.
	// The raw value is intentionally omitted to avoid leaking sensitive data.
	if t != "string" && t != "duration" && t != "ref" {
		tflog.Warn(ctx, "Kaiak attribute type mismatch: coercing to string", map[string]interface{}{
			"declared_type": t,
			"actual_type":   fmt.Sprintf("%T", v),
		})
	}
	return types.StringValue(fmt.Sprintf("%v", v))
}

// kaiakSliceToTF converts a kaiak slice value to a terraform ListValue.
func kaiakSliceToTF(ctx context.Context, v any, t string) attr.Value {
	elemType := kaiakTypeToAttrType(t[2:])
	items, ok := v.([]interface{})
	if !ok {
		return types.ListNull(elemType)
	}
	elems := make([]attr.Value, 0, len(items))
	for _, item := range items {
		elems = append(elems, kaiakValueToTF(ctx, item, t[2:]))
	}
	list, diags := types.ListValue(elemType, elems)
	if diags.HasError() {
		return types.ListNull(elemType)
	}
	return list
}

// kaiakMapToTF converts a kaiak map value to a terraform MapValue.
func kaiakMapToTF(ctx context.Context, v any, t string) attr.Value {
	elemType := kaiakMapElemType(t)
	items, ok := v.(map[string]interface{})
	if !ok {
		return types.MapNull(elemType)
	}
	idx := strings.Index(t, "]")
	if idx < 0 || idx+1 >= len(t) {
		tflog.Warn(ctx, "Malformed map type string, treating values as strings", map[string]interface{}{
			"type": t,
		})
		return types.MapNull(types.StringType)
	}
	valType := t[idx+1:]
	elems := make(map[string]attr.Value, len(items))
	for k, item := range items {
		elems[k] = kaiakValueToTF(ctx, item, valType)
	}
	m, diags := types.MapValue(elemType, elems)
	if diags.HasError() {
		return types.MapNull(elemType)
	}
	return m
}

// kaiakNullValue returns a typed null for the given kaiak type.
func kaiakNullValue(t string) attr.Value {
	switch {
	case t == "bool":
		return types.BoolNull()
	case t == "int" || t == "uint":
		return types.Int64Null()
	case t == "float":
		return types.Float64Null()
	case strings.HasPrefix(t, "[]"):
		return types.ListNull(kaiakTypeToAttrType(t[2:]))
	case strings.HasPrefix(t, "map["):
		return types.MapNull(kaiakMapElemType(t))
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
	switch {
	case a.Type == "bool":
		return tfschema.BoolAttribute{
			Description: a.Description,
			Required:    a.Required,
			Optional:    opt,
			Computed:    computed,
			Sensitive:   a.Sensitive,
		}
	case a.Type == "int" || a.Type == "uint":
		return tfschema.Int64Attribute{
			Description: a.Description,
			Required:    a.Required,
			Optional:    opt,
			Computed:    computed,
			Sensitive:   a.Sensitive,
		}
	case a.Type == "float":
		return tfschema.Float64Attribute{
			Description: a.Description,
			Required:    a.Required,
			Optional:    opt,
			Computed:    computed,
			Sensitive:   a.Sensitive,
		}
	case strings.HasPrefix(a.Type, "[]"):
		return tfschema.ListAttribute{
			Description: a.Description,
			ElementType: kaiakTypeToAttrType(a.Type[2:]),
			Required:    a.Required,
			Optional:    opt,
			Computed:    computed,
			Sensitive:   a.Sensitive,
		}
	case strings.HasPrefix(a.Type, "map["):
		return tfschema.MapAttribute{
			Description: a.Description,
			ElementType: kaiakMapElemType(a.Type),
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
