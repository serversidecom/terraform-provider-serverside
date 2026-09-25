package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = (*virtualNetworkResource)(nil)
	_ resource.ResourceWithImportState = (*virtualNetworkResource)(nil)
)

type virtualNetworkResource struct{ client *client.Client }

type virtualNetworkModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Type      types.String `tfsdk:"type"`
	VlanID    types.Int64  `tfsdk:"vlan_id"`
	ManagedBy types.String `tfsdk:"managed_by"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewVirtualNetworkResource() resource.Resource { return &virtualNetworkResource{} }

func (r *virtualNetworkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_network"
}

func (r *virtualNetworkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A virtual network (VLAN). A PRIVATE network is layer 2 only: the platform assigns it no addresses, so servers on it use " +
			"a private range you choose. A PUBLIC network carries IP blocks (serverside_ip_block). The API refuses to delete a network " +
			"while servers are attached or IP blocks remain on it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Description: "Can be changed in place.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"type": schema.StringAttribute{
				Description:   "PRIVATE (default) or PUBLIC. Changing it replaces the network.",
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("PRIVATE"),
				Validators:    []validator.String{stringvalidator.OneOf("PRIVATE", "PUBLIC")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vlan_id": schema.Int64Attribute{
				Description:   "VLAN tag the network uses on your servers, 11 to 4094 (2 to 10 are reserved). Changing it replaces the network.",
				Required:      true,
				Validators:    []validator.Int64{int64validator.Between(11, 4094)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"managed_by": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *virtualNetworkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func (r *virtualNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan virtualNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	n, err := r.client.CreateNetwork(ctx, plan.Name.ValueString(), plan.Type.ValueString(), plan.VlanID.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "create virtual network", err)
		return
	}
	plan.ID = types.StringValue(n.ID)
	plan.ManagedBy = types.StringValue(n.ManagedBy)
	plan.CreatedAt = types.StringValue(n.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *virtualNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state virtualNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	n, err := r.client.GetNetwork(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read virtual network", err)
		return
	}
	state.Name = types.StringValue(n.Name)
	state.Type = types.StringValue(n.Type)
	state.VlanID = int64Value(n.LocalVlanID)
	state.ManagedBy = types.StringValue(n.ManagedBy)
	state.CreatedAt = types.StringValue(n.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *virtualNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state virtualNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.RenameNetwork(ctx, state.ID.ValueString(), plan.Name.ValueString()); err != nil {
		addAPIError(&resp.Diagnostics, "rename virtual network", err)
		return
	}
	plan.ID, plan.ManagedBy, plan.CreatedAt = state.ID, state.ManagedBy, state.CreatedAt
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *virtualNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state virtualNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteNetwork(ctx, state.ID.ValueString())
	switch {
	case err == nil, client.IsNotFound(err):
	case client.HasCode(err, "NETWORK_HAS_ASSIGNED_SERVICE"):
		resp.Diagnostics.AddError("Virtual network still has servers attached",
			"Detach every server first (remove its serverside_virtual_network_attachment, or detach it in the cloud console). "+err.Error())
	case client.HasCode(err, "NETWORK_HAS_PREFIXES"):
		resp.Diagnostics.AddError("Virtual network still holds IP blocks",
			"Remove the IP blocks on this network first. "+err.Error())
	default:
		addAPIError(&resp.Diagnostics, "delete virtual network", err)
	}
}

func (r *virtualNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
