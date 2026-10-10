package provider

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

// ---------------------------------------------------------------- serverside_datacenters

type datacentersDataSource struct{ client *client.Client }

type datacenterModel struct {
	ID          types.String `tfsdk:"id"`
	Code        types.String `tfsdk:"code"`
	Name        types.String `tfsdk:"name"`
	City        types.String `tfsdk:"city"`
	State       types.String `tfsdk:"state"`
	CountryCode types.String `tfsdk:"country_code"`
	Facility    types.String `tfsdk:"facility"`
}

type datacentersModel struct {
	Datacenters []datacenterModel `tfsdk:"datacenters"`
}

func NewDatacentersDataSource() datasource.DataSource { return &datacentersDataSource{} }

func (d *datacentersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datacenters"
}

func datacenterAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true},
		"code":         schema.StringAttribute{Description: "Short code such as ams1.", Computed: true},
		"name":         schema.StringAttribute{Computed: true},
		"city":         schema.StringAttribute{Computed: true},
		"state":        schema.StringAttribute{Computed: true},
		"country_code": schema.StringAttribute{Computed: true},
		"facility":     schema.StringAttribute{Computed: true},
	}
}

func (d *datacentersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Datacenters (the API's regions) that services and IP blocks can be ordered in.",
		Attributes: map[string]schema.Attribute{
			"datacenters": schema.ListNestedAttribute{
				Computed:     true,
				NestedObject: schema.NestedAttributeObject{Attributes: datacenterAttrs()},
			},
		},
	}
}

func (d *datacentersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *datacentersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	dcs, err := d.client.ListRegions(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "list datacenters", err)
		return
	}
	state := datacentersModel{Datacenters: []datacenterModel{}}
	for _, dc := range dcs {
		state.Datacenters = append(state.Datacenters, datacenterModel{
			ID: types.StringValue(dc.ID), Code: types.StringValue(dc.Code), Name: types.StringValue(dc.Name), City: types.StringValue(dc.City),
			State: types.StringValue(dc.State), CountryCode: types.StringValue(dc.CountryCode), Facility: types.StringValue(dc.Facility),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// ---------------------------------------------------------------- plans (shared)

type availabilityModel struct {
	DatacenterID   types.String `tfsdk:"datacenter_id"`
	DatacenterCode types.String `tfsdk:"datacenter_code"`
	DatacenterName types.String `tfsdk:"datacenter_name"`
	Quantity       types.Int64  `tfsdk:"quantity"`
}

type priceModel struct {
	Currency          types.String `tfsdk:"currency"`
	HourlyMillicents  types.Int64  `tfsdk:"hourly_millicents"`
	MonthlyMillicents types.Int64  `tfsdk:"monthly_millicents"`
}

func availabilityAttr() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Description: "Stock per datacenter.",
		Computed:    true,
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"datacenter_id":   schema.StringAttribute{Computed: true},
			"datacenter_code": schema.StringAttribute{Computed: true},
			"datacenter_name": schema.StringAttribute{Computed: true},
			"quantity":        schema.Int64Attribute{Computed: true},
		}},
	}
}

func priceAttr() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Description: "Prices in thousandths of a cent (millicents) of the currency.",
		Computed:    true,
		Attributes: map[string]schema.Attribute{
			"currency":           schema.StringAttribute{Computed: true},
			"hourly_millicents":  schema.Int64Attribute{Computed: true},
			"monthly_millicents": schema.Int64Attribute{Computed: true},
		},
	}
}

func toAvailability(av []client.Availability) []availabilityModel {
	out := []availabilityModel{}
	for _, a := range av {
		out = append(out, availabilityModel{
			DatacenterID: types.StringValue(a.ID), DatacenterCode: types.StringValue(a.Code),
			DatacenterName: types.StringValue(a.Name), Quantity: types.Int64Value(a.Quantity),
		})
	}
	return out
}

func toPrice(p client.Pricing) *priceModel {
	m := &priceModel{Currency: types.StringValue(p.Currency), HourlyMillicents: types.Int64Null(), MonthlyMillicents: types.Int64Null()}
	if r, ok := p.Rates["HOURLY"]; ok {
		m.HourlyMillicents = types.Int64Value(r.PriceMillicents)
	}
	if r, ok := p.Rates["MONTHLY"]; ok {
		m.MonthlyMillicents = types.Int64Value(r.PriceMillicents)
	}
	return m
}

