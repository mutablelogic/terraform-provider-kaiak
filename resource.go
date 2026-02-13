package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	// Packages
	attr "github.com/hashicorp/terraform-plugin-framework/attr"
	diag "github.com/hashicorp/terraform-plugin-framework/diag"
	path "github.com/hashicorp/terraform-plugin-framework/path"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	tfsdk "github.com/hashicorp/terraform-plugin-framework/tfsdk"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	httpclient "github.com/mutablelogic/go-server/pkg/provider/httpclient"
	schema "github.com/mutablelogic/go-server/pkg/provider/schema"
)

///////////////////////////////////////////////////////////////////////////////
// TYPES

// dynamicResource implements a Terraform resource whose schema is discovered
// at runtime from the Kaiak server.
type dynamicResource struct {
	client *httpclient.Client
	meta   schema.ResourceMeta
	infos  []attrInfo
}

// attrGetter is satisfied by tfsdk.Config, tfsdk.Plan, and tfsdk.State.
type attrGetter interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}

var _ resource.Resource = (*dynamicResource)(nil)
var _ resource.ResourceWithImportState = (*dynamicResource)(nil)

// getInfos returns the cached attrInfo slice, building it on first call.
// This is necessary because the Terraform framework may call Schema() on one
// resource instance and CRUD methods on a different instance.
func (r *dynamicResource) getInfos() []attrInfo {
	if r.infos == nil {
		_, infos, _ := buildResourceSchema(r.meta.Name, r.meta.Attributes)
		r.infos = infos
	}
	return r.infos
}

///////////////////////////////////////////////////////////////////////////////
// LIFECYCLE

func newDynamicResource(meta schema.ResourceMeta) *dynamicResource {
	return &dynamicResource{meta: meta}
}

// fullName returns the fully-qualified kaiak instance name.
func (r *dynamicResource) fullName(label string) string {
	return r.meta.Name + "." + label
}

// generateLabel returns a short random hex string for use as an instance label.
func generateLabel() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "tf_" + hex.EncodeToString(b)
}

///////////////////////////////////////////////////////////////////////////////
// RESOURCE INTERFACE

func (r *dynamicResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.meta.Name
}

func (r *dynamicResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	s, infos, diags := buildResourceSchema(r.meta.Name, r.meta.Attributes)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.infos = infos
	resp.Schema = s
}

func (r *dynamicResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*httpclient.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *httpclient.Client, got %T", req.ProviderData))
		return
	}
	r.client = client
}

// requireClient returns true if the client is available, or adds a diagnostic
// error and returns false. Call at the top of each CRUD method.
func (r *dynamicResource) requireClient(diags *diag.Diagnostics) bool {
	if r.client != nil {
		return true
	}
	diags.AddError("Resource not configured",
		"The provider has not been configured. Ensure the provider block is present and valid.")
	return false
}

///////////////////////////////////////////////////////////////////////////////
// CRUD

