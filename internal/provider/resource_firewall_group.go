package provider

import (
	"context"
	"net/netip"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = (*firewallGroupResource)(nil)
	_ resource.ResourceWithImportState = (*firewallGroupResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*firewallGroupResource)(nil)
)

type firewallGroupResource struct{ client *client.Client }

type firewallGroupModel struct {
	ID      types.String        `tfsdk:"id"`
	Name    types.String        `tfsdk:"name"`
	Rules   []firewallRuleModel `tfsdk:"rules"`
	RuleIDs types.List          `tfsdk:"rule_ids"`
}

type firewallRuleModel struct {
	Action               types.String `tfsdk:"action"`
	Direction            types.String `tfsdk:"direction"`
	Protocol             types.String `tfsdk:"protocol"`
	SourceAddress        types.String `tfsdk:"source_address"`
	SourcePortStart      types.Int64  `tfsdk:"source_port_start"`
	SourcePortEnd        types.Int64  `tfsdk:"source_port_end"`
	DestinationPortStart types.Int64  `tfsdk:"destination_port_start"`
	DestinationPortEnd   types.Int64  `tfsdk:"destination_port_end"`
	RateLimitPps         types.Int64  `tfsdk:"rate_limit_pps"`
	MinPacketLength      types.Int64  `tfsdk:"min_packet_length"`
	MaxPacketLength      types.Int64  `tfsdk:"max_packet_length"`
	Description          types.String `tfsdk:"description"`
	Enabled              types.Bool   `tfsdk:"enabled"`
}

func NewFirewallGroupResource() resource.Resource { return &firewallGroupResource{} }

func (r *firewallGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_group"
}

func portAttr(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{
		Description: desc,
		Optional:    true,
		Validators:  []validator.Int64{int64validator.Between(1, 65535)},
	}
}

func (r *firewallGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A firewall group: an ordered list of rules applied to the IP addresses it is assigned to " +
			"(see serverside_firewall_group_assignment). Rules on an address are matched by priority, the first match " +
			"decides, and traffic that no rule matches is allowed, so a group of ALLOW rules alone filters nothing. " +
			"Rules are stateless.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 50)},
			},
			"rules": schema.ListNestedAttribute{
				Description: "Rules in priority order: the first rule in the list is matched first.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"action": schema.StringAttribute{
							Description: "ALLOW, BLOCK or RATE_LIMIT. RATE_LIMIT needs rate_limit_pps.",
							Required:    true,
							Validators:  []validator.String{stringvalidator.OneOf("ALLOW", "BLOCK", "RATE_LIMIT")},
						},
						"direction": schema.StringAttribute{
							Description: "INBOUND (default) or OUTBOUND. The API stores OUTBOUND rules, but whether they are enforced is not confirmed; do not rely on them.",
							Optional:    true,
							Computed:    true,
							Default:     stringdefault.StaticString("INBOUND"),
							Validators:  []validator.String{stringvalidator.OneOf("INBOUND", "OUTBOUND")},
						},
						"protocol": schema.StringAttribute{
							Description: "ANY, TCP, UDP, ICMP, ICMP_V6 or GRE.",
							Required:    true,
							Validators:  []validator.String{stringvalidator.OneOf("ANY", "TCP", "UDP", "ICMP", "ICMP_V6", "GRE")},
						},
						"source_address": schema.StringAttribute{
							Description: "Source IP address or CIDR prefix. Omit to match any source.",
							Optional:    true,
						},
						"source_port_start":      portAttr("First source port of the range (TCP and UDP only)."),
						"source_port_end":        portAttr("Last source port of the range."),
						"destination_port_start": portAttr("First destination port of the range (TCP and UDP only)."),
						"destination_port_end":   portAttr("Last destination port of the range."),
						"rate_limit_pps": schema.Int64Attribute{
							Description: "Packets per second allowed through a RATE_LIMIT rule.",
							Optional:    true,
							Validators:  []validator.Int64{int64validator.AtLeast(1)},
						},
						"min_packet_length": schema.Int64Attribute{Description: "Match only packets at least this long, in bytes.", Optional: true},
						"max_packet_length": schema.Int64Attribute{Description: "Match only packets at most this long, in bytes.", Optional: true},
						"description": schema.StringAttribute{
							Optional:   true,
							Validators: []validator.String{stringvalidator.LengthAtMost(100)},
						},
						"enabled": schema.BoolAttribute{
							Description: "Set to false to switch the rule off without removing it. Defaults to true.",
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(true),
						},
					},
				},
			},
			"rule_ids": schema.ListAttribute{
				Description: "API ids of the rules, in the same order as rules.",
				Computed:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (r *firewallGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

// ModifyPlan keeps rule_ids known when the rules do not change, so an
// unrelated edit (a rename) does not show them as "known after apply".
func (r *firewallGroupResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state firewallGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if reflect.DeepEqual(plan.Rules, state.Rules) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("rule_ids"), state.RuleIDs)...)
	}
}