// isDatacenter matches a datacenter given by id or by code, as the API's own
// datacenter fields accept either.
func isDatacenter(a client.Availability, datacenter string) bool {
	return a.ID == datacenter || a.Code == datacenter
}

func inStockAt(av []client.Availability, datacenter string) bool {
	for _, a := range av {
		if (datacenter == "" || isDatacenter(a, datacenter)) && a.Quantity > 0 {
			return true
		}
	}
	return false
}

func offeredAt(av []client.Availability, datacenter string) bool {
	if datacenter == "" {
		return true
	}
	for _, a := range av {
		if isDatacenter(a, datacenter) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- serverside_baremetal_plans

type baremetalPlansDataSource struct{ client *client.Client }

type baremetalPlanModel struct {
	ID                  types.String        `tfsdk:"id"`
	Slug                types.String        `tfsdk:"slug"`
	Name                types.String        `tfsdk:"name"`
	CPU                 types.String        `tfsdk:"cpu"`
	CPUSockets          types.Int64         `tfsdk:"cpu_sockets"`
	CPUCores            types.Int64         `tfsdk:"cpu_cores"`
	MemoryGB            types.Int64         `tfsdk:"memory_gb"`
	IncludedEgressGB    types.Int64         `tfsdk:"included_egress_gb"`
	InterfaceSpeedsMbps []int64             `tfsdk:"interface_speeds_mbps"`
	Price               *priceModel         `tfsdk:"price"`
	Availability        []availabilityModel `tfsdk:"availability"`
}

type baremetalPlansModel struct {
	DatacenterID types.String         `tfsdk:"datacenter_id"`
	InStockOnly  types.Bool           `tfsdk:"in_stock_only"`
	Plans        []baremetalPlanModel `tfsdk:"plans"`
}

func NewBaremetalPlansDataSource() datasource.DataSource { return &baremetalPlansDataSource{} }

func (d *baremetalPlansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_baremetal_plans"
}

func (d *baremetalPlansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Bare metal plans with specifications, prices and stock.",
		Attributes: map[string]schema.Attribute{
			"datacenter_id": schema.StringAttribute{Description: "Only plans offered in this datacenter, given by id or code.", Optional: true},
			"in_stock_only": schema.BoolAttribute{Description: "Only plans with stock (in datacenter_id, when set).", Optional: true},
			"plans": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":                 schema.StringAttribute{Computed: true},
					"slug":               schema.StringAttribute{Computed: true},
					"name":               schema.StringAttribute{Computed: true},
					"cpu":                schema.StringAttribute{Computed: true},
					"cpu_sockets":        schema.Int64Attribute{Computed: true},
					"cpu_cores":          schema.Int64Attribute{Computed: true},
					"memory_gb":          schema.Int64Attribute{Computed: true},
					"included_egress_gb": schema.Int64Attribute{Description: "Monthly outbound allowance in GB; null means unlimited.", Computed: true},
					"interface_speeds_mbps": schema.ListAttribute{
						Description: "Speed of each network port in Mbps.",
						Computed:    true,
						ElementType: types.Int64Type,
					},
					"price":        priceAttr(),
					"availability": availabilityAttr(),
				}},
			},
		},
	}
}

