package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = (*attachmentResource)(nil)
	_ resource.ResourceWithImportState = (*attachmentResource)(nil)
)

// Attach and detach are asynchronous: the API answers 202 and the switch
// change is applied afterwards.
var (
	attachTimeout      = 15 * time.Minute
	attachPollInterval = 5 * time.Second
)

type attachmentResource struct{ client *client.Client }

type attachmentModel struct {
	ID            types.String `tfsdk:"id"`
	NetworkID     types.String `tfsdk:"network_id"`
	ServerID      types.String `tfsdk:"server_id"`
	SegmentID     types.String `tfsdk:"segment_id"`
	Native        types.Bool   `tfsdk:"native"`
	Status        types.String `tfsdk:"status"`
	IPv4Addresses types.List   `tfsdk:"ipv4_addresses"`
	IPv6Addresses types.List   `tfsdk:"ipv6_addresses"`
}

func NewVirtualNetworkAttachmentResource() resource.Resource { return &attachmentResource{} }

func (r *attachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_network_attachment"
}

func (r *attachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "Attaches one network segment (port or bond) of a bare metal server to a virtual network. " +
			"Get the segment id from the serverside_baremetal_server data source. The server must not be locked.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "<network_id>/<server_id>/<segment_id>",
				Computed:      true,
				PlanModifiers: keep,
			},
			"network_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"server_id":  schema.StringAttribute{Description: "Id of the bare metal service.", Required: true, PlanModifiers: replace},
			"segment_id": schema.StringAttribute{Description: "Id of the server's network segment.", Required: true, PlanModifiers: replace},
			"native": schema.BoolAttribute{
				Description:   "Carry the network untagged (native VLAN) on the segment instead of tagged. Defaults to false.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"ipv4_addresses": schema.ListAttribute{
				Description: "Addresses from this network assigned to the server (public networks only).",
				Computed:    true,
				ElementType: types.StringType,
			},
			"ipv6_addresses": schema.ListAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (r *attachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromResource(req, resp)
}

func (r *attachmentResource) setFromAPI(ctx context.Context, m *attachmentModel, a *client.NetworkAttachment) error {
	m.Status = types.StringValue(a.Status)
	v4, d := types.ListValueFrom(ctx, types.StringType, nonNil(a.IPv4Addresses))
	if d.HasError() {
		return fmt.Errorf("convert ipv4 addresses")
	}
	v6, d := types.ListValueFrom(ctx, types.StringType, nonNil(a.IPv6Addresses))
	if d.HasError() {
		return fmt.Errorf("convert ipv6 addresses")
	}
	m.IPv4Addresses, m.IPv6Addresses = v4, v6
	if a.IsNative != nil {
		m.Native = types.BoolValue(*a.IsNative)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// waitFor polls until done reports true, fails, or the timeout passes.
func waitFor(ctx context.Context, timeout time.Duration, check func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out after %s", timeout)
		case <-time.After(attachPollInterval):
		}
	}
}

func (r *attachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan attachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	netID, srvID, segID := plan.NetworkID.ValueString(), plan.ServerID.ValueString(), plan.SegmentID.ValueString()
	if err := r.client.AttachServer(ctx, netID, srvID, segID, plan.Native.ValueBool(), client.NewIdempotencyKey()); err != nil {
		addAPIError(&resp.Diagnostics, "attach server to virtual network", err)
		return
	}
	plan.ID = types.StringValue(netID + "/" + srvID + "/" + segID)

	var att *client.NetworkAttachment
	err := waitFor(ctx, attachTimeout, func() (bool, error) {
		a, err := r.client.FindAttachment(ctx, netID, srvID, segID)
		if err != nil {
			return false, err
		}
		att = a
		if a == nil {
			return false, nil
		}
		switch a.Status {
		case "ATTACHED":
			return true, nil
		case "ATTACH_FAILED":
			return false, fmt.Errorf("the network change failed (status ATTACH_FAILED)")
		}
		return false, nil
	})
	if att != nil {
		if e := r.setFromAPI(ctx, &plan, att); e != nil {
			resp.Diagnostics.AddError("Internal error", e.Error())
		}
	} else {
		plan.Status = types.StringValue("ATTACHING")
		plan.IPv4Addresses = types.ListNull(types.StringType)
		plan.IPv6Addresses = types.ListNull(types.StringType)
	}
	// Save the attachment even on failure so a later apply or destroy can
	// act on it.
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if err != nil {
		addAPIError(&resp.Diagnostics, "wait for virtual network attachment", err)
	}
}

func (r *attachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state attachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.FindAttachment(ctx, state.NetworkID.ValueString(), state.ServerID.ValueString(), state.SegmentID.ValueString())
	if client.IsNotFound(err) || (err == nil && a == nil) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read virtual network attachment", err)
		return
	}
	if e := r.setFromAPI(ctx, &state, a); e != nil {
		resp.Diagnostics.AddError("Internal error", e.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *attachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every configurable attribute forces replacement.
	var state attachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *attachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state attachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	netID, srvID, segID := state.NetworkID.ValueString(), state.ServerID.ValueString(), state.SegmentID.ValueString()
	err := r.client.DetachServer(ctx, netID, srvID, segID, client.NewIdempotencyKey())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "detach server from virtual network", err)
		return
	}
	err = waitFor(ctx, attachTimeout, func() (bool, error) {
		a, err := r.client.FindAttachment(ctx, netID, srvID, segID)
		if client.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if a == nil {
			return true, nil
		}
		if a.Status == "DETACH_FAILED" {
			return false, fmt.Errorf("the network change failed (status DETACH_FAILED)")
		}
		return false, nil
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "wait for virtual network detach", err)
	}
}

func (r *attachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("segment_id"), parts[2])...)
}