func (r *dynamicResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if !r.requireClient(&resp.Diagnostics) {
		return
	}

	label := generateLabel()
	fullName := r.fullName(label)

	// Create the instance on the server
	_, err := r.client.CreateResourceInstance(ctx, schema.CreateResourceInstanceRequest{
		Name: fullName,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create resource instance", err.Error())
		return
	}

	// Extract desired attributes from the plan and apply them
	attrs := r.extractAttrs(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		if _, err := r.client.DestroyResourceInstance(ctx, fullName, false); err != nil {
			resp.Diagnostics.AddWarning("Cleanup failed",
				fmt.Sprintf("Instance %s was created but attribute extraction failed. "+
					"Attempted to destroy the instance but cleanup also failed: %s. "+
					"The instance may need manual removal.", fullName, err))
		}
		return
	}

	if len(attrs) > 0 {
		_, err := r.client.UpdateResourceInstance(ctx, fullName, schema.UpdateResourceInstanceRequest{
			Attributes: attrs,
			Apply:      true,
		})
		if err != nil {
			if _, cleanupErr := r.client.DestroyResourceInstance(ctx, fullName, false); cleanupErr != nil {
				resp.Diagnostics.AddWarning("Cleanup failed",
					fmt.Sprintf("Instance %s was created but applying attributes failed. "+
						"Attempted to destroy the instance but cleanup also failed: %s. "+
						"The instance may need manual removal.", fullName, cleanupErr))
			}
			resp.Diagnostics.AddError("Failed to apply attributes", err.Error())
			return
		}
	}

	// Read back the full state from the server
	r.writeState(ctx, fullName, &resp.State, &resp.Diagnostics, attrs)
}

func (r *dynamicResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if !r.requireClient(&resp.Diagnostics) {
		return
	}

	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.writeState(ctx, id.ValueString(), &resp.State, &resp.Diagnostics, nil)
}

func (r *dynamicResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if !r.requireClient(&resp.Diagnostics) {
		return
	}

	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fullName := id.ValueString()

	// Extract desired attributes and apply them
	attrs := r.extractAttrs(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpdateResourceInstance(ctx, fullName, schema.UpdateResourceInstanceRequest{
		Attributes: attrs,
		Apply:      true,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update resource instance", err.Error())
		return
	}

	r.writeState(ctx, fullName, &resp.State, &resp.Diagnostics, attrs)
}

func (r *dynamicResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if !r.requireClient(&resp.Diagnostics) {
		return
	}

	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.DestroyResourceInstance(ctx, id.ValueString(), false)
	if err != nil {
		resp.Diagnostics.AddError("Failed to destroy resource instance", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *dynamicResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import by fully qualified name (e.g. "httpstatic.docs").
	parts := strings.SplitN(req.ID, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected format \"resource_type.label\" (e.g. \"httpstatic.docs\"), got %q", req.ID))
		return
	}

	if parts[0] != r.meta.Name {
		resp.Diagnostics.AddError("Resource type mismatch",
			fmt.Sprintf("Import ID %q has resource type %q, but this resource block is kaiak_%s. "+
				"Use the matching resource type or correct the import ID.", req.ID, parts[0], r.meta.Name))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
}

///////////////////////////////////////////////////////////////////////////////
// PRIVATE — extract terraform plan/config → kaiak State

// extractAttrs reads all non-readonly kaiak attributes from a terraform
// plan (or config). Block attributes are read by fetching the parent
// object first, then extracting individual fields.
func (r *dynamicResource) extractAttrs(ctx context.Context, src attrGetter, diags *diag.Diagnostics) schema.State {
	state := make(schema.State)

	// Top-level attributes
	for _, info := range r.getInfos() {
		if info.attr.ReadOnly || info.tfBlock != "" {
			continue
		}
		extractSingleAttr(ctx, src, path.Root(info.tfField), info, state, diags)
	}

	// Block attributes — group by block name
	blockGroups := map[string][]attrInfo{}
	for _, info := range r.getInfos() {
		if info.attr.ReadOnly || info.tfBlock == "" {
			continue
		}
		blockGroups[info.tfBlock] = append(blockGroups[info.tfBlock], info)
	}

	for blockName, infos := range blockGroups {
		var block types.Object
		diags.Append(src.GetAttribute(ctx, path.Root(blockName), &block)...)
		if block.IsNull() || block.IsUnknown() {
			continue
		}
		attrs := block.Attributes()
		for _, info := range infos {
			v, ok := attrs[info.tfField]
			if !ok {
				continue
			}
			extractBlockAttr(info, v, state)
		}
	}

	return state
}

///////////////////////////////////////////////////////////////////////////////
// PRIVATE — kaiak State → terraform state

// writeState fetches the instance from the server and populates
// the terraform state with the id and all resource attributes.
// For writable attributes not present in the server state, the value
// from plannedAttrs (the Go values extracted from the plan) is preserved
// so Terraform's consistency check does not fail.
func (r *dynamicResource) writeState(ctx context.Context, fullName string, tfState *tfsdk.State, diags *diag.Diagnostics, plannedAttrs schema.State) {
	result, err := r.client.GetResourceInstance(ctx, fullName)
	if err != nil {
		diags.AddError("Failed to read resource instance", err.Error())
		return
	}

	kaiakState := result.Instance.State

	// Fixed attributes
	diags.Append(tfState.SetAttribute(ctx, path.Root("id"), types.StringValue(fullName))...)

	// Merge: server state wins, then fall back to planned values for writable attrs
	merged := make(schema.State, len(kaiakState))
	for k, v := range kaiakState {
		merged[k] = v
	}
	if plannedAttrs != nil {
		for _, info := range r.getInfos() {
			if info.attr.ReadOnly {
				continue
			}
			if _, ok := merged[info.kaiakName]; !ok {
				if pv, ok := plannedAttrs[info.kaiakName]; ok {
					merged[info.kaiakName] = pv
				}
			}
		}
	}

	// Top-level attributes
	for _, info := range r.getInfos() {
		if info.tfBlock != "" {
			continue
		}
		v := merged[info.kaiakName]
		diags.Append(tfState.SetAttribute(ctx, path.Root(info.tfField), kaiakValueToTF(ctx, v, info.attr.Type))...)
	}

	// Block attributes — set each block as a typed object
	blockGroups := map[string][]attrInfo{}
	for _, info := range r.getInfos() {
		if info.tfBlock == "" {
			continue
		}
		blockGroups[info.tfBlock] = append(blockGroups[info.tfBlock], info)
	}

	for blockName, infos := range blockGroups {
		attrTypes := make(map[string]attr.Type, len(infos))
		attrValues := make(map[string]attr.Value, len(infos))
		hasValue := false

		for _, info := range infos {
			attrTypes[info.tfField] = kaiakTypeToAttrType(info.attr.Type)
			if v, ok := merged[info.kaiakName]; ok && v != nil {
				hasValue = true
				attrValues[info.tfField] = kaiakValueToTF(ctx, v, info.attr.Type)
			} else {
				attrValues[info.tfField] = kaiakNullValue(info.attr.Type)
			}
		}

		if hasValue {
			obj, d := types.ObjectValue(attrTypes, attrValues)
			diags.Append(d...)
			diags.Append(tfState.SetAttribute(ctx, path.Root(blockName), obj)...)
		} else {
			diags.Append(tfState.SetAttribute(ctx, path.Root(blockName), types.ObjectNull(attrTypes))...)
		}
	}
}

///////////////////////////////////////////////////////////////////////////////
// PRIVATE — attribute extraction helpers

// extractSingleAttr reads a single top-level terraform attribute and stores
// the Go value into the kaiak state map, handling all supported types.
func extractSingleAttr(ctx context.Context, src attrGetter, p path.Path, info attrInfo, state schema.State, diags *diag.Diagnostics) {
	switch {
	case info.attr.Type == "bool":
		var v types.Bool
		diags.Append(src.GetAttribute(ctx, p, &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			state[info.kaiakName] = v.ValueBool()
		}
	case info.attr.Type == "int" || info.attr.Type == "uint":
		var v types.Int64
		diags.Append(src.GetAttribute(ctx, p, &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			state[info.kaiakName] = v.ValueInt64()
		}
	case info.attr.Type == "float":
		var v types.Float64
		diags.Append(src.GetAttribute(ctx, p, &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			state[info.kaiakName] = v.ValueFloat64()
		}
	case strings.HasPrefix(info.attr.Type, "[]"):
		var v types.List
		diags.Append(src.GetAttribute(ctx, p, &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			state[info.kaiakName] = tfListToKaiak(v, info.attr.Type[2:])
		}
	case strings.HasPrefix(info.attr.Type, "map["):
		var v types.Map
		diags.Append(src.GetAttribute(ctx, p, &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			if idx := strings.Index(info.attr.Type, "]"); idx >= 0 && idx+1 < len(info.attr.Type) {
				state[info.kaiakName] = tfMapToKaiak(v, info.attr.Type[idx+1:])
			}
		}
	default:
		var v types.String
		diags.Append(src.GetAttribute(ctx, p, &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			state[info.kaiakName] = v.ValueString()
		}
	}
}

// extractBlockAttr reads a single attribute from a block object value and
// stores the Go value into the kaiak state map.
func extractBlockAttr(info attrInfo, v attr.Value, state schema.State) {
	switch {
	case info.attr.Type == "bool":
		if bv, ok := v.(types.Bool); ok && !bv.IsNull() && !bv.IsUnknown() {
			state[info.kaiakName] = bv.ValueBool()
		}
	case info.attr.Type == "int" || info.attr.Type == "uint":
		if iv, ok := v.(types.Int64); ok && !iv.IsNull() && !iv.IsUnknown() {
			state[info.kaiakName] = iv.ValueInt64()
		}
	case info.attr.Type == "float":
		if fv, ok := v.(types.Float64); ok && !fv.IsNull() && !fv.IsUnknown() {
			state[info.kaiakName] = fv.ValueFloat64()
		}
	case strings.HasPrefix(info.attr.Type, "[]"):
		if lv, ok := v.(types.List); ok && !lv.IsNull() && !lv.IsUnknown() {
			state[info.kaiakName] = tfListToKaiak(lv, info.attr.Type[2:])
		}
	case strings.HasPrefix(info.attr.Type, "map["):
		if mv, ok := v.(types.Map); ok && !mv.IsNull() && !mv.IsUnknown() {
			if idx := strings.Index(info.attr.Type, "]"); idx >= 0 && idx+1 < len(info.attr.Type) {
				state[info.kaiakName] = tfMapToKaiak(mv, info.attr.Type[idx+1:])
			}
		}
	default:
		if sv, ok := v.(types.String); ok && !sv.IsNull() && !sv.IsUnknown() {
			state[info.kaiakName] = sv.ValueString()
		}
	}
}

// tfListToKaiak converts a terraform ListValue to a Go slice for the kaiak API.
func tfListToKaiak(list types.List, elemType string) []interface{} {
	elems := list.Elements()
	result := make([]interface{}, 0, len(elems))
	for _, e := range elems {
		result = append(result, tfElemToGo(e, elemType))
	}
	return result
}

// tfMapToKaiak converts a terraform MapValue to a Go map for the kaiak API.
func tfMapToKaiak(m types.Map, valType string) map[string]interface{} {
	elems := m.Elements()
	result := make(map[string]interface{}, len(elems))
	for k, e := range elems {
		result[k] = tfElemToGo(e, valType)
	}
	return result
}

// tfElemToGo converts a terraform attr.Value to its Go equivalent for a
// given kaiak type string.
func tfElemToGo(v attr.Value, t string) interface{} {
	switch t {
	case "bool":
		if bv, ok := v.(types.Bool); ok {
			return bv.ValueBool()
		}
	case "int", "uint":
		if iv, ok := v.(types.Int64); ok {
			return iv.ValueInt64()
		}
	case "float":
		if fv, ok := v.(types.Float64); ok {
			return fv.ValueFloat64()
		}
	}
	if sv, ok := v.(types.String); ok {
		return sv.ValueString()
	}
	return fmt.Sprintf("%v", v)
}