func (d *baremetalPlansDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *baremetalPlansDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg baremetalPlansModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plans, err := d.client.ListBaremetalPlans(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "list bare metal plans", err)
		return
	}
	dc := cfg.DatacenterID.ValueString()
	cfg.Plans = []baremetalPlanModel{}
	for _, p := range plans {
		if !offeredAt(p.Availability, dc) || (cfg.InStockOnly.ValueBool() && !inStockAt(p.Availability, dc)) {
			continue
		}
		speeds := []int64{}
		for _, i := range p.Specs.Interfaces {
			speeds = append(speeds, i.SpeedMbps)
		}
		cfg.Plans = append(cfg.Plans, baremetalPlanModel{
			ID: types.StringValue(p.ID), Slug: types.StringValue(p.Slug), Name: types.StringValue(p.Name), CPU: types.StringValue(p.Specs.CPU),
			CPUSockets: types.Int64Value(p.Specs.CPUSockets), CPUCores: types.Int64Value(p.Specs.CPUCores),
			MemoryGB: types.Int64Value(p.Specs.MemoryGB), IncludedEgressGB: int64Value(p.Specs.IncludedEgressGB),
			InterfaceSpeedsMbps: speeds, Price: toPrice(p.Pricing), Availability: toAvailability(p.Availability),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}

// ---------------------------------------------------------------- serverside_ip_block_plans

type ipBlockPlansDataSource struct{ client *client.Client }

type ipBlockPlanModel struct {
	ID              types.String        `tfsdk:"id"`
	Name            types.String        `tfsdk:"name"`
	IPVersion       types.String        `tfsdk:"ip_version"`
	PrefixLength    types.Int64         `tfsdk:"prefix_length"`
	UsableAddresses types.Int64         `tfsdk:"usable_addresses"`
	Price           *priceModel         `tfsdk:"price"`
	Availability    []availabilityModel `tfsdk:"availability"`
}

type ipBlockPlansModel struct {
	DatacenterID types.String       `tfsdk:"datacenter_id"`
	Plans        []ipBlockPlanModel `tfsdk:"plans"`
}

func NewIPBlockPlansDataSource() datasource.DataSource { return &ipBlockPlansDataSource{} }

func (d *ipBlockPlansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_block_plans"
}

func (d *ipBlockPlansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "IP block plans (prefix sizes) with prices and stock.",
		Attributes: map[string]schema.Attribute{
			"datacenter_id": schema.StringAttribute{Description: "Only plans offered in this datacenter, given by id or code.", Optional: true},
			"plans": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":               schema.StringAttribute{Computed: true},
					"name":             schema.StringAttribute{Computed: true},
					"ip_version":       schema.StringAttribute{Description: "V4 or V6.", Computed: true},
					"prefix_length":    schema.Int64Attribute{Computed: true},
					"usable_addresses": schema.Int64Attribute{Computed: true},
					"price":            priceAttr(),
					"availability":     availabilityAttr(),
				}},
			},
		},
	}
}

func (d *ipBlockPlansDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *ipBlockPlansDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg ipBlockPlansModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plans, err := d.client.ListIPBlockPlans(ctx, cfg.DatacenterID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "list IP block plans", err)
		return
	}
	cfg.Plans = []ipBlockPlanModel{}
	for _, p := range plans {
		cfg.Plans = append(cfg.Plans, ipBlockPlanModel{
			ID: types.StringValue(p.ID), Name: types.StringValue(p.Name), IPVersion: types.StringValue(p.Specs.IPVersion),
			PrefixLength: types.Int64Value(p.Specs.PrefixLength), UsableAddresses: int64Value(p.Specs.UsableAddresses),
			Price: toPrice(p.Pricing), Availability: toAvailability(p.Availability),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}

// ---------------------------------------------------------------- serverside_baremetal_server

type baremetalServerDataSource struct{ client *client.Client }

type serverInterfaceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	MacAddress types.String `tfsdk:"mac_address"`
	SpeedMbps  types.Int64  `tfsdk:"speed_mbps"`
}

type serverSegmentModel struct {
	SegmentID  types.String           `tfsdk:"segment_id"`
	Name       types.String           `tfsdk:"name"`
	BondMode   types.String           `tfsdk:"bond_mode"`
	Interfaces []serverInterfaceModel `tfsdk:"interfaces"`
}

type serverNetworkModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Type      types.String `tfsdk:"type"`
	ManagedBy types.String `tfsdk:"managed_by"`
	Native    types.Bool   `tfsdk:"native"`
	VlanID    types.Int64  `tfsdk:"vlan_id"`
	SegmentID types.String `tfsdk:"segment_id"`
	Addresses []string     `tfsdk:"addresses"`
}

