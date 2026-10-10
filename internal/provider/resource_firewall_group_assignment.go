package provider

import (
	"context"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = (*firewallAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*firewallAssignmentResource)(nil)
)

type firewallAssignmentResource struct{ client *client.Client }

type firewallAssignmentModel struct {
	ID              types.String `tfsdk:"id"`
	FirewallGroupID types.String `tfsdk:"firewall_group_id"`
	Address         types.String `tfsdk:"address"`
}

func NewFirewallGroupAssignmentResource() resource.Resource { return &firewallAssignmentResource{} }

func (r *firewallAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_group_assignment"
}

func (r *firewallAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Applies a firewall group to one of the organization's IP addresses. An address can carry several groups; " +
			"their rules are matched together by priority.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "<firewall_group_id>/<address>",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"firewall_group_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"address": schema.StringAttribute{
				Description:   "The IPv4 or IPv6 address.",
				Required:      true,
				PlanModifiers: replace,
			},
		},
	}
}

func (r *firewallAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func (r *firewallAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, ip := plan.FirewallGroupID.ValueString(), plan.Address.ValueString()
	if err := r.client.AssignFirewallGroup(ctx, group, ip); err != nil {
		addAPIError(&resp.Diagnostics, "assign firewall group", err)
		return
	}
	plan.ID = types.StringValue(group + "/" + ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *firewallAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, err := r.client.GetFirewallGroup(ctx, state.FirewallGroupID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read firewall group", err)
		return
	}
	if !containsAddress(group.AssignedAddresses, state.Address.ValueString()) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *firewallAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every attribute forces replacement.
	var plan firewallAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *firewallAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.UnassignFirewallGroup(ctx, state.FirewallGroupID.ValueString(), state.Address.ValueString())
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "unassign firewall group", err)
	}
}

func (r *firewallAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("firewall_group_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("address"), parts[1])...)
}

// containsAddress compares parsed addresses, so the API may spell an IPv6
// address differently from the configuration.
func containsAddress(list []string, address string) bool {
	want, err := netip.ParseAddr(address)
	for _, a := range list {
		if a == address {
			return true
		}
		if got, e := netip.ParseAddr(a); err == nil && e == nil && got == want {
			return true
		}
	}
	return false
}
