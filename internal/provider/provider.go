// Package provider implements the Serverside.com Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

const (
	envAPIKey = "SERVERSIDE_API_KEY"
	envAPIURL = "SERVERSIDE_API_URL"
)

var _ provider.Provider = (*serversideProvider)(nil)

type serversideProvider struct {
	version string
}

type providerModel struct {
	APIKey types.String `tfsdk:"api_key"`
	APIURL types.String `tfsdk:"api_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &serversideProvider{version: version} }
}

func (p *serversideProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "serverside"
	resp.Version = p.version
}

func (p *serversideProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages Serverside.com bare metal networking, firewall groups and SSH keys through the Serverside.com API.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Description: "Organization API key, created in the cloud console at https://cloud.serverside.com. Can also be set with the " + envAPIKey + " environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"api_url": schema.StringAttribute{
				Description: "Base URL of the API. Defaults to " + client.DefaultBaseURL + "; can also be set with the " + envAPIURL + " environment variable.",
				Optional:    true,
			},
		},
	}
}

func (p *serversideProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.APIKey.IsUnknown() || cfg.APIURL.IsUnknown() {
		resp.Diagnostics.AddError("Unknown provider configuration",
			"api_key and api_url must be known when the provider is configured; they cannot come from a resource created in the same apply.")
		return
	}

	apiKey := os.Getenv(envAPIKey)
	if !cfg.APIKey.IsNull() {
		apiKey = cfg.APIKey.ValueString()
	}
	apiURL := os.Getenv(envAPIURL)
	if !cfg.APIURL.IsNull() {
		apiURL = cfg.APIURL.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Missing API key",
			"Set api_key in the provider block or the "+envAPIKey+" environment variable.")
		return
	}

	c, err := client.New(apiURL, apiKey, client.WithUserAgent("terraform-provider-serverside/"+p.version))
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *serversideProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSSHKeyResource,
		NewFirewallGroupResource,
		NewFirewallGroupAssignmentResource,
		NewVirtualNetworkResource,
		NewVirtualNetworkAttachmentResource,
		NewIPBlockResource,
	}
}

func (p *serversideProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDatacentersDataSource,
		NewBaremetalPlansDataSource,
		NewIPBlockPlansDataSource,
		NewBaremetalServerDataSource,
		NewIPAddressDataSource,
	}
}