type baremetalServerModel struct {
	ID              types.String         `tfsdk:"id"`
	Name            types.String         `tfsdk:"name"`
	State           types.String         `tfsdk:"state"`
	Locked          types.Bool           `tfsdk:"locked"`
	Suspended       types.Bool           `tfsdk:"suspended"`
	PrimaryIPv4     types.String         `tfsdk:"primary_ipv4"`
	PrimaryIPv6     types.String         `tfsdk:"primary_ipv6"`
	DatacenterID    types.String         `tfsdk:"datacenter_id"`
	DatacenterCode  types.String         `tfsdk:"datacenter_code"`
	DatacenterName  types.String         `tfsdk:"datacenter_name"`
	OperatingSystem types.String         `tfsdk:"operating_system"`
	Segments        []serverSegmentModel `tfsdk:"segments"`
	Networks        []serverNetworkModel `tfsdk:"networks"`
}

func NewBaremetalServerDataSource() datasource.DataSource { return &baremetalServerDataSource{} }

func (d *baremetalServerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_baremetal_server"
}

func (d *baremetalServerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads an existing bare metal server: its network segments (for serverside_virtual_network_attachment) and the virtual networks it is on (for serverside_ip_address).",
		Attributes: map[string]schema.Attribute{
			"id":               schema.StringAttribute{Description: "Id of the bare metal service.", Required: true},
			"name":             schema.StringAttribute{Computed: true},
			"state":            schema.StringAttribute{Description: "ACTIVE, SUSPENDED, OPERATION_IN_PROGRESS or DELETING.", Computed: true},
			"locked":           schema.BoolAttribute{Computed: true},
			"suspended":        schema.BoolAttribute{Computed: true},
			"primary_ipv4":     schema.StringAttribute{Computed: true},
			"primary_ipv6":     schema.StringAttribute{Computed: true},
			"datacenter_id":    schema.StringAttribute{Computed: true},
			"datacenter_code":  schema.StringAttribute{Computed: true},
			"datacenter_name":  schema.StringAttribute{Computed: true},
			"operating_system": schema.StringAttribute{Computed: true},
			"segments": schema.ListNestedAttribute{
				Description: "Network segments: each is one port, or several ports bonded, that virtual networks attach to.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"segment_id": schema.StringAttribute{Computed: true},
					"name":       schema.StringAttribute{Computed: true},
					"bond_mode":  schema.StringAttribute{Description: "LACP for a bond; null for a single port.", Computed: true},
					"interfaces": schema.ListNestedAttribute{
						Description: "Physical ports in the segment.",
						Computed:    true,
						NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
							"id":          schema.StringAttribute{Computed: true},
							"name":        schema.StringAttribute{Computed: true},
							"mac_address": schema.StringAttribute{Computed: true},
							"speed_mbps":  schema.Int64Attribute{Computed: true},
						}},
					},
				}},
			},
			"networks": schema.ListNestedAttribute{
				Description: "Virtual networks on the server, including its system-managed public network.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":         schema.StringAttribute{Computed: true},
					"name":       schema.StringAttribute{Computed: true},
					"type":       schema.StringAttribute{Description: "PRIVATE or PUBLIC.", Computed: true},
					"managed_by": schema.StringAttribute{Description: "CUSTOMER or SYSTEM.", Computed: true},
					"native":     schema.BoolAttribute{Computed: true},
					"vlan_id":    schema.Int64Attribute{Computed: true},
					"segment_id": schema.StringAttribute{Description: "Segment the network is attached to.", Computed: true},
					"addresses":  schema.ListAttribute{Computed: true, ElementType: types.StringType},
				}},
			},
		},
	}
}

