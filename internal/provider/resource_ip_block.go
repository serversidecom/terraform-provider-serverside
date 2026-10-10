package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = (*ipBlockResource)(nil)
	_ resource.ResourceWithImportState = (*ipBlockResource)(nil)
)

type ipBlockResource struct{ client *client.Client }

type ipBlockModel struct {
	ID             types.String `tfsdk:"id"`
	PlanID         types.String `tfsdk:"plan_id"`
	DatacenterID   types.String `tfsdk:"datacenter_id"`
	NetworkID      types.String `tfsdk:"network_id"`
	SubscriptionID types.String `tfsdk:"subscription_id"`
	IPVersion      types.String `tfsdk:"ip_version"`
	Prefix         types.String `tfsdk:"prefix"`
	PrefixLength   types.Int64  `tfsdk:"prefix_length"`
	Gateway        types.String `tfsdk:"gateway"`
}

func NewIPBlockResource() resource.Resource { return &ipBlockResource{} }

func (r *ipBlockResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_block"
}

func (r *ipBlockResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "An hourly-billed IP block added to a public virtual network. Ordering one needs the organization's " +
			"usage-based billing entitlement (the API answers 422 USAGE_BILLING_DISABLED without it). Destroying the " +
			"resource requests cancellation of the block's subscription; an unassigned block is released at once.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Description: "Id of the IP block.", Computed: true, PlanModifiers: keep},
			"plan_id": schema.StringAttribute{
				Description: "IP block plan, from the serverside_ip_block_plans data source. Changing it replaces the block.",
				Required:    true,
				// An imported block has no plan_id in state (the API cannot
				// report it), so filling it in must not force a replacement.
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull()
					},
					"Replaces the IP block when the plan changes.",
					"Replaces the IP block when the plan changes.",
				)},
			},
			"datacenter_id":   schema.StringAttribute{Description: "Datacenter from serverside_datacenters, by id or code (such as ams1).", Required: true, PlanModifiers: replace},
			"network_id":      schema.StringAttribute{Description: "Public virtual network to add the block to.", Required: true, PlanModifiers: replace},
			"subscription_id": schema.StringAttribute{Description: "Subscription that bills the block.", Computed: true, PlanModifiers: keep},
			"ip_version":      schema.StringAttribute{Description: "V4 or V6.", Computed: true, PlanModifiers: keep},
			"prefix":          schema.StringAttribute{Description: "The allocated network, for example 198.51.100.0/29.", Computed: true, PlanModifiers: keep},
			"prefix_length": schema.Int64Attribute{
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"gateway": schema.StringAttribute{Computed: true, PlanModifiers: keep},
		},
	}
}

func (r *ipBlockResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

// setFromBlock fills the attributes the API reports.
func setFromBlock(m *ipBlockModel, b *client.IPBlock) {
	m.ID = types.StringValue(b.ID)
	m.NetworkID = types.StringValue(b.NetworkID)
	// Keep a datacenter given by its code rather than its id.
	if dc := m.DatacenterID.ValueString(); dc != b.Datacenter.ID && dc != b.Datacenter.Code {
		m.DatacenterID = types.StringValue(b.Datacenter.ID)
	}
	m.IPVersion = types.StringValue(b.IPVersion)
	m.Prefix = types.StringValue(b.CIDR)
	m.PrefixLength = types.Int64Value(b.PrefixLength)
	m.Gateway = stringValue(b.Gateway)
}

// setSubscription looks up the subscription id, which the block itself does
// not carry. A failure only warns: the block is there either way.
func (r *ipBlockResource) setSubscription(ctx context.Context, m *ipBlockModel, d *diag.Diagnostics) {
	sub, err := r.client.GetIPBlockSubscription(ctx, m.ID.ValueString())
	if err != nil {
		m.SubscriptionID = types.StringNull()
		d.AddWarning("Serverside.com API: read IP block subscription", err.Error())
		return
	}
	m.SubscriptionID = types.StringValue(sub.ID)
}

func (r *ipBlockResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ipBlockModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	block, err := r.client.CreateIPBlock(ctx, plan.PlanID.ValueString(), plan.DatacenterID.ValueString(), plan.NetworkID.ValueString(), client.NewIdempotencyKey())
	if err != nil {
		if client.HasCode(err, "USAGE_BILLING_DISABLED") {
			resp.Diagnostics.AddError("Hourly billing is not enabled for this organization",
				"IP blocks are ordered on hourly billing, which needs the billing.usage_based entitlement. "+err.Error())
			return
		}
		addAPIError(&resp.Diagnostics, "create IP block", err)
		return
	}
	setFromBlock(&plan, block)
	r.setSubscription(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ipBlockResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ipBlockModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	block, err := r.client.GetIPBlock(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read IP block", err)
		return
	}
	setFromBlock(&state, block)
	if state.SubscriptionID.IsNull() {
		r.setSubscription(ctx, &state, &resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ipBlockResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// The only in-place change is filling in plan_id after an import.
	var plan, state ipBlockModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.PlanID = plan.PlanID
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ipBlockResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ipBlockModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteIPBlock(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "cancel IP block", err)
	}
}

// ImportState takes the block id; plan_id cannot be read back and has to
// match the configuration.
func (r *ipBlockResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
