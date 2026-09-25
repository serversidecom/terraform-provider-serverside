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
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	City     types.String `tfsdk:"city"`
	State    types.String `tfsdk:"state"`
	Country  types.String `tfsdk:"country"`
	Facility types.String `tfsdk:"facility"`
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
		"id":       schema.StringAttribute{Computed: true},
		"name":     schema.StringAttribute{Computed: true},
		"city":     schema.StringAttribute{Computed: true},
		"state":    schema.StringAttribute{Computed: true},
		"country":  schema.StringAttribute{Computed: true},
		"facility": schema.StringAttribute{Computed: true},
	}
}

func (d *datacentersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Datacenters where virtual networks and IP blocks can be created.",
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
	dcs, err := d.client.ListNetworkDatacenters(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "list datacenters", err)
		return
	}
	state := datacentersModel{Datacenters: []datacenterModel{}}
	for _, dc := range dcs {
		state.Datacenters = append(state.Datacenters, datacenterModel{
			ID: types.StringValue(dc.ID), Name: types.StringValue(dc.Name), City: types.StringValue(dc.City),
			State: types.StringValue(dc.State), Country: types.StringValue(dc.Country), Facility: types.StringValue(dc.Facility),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// ---------------------------------------------------------------- plans (shared)

type availabilityModel struct {
	DatacenterID   types.String `tfsdk:"datacenter_id"`
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
		out = append(out, availabilityModel{DatacenterID: types.StringValue(a.ID), DatacenterName: types.StringValue(a.Name), Quantity: types.Int64Value(a.Quantity)})
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

func inStockAt(av []client.Availability, datacenterID string) bool {
	for _, a := range av {
		if (datacenterID == "" || a.ID == datacenterID) && a.Quantity > 0 {
			return true
		}
	}
	return false
}

func offeredAt(av []client.Availability, datacenterID string) bool {
	if datacenterID == "" {
		return true
	}
	for _, a := range av {
		if a.ID == datacenterID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- serverside_baremetal_plans

type baremetalPlansDataSource struct{ client *client.Client }

type baremetalPlanModel struct {
	ID              types.String        `tfsdk:"id"`
	Name            types.String        `tfsdk:"name"`
	CPU             types.String        `tfsdk:"cpu"`
	CPUSockets      types.Int64         `tfsdk:"cpu_sockets"`
	CPUCores        types.Int64         `tfsdk:"cpu_cores"`
	MemoryGB        types.Int64         `tfsdk:"memory_gb"`
	EgressGB        types.Int64         `tfsdk:"egress_gb"`
	InterfaceSpeeds []int64             `tfsdk:"interface_speeds"`
	Price           *priceModel         `tfsdk:"price"`
	Availability    []availabilityModel `tfsdk:"availability"`
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
			"datacenter_id": schema.StringAttribute{Description: "Only plans offered in this datacenter.", Optional: true},
			"in_stock_only": schema.BoolAttribute{Description: "Only plans with stock (in datacenter_id, when set).", Optional: true},
			"plans": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":               schema.StringAttribute{Computed: true},
					"name":             schema.StringAttribute{Computed: true},
					"cpu":              schema.StringAttribute{Computed: true},
					"cpu_sockets":      schema.Int64Attribute{Computed: true},
					"cpu_cores":        schema.Int64Attribute{Computed: true},
					"memory_gb":        schema.Int64Attribute{Computed: true},
					"egress_gb":        schema.Int64Attribute{Description: "Monthly outbound allowance in GB; 0 means unlimited.", Computed: true},
					"interface_speeds": schema.ListAttribute{Description: "Speed of each network port, as the API reports it.", Computed: true, ElementType: types.Int64Type},
					"price":            priceAttr(),
					"availability":     availabilityAttr(),
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
			speeds = append(speeds, i.InterfaceSpeed)
		}
		cfg.Plans = append(cfg.Plans, baremetalPlanModel{
			ID: types.StringValue(p.ID), Name: types.StringValue(p.Name), CPU: types.StringValue(p.Specs.CPU),
			CPUSockets: types.Int64Value(p.Specs.CPUSockets), CPUCores: types.Int64Value(p.Specs.CPUCores),
			MemoryGB: types.Int64Value(p.Specs.Memory), EgressGB: types.Int64Value(p.Specs.EgressGB),
			InterfaceSpeeds: speeds, Price: toPrice(p.Pricing), Availability: toAvailability(p.Availability),
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
			"datacenter_id": schema.StringAttribute{Description: "Only plans offered in this datacenter.", Optional: true},
			"plans": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":               schema.StringAttribute{Computed: true},
					"name":             schema.StringAttribute{Computed: true},
					"ip_version":       schema.StringAttribute{Computed: true},
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

type serverSegmentModel struct {
	SegmentID          types.String `tfsdk:"segment_id"`
	LogicalInterfaceID types.String `tfsdk:"logical_interface_id"`
	InterfaceName      types.String `tfsdk:"interface_name"`
	MacAddress         types.String `tfsdk:"mac_address"`
	Speed              types.Int64  `tfsdk:"speed"`
}

type serverNetworkModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Type               types.String `tfsdk:"type"`
	Native             types.Bool   `tfsdk:"native"`
	VlanID             types.Int64  `tfsdk:"vlan_id"`
	LogicalInterfaceID types.String `tfsdk:"logical_interface_id"`
	Addresses          []string     `tfsdk:"addresses"`
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
			"state":            schema.StringAttribute{Description: "ACTIVE, SUSPENDED or OPERATION_IN_PROGRESS.", Computed: true},
			"locked":           schema.BoolAttribute{Computed: true},
			"suspended":        schema.BoolAttribute{Computed: true},
			"primary_ipv4":     schema.StringAttribute{Computed: true},
			"primary_ipv6":     schema.StringAttribute{Computed: true},
			"datacenter_id":    schema.StringAttribute{Computed: true},
			"datacenter_name":  schema.StringAttribute{Computed: true},
			"operating_system": schema.StringAttribute{Computed: true},
			"segments": schema.ListNestedAttribute{
				Description: "Physical ports and the segment id each belongs to.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"segment_id":           schema.StringAttribute{Computed: true},
					"logical_interface_id": schema.StringAttribute{Computed: true},
					"interface_name":       schema.StringAttribute{Computed: true},
					"mac_address":          schema.StringAttribute{Computed: true},
					"speed":                schema.Int64Attribute{Computed: true},
				}},
			},
			"networks": schema.ListNestedAttribute{
				Description: "Virtual networks on the server, including its system-managed public network.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":                   schema.StringAttribute{Computed: true},
					"name":                 schema.StringAttribute{Computed: true},
					"type":                 schema.StringAttribute{Computed: true},
					"native":               schema.BoolAttribute{Computed: true},
					"vlan_id":              schema.Int64Attribute{Computed: true},
					"logical_interface_id": schema.StringAttribute{Computed: true},
					"addresses":            schema.ListAttribute{Computed: true, ElementType: types.StringType},
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
		Locked: types.BoolValue(s.Locked), Suspended: types.BoolValue(s.Suspended),
		PrimaryIPv4: stringValue(s.PrimaryIPv4), PrimaryIPv6: stringValue(s.PrimaryIPv6),
		DatacenterID: types.StringValue(s.Region.ID), DatacenterName: types.StringValue(s.Region.Name),
		OperatingSystem: types.StringNull(),
		Segments:        []serverSegmentModel{}, Networks: []serverNetworkModel{},
	}
	if s.OperatingSystem != nil {
		m.OperatingSystem = types.StringValue(s.OperatingSystem.Name)
	}
	for _, li := range s.Interfaces {
		for _, pi := range li.PhysicalInterfaces {
			m.Segments = append(m.Segments, serverSegmentModel{
				SegmentID: stringValue(pi.SegmentID), LogicalInterfaceID: types.StringValue(li.LogicalInterfaceID),
				InterfaceName: types.StringValue(pi.Name), MacAddress: types.StringValue(pi.MacAddress), Speed: types.Int64Value(pi.InterfaceSpeed),
			})
		}
		for _, vn := range li.VirtualNetworks {
			addrs := []string{}
			for _, p := range vn.Prefixes {
				addrs = append(addrs, p.Addresses...)
			}
			m.Networks = append(m.Networks, serverNetworkModel{
				ID: types.StringValue(vn.ID), Name: types.StringValue(vn.Name), Type: types.StringValue(vn.Type),
				Native: types.BoolValue(vn.IsNative), VlanID: int64Value(vn.LocalVlanID),
				LogicalInterfaceID: types.StringValue(li.LogicalInterfaceID), Addresses: addrs,
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
	ID         types.String `tfsdk:"id"`
	PrefixID   types.String `tfsdk:"prefix_id"`
	State      types.String `tfsdk:"state"`
	ReverseDNS types.String `tfsdk:"reverse_dns"`
}

func NewIPAddressDataSource() datasource.DataSource { return &ipAddressDataSource{} }

func (d *ipAddressDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_address"
}

func (d *ipAddressDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up the API id of an IP address on a public virtual network, for serverside_firewall_group_assignment.",
		Attributes: map[string]schema.Attribute{
			"network_id":  schema.StringAttribute{Description: "Virtual network holding the address, for example a server's public network from serverside_baremetal_server.", Required: true},
			"address":     schema.StringAttribute{Description: "The IPv4 or IPv6 address.", Required: true},
			"id":          schema.StringAttribute{Computed: true},
			"prefix_id":   schema.StringAttribute{Computed: true},
			"state":       schema.StringAttribute{Description: "FREE, RESERVED, GATEWAY or ASSIGNED.", Computed: true},
			"reverse_dns": schema.StringAttribute{Computed: true},
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
	family, prefixes := "v4", n.IPv4ChildPrefixes
	if want.Is6() {
		family, prefixes = "v6", n.IPv6ChildPrefixes
	}
	for _, ps := range prefixes {
		if pfx, err := netip.ParsePrefix(ps.Network); err == nil && !pfx.Contains(want) {
			continue
		}
		p, err := d.client.GetPrefix(ctx, netID, ps.ID, family)
		if err != nil {
			addAPIError(&resp.Diagnostics, "read prefix", err)
			return
		}
		for _, a := range p.Addresses {
			if got, err := netip.ParseAddr(a.Address); err == nil && got == want {
				cfg.ID = types.StringValue(a.ID)
				cfg.PrefixID = types.StringValue(p.ID)
				cfg.State = types.StringValue(a.State)
				cfg.ReverseDNS = stringValue(&a.RDNS)
				resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
				return
			}
		}
	}
	resp.Diagnostics.AddError("IP address not found",
		fmt.Sprintf("%s is not on virtual network %s", want, netID))
}