func (d *baremetalServerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *baremetalServerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg baremetalServerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := d.client.GetServer(ctx, cfg.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "read bare metal server", err)
		return
	}
	m := baremetalServerModel{
		ID: types.StringValue(s.ID), Name: stringValue(s.Name), State: types.StringValue(s.State),
		Locked: types.BoolValue(s.IsLocked), Suspended: types.BoolValue(s.IsSuspended),
		PrimaryIPv4: stringValue(s.PrimaryIPv4), PrimaryIPv6: stringValue(s.PrimaryIPv6),
		DatacenterID: types.StringValue(s.Region.ID), DatacenterCode: types.StringValue(s.Region.Code),
		DatacenterName:  types.StringValue(s.Region.Name),
		OperatingSystem: types.StringNull(),
		Segments:        []serverSegmentModel{}, Networks: []serverNetworkModel{},
	}
	if s.OperatingSystem != nil {
		m.OperatingSystem = types.StringValue(s.OperatingSystem.Name)
	}
	for _, seg := range s.Interfaces {
		sm := serverSegmentModel{
			SegmentID: types.StringValue(seg.SegmentID), Name: types.StringValue(seg.Name),
			BondMode: stringValue(seg.BondMode), Interfaces: []serverInterfaceModel{},
		}
		for _, pi := range seg.PhysicalInterfaces {
			sm.Interfaces = append(sm.Interfaces, serverInterfaceModel{
				ID: types.StringValue(pi.ID), Name: types.StringValue(pi.Name),
				MacAddress: types.StringValue(pi.MacAddress), SpeedMbps: types.Int64Value(pi.SpeedMbps),
			})
		}
		m.Segments = append(m.Segments, sm)
		for _, vn := range seg.Networks {
			addrs := []string{}
			for _, p := range vn.Prefixes {
				addrs = append(addrs, p.Addresses...)
			}
			native := types.BoolNull()
			if vn.IsNative != nil {
				native = types.BoolValue(*vn.IsNative)
			}
			m.Networks = append(m.Networks, serverNetworkModel{
				ID: types.StringValue(vn.ID), Name: types.StringValue(vn.Name), Type: types.StringValue(vn.Type),
				ManagedBy: types.StringValue(vn.ManagedBy), Native: native, VlanID: int64Value(vn.LocalVlanID),
				SegmentID: types.StringValue(seg.SegmentID), Addresses: addrs,
			})
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ---------------------------------------------------------------- serverside_ip_address

type ipAddressDataSource struct{ client *client.Client }

type ipAddressModel struct {
	NetworkID  types.String `tfsdk:"network_id"`
	Address    types.String `tfsdk:"address"`
	IPBlockID  types.String `tfsdk:"ip_block_id"`
	State      types.String `tfsdk:"state"`
	ReverseDNS types.String `tfsdk:"reverse_dns"`
	ServiceIDs []string     `tfsdk:"service_ids"`
}

func NewIPAddressDataSource() datasource.DataSource { return &ipAddressDataSource{} }

func (d *ipAddressDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_address"
}

func (d *ipAddressDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up an IP address on a virtual network: the IP block holding it, its state, its reverse DNS record and the services it is assigned to.",
		Attributes: map[string]schema.Attribute{
			"network_id":  schema.StringAttribute{Description: "Virtual network holding the address, for example a server's public network from serverside_baremetal_server.", Required: true},
			"address":     schema.StringAttribute{Description: "The IPv4 or IPv6 address.", Required: true},
			"ip_block_id": schema.StringAttribute{Computed: true},
			"state":       schema.StringAttribute{Description: "FREE, RESERVED, GATEWAY or ASSIGNED.", Computed: true},
			"reverse_dns": schema.StringAttribute{Computed: true},
			"service_ids": schema.ListAttribute{Description: "Services the address is assigned to.", Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *ipAddressDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSource(req, resp)
}

func (d *ipAddressDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg ipAddressModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	want, err := netip.ParseAddr(cfg.Address.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid address", err.Error())
		return
	}
	netID := cfg.NetworkID.ValueString()
	n, err := d.client.GetNetwork(ctx, netID)
	if err != nil {
		addAPIError(&resp.Diagnostics, "read virtual network", err)
		return
	}
	for _, bs := range n.IPBlocks {
		if pfx, err := netip.ParsePrefix(bs.CIDR); err == nil && !pfx.Contains(want) {
			continue
		}
		b, err := d.client.GetIPBlock(ctx, bs.ID)
		if err != nil {
			addAPIError(&resp.Diagnostics, "read IP block", err)
			return
		}
		for _, a := range b.Addresses {
			if got, err := netip.ParseAddr(a.Address); err == nil && got == want {
				cfg.IPBlockID = types.StringValue(b.ID)
				cfg.State = types.StringValue(a.State)
				cfg.ReverseDNS = stringValue(a.RDNS)
				cfg.ServiceIDs = []string{}
				for _, sa := range a.ServiceAssignments {
					cfg.ServiceIDs = append(cfg.ServiceIDs, sa.ServiceID)
				}
				resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
				return
			}
		}
	}
	resp.Diagnostics.AddError("IP address not found",
		fmt.Sprintf("%s is not on virtual network %s", want, netID))
}
