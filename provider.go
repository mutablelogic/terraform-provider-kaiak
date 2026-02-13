package main

import (
	"context"
	"os"

	// Packages
	datasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	provider "github.com/hashicorp/terraform-plugin-framework/provider"
	tfschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	resource "github.com/hashicorp/terraform-plugin-framework/resource"
	types "github.com/hashicorp/terraform-plugin-framework/types"
	tflog "github.com/hashicorp/terraform-plugin-log/tflog"
	client "github.com/mutablelogic/go-client"
	httpclient "github.com/mutablelogic/go-server/pkg/provider/httpclient"
	schema "github.com/mutablelogic/go-server/pkg/provider/schema"
)

///////////////////////////////////////////////////////////////////////////////
// TYPES

// kaiakProvider implements the Terraform provider for a running Kaiak server.
type kaiakProvider struct {
	version  string
	endpoint string // resolved during Configure; used by Resources for discovery
	apiKey   string // resolved during Configure; used by Resources for discovery
}

// kaiakProviderModel maps provider schema data to a Go type.
type kaiakProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	ApiKey   types.String `tfsdk:"api_key"`
}

var _ provider.Provider = (*kaiakProvider)(nil)

///////////////////////////////////////////////////////////////////////////////
// LIFECYCLE

// New returns a provider factory that creates a new provider instance
// with the given version. It is called by the plugin framework.
func New(v string) func() provider.Provider {
	return func() provider.Provider {
		return &kaiakProvider{version: v}
	}
}

// resolveEndpoint returns the API endpoint from the environment, falling
// back to a sensible default.
func resolveEndpoint() string {
	if v := os.Getenv("KAIAK_ENDPOINT"); v != "" {
		return v
	}
	return "http://localhost:8084/api"
}

// resolveApiKey returns the API key from the environment, or empty string.
func resolveApiKey() string {
	return os.Getenv("KAIAK_API_KEY")
}

///////////////////////////////////////////////////////////////////////////////
// PROVIDER INTERFACE

func (p *kaiakProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kaiak"
	resp.Version = p.version
}

func (p *kaiakProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = tfschema.Schema{
		Description: "Manage resources on a running Kaiak server.",
		Attributes: map[string]tfschema.Attribute{
			"endpoint": tfschema.StringAttribute{
				Description: "Base URL of the Kaiak server API (e.g. http://localhost:8084/api). " +
					"Can also be set via the KAIAK_ENDPOINT environment variable.",
				Optional: true,
			},
			"api_key": tfschema.StringAttribute{
				Description: "API key (bearer token) for authenticating with the Kaiak server. " +
					"Can also be set via the KAIAK_API_KEY environment variable.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (p *kaiakProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config kaiakProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve endpoint: config value > environment variable > default
	endpoint := config.Endpoint.ValueString()
	if endpoint == "" {
		endpoint = resolveEndpoint()
	}

	// Resolve API key: config value > environment variable
	apiKey := config.ApiKey.ValueString()
	if apiKey == "" {
		apiKey = resolveApiKey()
	}

	// Cache resolved values so Resources() uses the same settings
	p.endpoint = endpoint
	p.apiKey = apiKey

	// Build client options
	var opts []client.ClientOpt
	if apiKey != "" {
		opts = append(opts, client.OptReqToken(client.Token{
			Scheme: client.Bearer,
			Value:  apiKey,
		}))
	}

	// Create the HTTP client
	cl, err := httpclient.New(endpoint, opts...)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Kaiak client", err.Error())
		return
	}

	// Make the client available to resources and data sources
	resp.DataSourceData = cl
	resp.ResourceData = cl
}

// Resources discovers resource types from the running Kaiak server and
// returns a factory for each one. The server must be reachable at schema-
// discovery time (i.e. during terraform plan / apply).
//
// When Configure() has already run, the provider-configured endpoint and
// API key are used. Otherwise (e.g. during validate or early plan phases)
// the values fall back to KAIAK_ENDPOINT / KAIAK_API_KEY env vars.
func (p *kaiakProvider) Resources(ctx context.Context) []func() resource.Resource {
	// Prefer values cached from Configure(); fall back to env vars
	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = resolveEndpoint()
	}

	apiKey := p.apiKey
	if apiKey == "" {
		apiKey = resolveApiKey()
	}

	// Build client options with optional auth token
	var opts []client.ClientOpt
	if apiKey != "" {
		opts = append(opts, client.OptReqToken(client.Token{
			Scheme: client.Bearer,
			Value:  apiKey,
		}))
	}

	cl, err := httpclient.New(endpoint, opts...)
	if err != nil {
		tflog.Error(ctx, "Failed to create Kaiak client. No resources will be available.", map[string]interface{}{
			"endpoint": endpoint,
			"error":    err.Error(),
		})
		return nil
	}

	result, err := cl.ListResources(ctx, schema.ListResourcesRequest{})
	if err != nil {
		tflog.Error(ctx, "Failed to discover resources from Kaiak server. No resources will be available.", map[string]interface{}{
			"endpoint": endpoint,
			"error":    err.Error(),
		})
		return nil
	}

	factories := make([]func() resource.Resource, 0, len(result.Resources))
	for _, r := range result.Resources {
		meta := r // capture
		factories = append(factories, func() resource.Resource {
			return newDynamicResource(meta)
		})
	}
	return factories
}

func (p *kaiakProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewResourcesDataSource,
	}
}