// ruleSpec converts a configured rule into the API's shape.
func ruleSpec(m firewallRuleModel) client.FirewallRuleSpec {
	s := client.FirewallRuleSpec{
		Type:            m.Action.ValueString(),
		Direction:       m.Direction.ValueString(),
		Protocol:        m.Protocol.ValueString(),
		SourceAddress:   stringPtr(m.SourceAddress),
		SourcePortStart: int64Ptr(m.SourcePortStart),
		SourcePortEnd:   int64Ptr(m.SourcePortEnd),
		DestPortStart:   int64Ptr(m.DestinationPortStart),
		DestPortEnd:     int64Ptr(m.DestinationPortEnd),
		RateLimitPps:    int64Ptr(m.RateLimitPps),
		Description:     stringPtr(m.Description),
	}
	if min, max := int64Ptr(m.MinPacketLength), int64Ptr(m.MaxPacketLength); min != nil || max != nil {
		s.PacketCriteria = &client.PacketCriteria{MinPacketLength: min, MaxPacketLength: max}
	}
	if s.Direction == "" {
		s.Direction = "INBOUND"
	}
	return s
}

// normalizeSpec makes two specs comparable: empty strings and empty packet
// criteria become nil, and addresses are compared as canonical prefixes.
func normalizeSpec(s client.FirewallRuleSpec) client.FirewallRuleSpec {
	if s.Description != nil && *s.Description == "" {
		s.Description = nil
	}
	if s.SourceAddress != nil {
		v := *s.SourceAddress
		switch {
		case v == "":
			s.SourceAddress = nil
		default:
			if p, err := netip.ParsePrefix(v); err == nil {
				v = p.Masked().String()
			} else if a, err := netip.ParseAddr(v); err == nil {
				v = netip.PrefixFrom(a, a.BitLen()).String()
			}
			s.SourceAddress = &v
		}
	}
	if s.PacketCriteria != nil && s.PacketCriteria.MinPacketLength == nil && s.PacketCriteria.MaxPacketLength == nil {
		s.PacketCriteria = nil
	}
	return s
}

func ruleModelFromAPI(r client.FirewallRule) firewallRuleModel {
	m := firewallRuleModel{
		Action:               types.StringValue(r.Type),
		Direction:            types.StringValue(r.Direction),
		Protocol:             types.StringValue(r.Protocol),
		SourceAddress:        stringValue(r.SourceAddress),
		SourcePortStart:      int64Value(r.SourcePortStart),
		SourcePortEnd:        int64Value(r.SourcePortEnd),
		DestinationPortStart: int64Value(r.DestPortStart),
		DestinationPortEnd:   int64Value(r.DestPortEnd),
		RateLimitPps:         int64Value(r.RateLimitPps),
		MinPacketLength:      types.Int64Null(),
		MaxPacketLength:      types.Int64Null(),
		Description:          stringValue(r.Description),
		Enabled:              types.BoolValue(r.IsEnabled),
	}
	if r.PacketCriteria != nil {
		m.MinPacketLength = int64Value(r.PacketCriteria.MinPacketLength)
		m.MaxPacketLength = int64Value(r.PacketCriteria.MaxPacketLength)
	}
	return m
}

func sortedRules(g *client.FirewallGroup) []client.FirewallRule {
	rules := append([]client.FirewallRule(nil), g.Rules...)
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Priority < rules[j].Priority })
	return rules
}

func idList(ids []string) (types.List, diag.Diagnostics) {
	vals := make([]attr.Value, len(ids))
	for i, id := range ids {
		vals[i] = types.StringValue(id)
	}
	return types.ListValue(types.StringType, vals)
}

// applyRules makes the group's rules match desired and returns the rule ids
// in the desired order.
func (r *firewallGroupResource) applyRules(ctx context.Context, groupID string, current []client.FirewallRule, desiredModels []firewallRuleModel) ([]string, error) {
	existing := make([]existingRule, len(current))
	for i, c := range current {
		existing[i] = existingRule{ID: c.ID, Spec: normalizeSpec(c.FirewallRuleSpec), Enabled: c.IsEnabled}
	}
	desired := make([]desiredRule, len(desiredModels))
	for i, m := range desiredModels {
		desired[i] = desiredRule{Spec: normalizeSpec(ruleSpec(m)), Enabled: m.Enabled.IsNull() || m.Enabled.ValueBool()}
	}
	p := planRules(existing, desired)

	for _, id := range p.Deletes {
		if err := r.client.DeleteFirewallRule(ctx, groupID, id); err != nil && !client.IsNotFound(err) {
			return nil, err
		}
	}
	for _, u := range p.Updates {
		d := desired[u.Desired]
		if _, err := r.client.UpdateFirewallRule(ctx, groupID, u.ID, ruleSpec(desiredModels[u.Desired]), d.Enabled); err != nil {
			return nil, err
		}
	}
	var createdIDs []string
	if len(p.Creates) > 0 {
		specs := make([]client.FirewallRuleSpec, len(p.Creates))
		for i, d := range p.Creates {
			specs[i] = ruleSpec(desiredModels[d])
		}
		created, err := r.client.CreateFirewallRules(ctx, groupID, specs)
		if err != nil {
			return nil, err
		}
		for _, c := range created {
			createdIDs = append(createdIDs, c.ID)
		}
		// The create request has no enabled flag; switch off the rules that
		// are configured as disabled.
		for i, d := range p.Creates {
			if i < len(created) && !desired[d].Enabled {
				if _, err := r.client.UpdateFirewallRule(ctx, groupID, created[i].ID, specs[i], false); err != nil {
					return nil, err
				}
			}
		}
	}

	order := make([]string, len(p.Order))
	for i, slot := range p.Order {
		if slot.ExistingID != "" {
			order[i] = slot.ExistingID
		} else if slot.Created < len(createdIDs) {
			order[i] = createdIDs[slot.Created]
		}
	}
	if p.NeedsReorder && len(order) > 0 {
		if err := r.client.UpdateFirewallGroup(ctx, groupID, nil, order); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (r *firewallGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, err := r.client.CreateFirewallGroup(ctx, plan.Name.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "create firewall group", err)
		return
	}
	plan.ID = types.StringValue(group.ID)

	ids, err := r.applyRules(ctx, group.ID, nil, plan.Rules)
	if err != nil {
		// Keep the group in state so the next apply retries the rules
		// instead of creating a second group.
		plan.RuleIDs = types.ListNull(types.StringType)
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		addAPIError(&resp.Diagnostics, "create firewall rules", err)
		return
	}
	list, d := idList(ids)
	resp.Diagnostics.Append(d...)
	plan.RuleIDs = list
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *firewallGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, err := r.client.GetFirewallGroup(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read firewall group", err)
		return
	}
	state.Name = types.StringValue(group.Name)

	rules := sortedRules(group)
	ids := make([]string, len(rules))
	var models []firewallRuleModel
	for i, rule := range rules {
		ids[i] = rule.ID
		m := ruleModelFromAPI(rule)
		// Keep the configured spelling (for example "10.0.0.1" for
		// "10.0.0.1/32") when the stored rule means the same thing.
		if i < len(state.Rules) {
			prev := state.Rules[i]
			if prev.Enabled.ValueBool() == rule.IsEnabled && reflect.DeepEqual(normalizeSpec(ruleSpec(prev)), normalizeSpec(rule.FirewallRuleSpec)) {
				m = prev
			}
		}
		models = append(models, m)
	}
	if len(models) == 0 && state.Rules == nil {
		state.Rules = nil
	} else {
		state.Rules = models
	}
	list, d := idList(ids)
	resp.Diagnostics.Append(d...)
	state.RuleIDs = list
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *firewallGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state firewallGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	plan.ID = state.ID

	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		if err := r.client.UpdateFirewallGroup(ctx, id, &name, nil); err != nil {
			addAPIError(&resp.Diagnostics, "rename firewall group", err)
			return
		}
	}

	// Work from the stored rules, not from state, so rules changed outside
	// Terraform are corrected rather than overwritten blindly.
	group, err := r.client.GetFirewallGroup(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "read firewall group", err)
		return
	}
	ids, err := r.applyRules(ctx, id, sortedRules(group), plan.Rules)
	if err != nil {
		addAPIError(&resp.Diagnostics, "update firewall rules", err)
		return
	}
	list, d := idList(ids)
	resp.Diagnostics.Append(d...)
	plan.RuleIDs = list
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *firewallGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteFirewallGroup(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete firewall group", err)
	}
}

func (r *firewallGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
