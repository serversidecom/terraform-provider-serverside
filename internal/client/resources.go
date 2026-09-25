package client

import (
	"context"
	"net/http"
	"net/url"
)

// ---------------------------------------------------------------- SSH keys

type SSHKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
	KeyType   string `json:"keyType"`
	CreatedAt string `json:"createdAt"`
}

func (c *Client) CreateSSHKey(ctx context.Context, name, publicKey string) (*SSHKey, error) {
	var out SSHKey
	_, _, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   "/v1/organization/ssh-keys",
		body:   map[string]string{"name": name, "publicKey": publicKey},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSSHKey finds a key in the organization's list; the API has no GET by id.
// It returns a 404 *APIError when the key is gone.
func (c *Client) GetSSHKey(ctx context.Context, id string) (*SSHKey, error) {
	keys, err := listAll[SSHKey](ctx, c, "/v1/organization/ssh-keys", nil)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Code: "NOT_FOUND", Message: "SSH key not found", Method: http.MethodGet, Path: "/v1/organization/ssh-keys"}
}

func (c *Client) RenameSSHKey(ctx context.Context, id, name string) (*SSHKey, error) {
	var out SSHKey
	_, _, err := c.do(ctx, request{
		method: http.MethodPut,
		path:   "/v1/organization/ssh-keys/" + pathEscape(id),
		body:   map[string]string{"name": name},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: "/v1/organization/ssh-keys/" + pathEscape(id)}, nil)
	return err
}

// ---------------------------------------------------------------- Firewall

type PacketCriteria struct {
	MaxPacketLength *int64 `json:"maxPacketLength"`
	MinPacketLength *int64 `json:"minPacketLength"`
}

// FirewallRuleSpec is the full definition of a rule. The API's PATCH replaces
// every field, so updates always send the whole spec.
type FirewallRuleSpec struct {
	Type            string          `json:"type"`
	Direction       string          `json:"direction"`
	Protocol        string          `json:"protocol"`
	SourcePortStart *int64          `json:"sourcePortStart"`
	SourcePortEnd   *int64          `json:"sourcePortEnd"`
	SourceAddress   *string         `json:"sourceAddress"`
	DestPortStart   *int64          `json:"destPortStart"`
	DestPortEnd     *int64          `json:"destPortEnd"`
	RateLimitPps    *int64          `json:"rateLimitPps"`
	PacketCriteria  *PacketCriteria `json:"packetCriteria"`
	Description     *string         `json:"description"`
}

type firewallRuleUpdate struct {
	FirewallRuleSpec
	IsEnabled *bool `json:"isEnabled"`
}

type FirewallRule struct {
	FirewallRuleSpec
	ID              string `json:"id"`
	FirewallGroupID string `json:"firewallGroupId"`
	Priority        int64  `json:"priority"`
	IsEnabled       bool   `json:"isEnabled"`
}

type FirewallGroup struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	OrganizationID       string         `json:"organizationId"`
	Rules                []FirewallRule `json:"rules"`
	AssignedIPAddressIDs []string       `json:"assignedIpAddressIds"`
}

func (c *Client) CreateFirewallGroup(ctx context.Context, name string) (*FirewallGroup, error) {
	var out FirewallGroup
	_, _, err := c.do(ctx, request{method: http.MethodPost, path: "/v1/networking/firewall/groups", body: map[string]string{"name": name}}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetFirewallGroup(ctx context.Context, id string) (*FirewallGroup, error) {
	var out FirewallGroup
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/networking/firewall/groups/" + pathEscape(id)}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateFirewallGroup renames the group and/or sets the order of its rules.
// ruleOrder must list every rule of the group; nil leaves the order alone.
func (c *Client) UpdateFirewallGroup(ctx context.Context, id string, name *string, ruleOrder []string) error {
	body := map[string]any{}
	if name != nil {
		body["name"] = *name
	}
	if ruleOrder != nil {
		body["ruleOrder"] = ruleOrder
	}
	_, _, err := c.do(ctx, request{method: http.MethodPatch, path: "/v1/networking/firewall/groups/" + pathEscape(id), body: body}, nil)
	return err
}

func (c *Client) DeleteFirewallGroup(ctx context.Context, id string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: "/v1/networking/firewall/groups/" + pathEscape(id)}, nil)
	return err
}

// CreateFirewallRules appends rules to the end of the group, in order.
func (c *Client) CreateFirewallRules(ctx context.Context, groupID string, rules []FirewallRuleSpec) ([]FirewallRule, error) {
	var out []FirewallRule
	_, _, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   "/v1/networking/firewall/groups/" + pathEscape(groupID) + "/rules",
		body:   rules,
	}, &out)
	return out, err
}

func (c *Client) UpdateFirewallRule(ctx context.Context, groupID, ruleID string, spec FirewallRuleSpec, enabled bool) (*FirewallRule, error) {
	var out FirewallRule
	_, _, err := c.do(ctx, request{
		method: http.MethodPatch,
		path:   "/v1/networking/firewall/groups/" + pathEscape(groupID) + "/rules/" + pathEscape(ruleID),
		body:   firewallRuleUpdate{FirewallRuleSpec: spec, IsEnabled: &enabled},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteFirewallRule(ctx context.Context, groupID, ruleID string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: "/v1/networking/firewall/groups/" + pathEscape(groupID) + "/rules/" + pathEscape(ruleID)}, nil)
	return err
}

func (c *Client) AssignFirewallGroup(ctx context.Context, groupID, ipAddressID string) error {
	_, _, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   "/v1/networking/firewall/groups/" + pathEscape(groupID) + "/assignments",
		body:   map[string]string{"ipAddressEntityId": ipAddressID},
	}, nil)
	return err
}

func (c *Client) UnassignFirewallGroup(ctx context.Context, groupID, ipAddressID string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: "/v1/networking/firewall/groups/" + pathEscape(groupID) + "/assignments/" + pathEscape(ipAddressID)}, nil)
	return err
}

// ---------------------------------------------------------------- Virtual networks

type Datacenter struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	City     string `json:"city"`
	State    string `json:"state"`
	Facility string `json:"facility"`
}

type PrefixSummary struct {
	ID                    string     `json:"id"`
	Datacenter            Datacenter `json:"datacenter"`
	Network               string     `json:"network"`
	SubnetMask            string     `json:"subnetMask"`
	Gateway               *string    `json:"gateway"`
	PrefixLength          int64      `json:"prefixLength"`
	UsedAddressCount      int64      `json:"usedAddressCount"`
	AvailableAddressCount *int64     `json:"availableAddressCount"`
}

type Network struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Type              string          `json:"type"`
	ManagedBy         string          `json:"managedBy"`
	LocalVlanID       *int64          `json:"localVlanId"`
	CreatedAt         string          `json:"createdAt"`
	IPv4ChildPrefixes []PrefixSummary `json:"ipv4ChildPrefixes"`
	IPv6ChildPrefixes []PrefixSummary `json:"ipv6ChildPrefixes"`
}

func (c *Client) CreateNetwork(ctx context.Context, name, netType string, localVlanID int64) (*Network, error) {
	var out Network
	_, _, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   "/v1/networking/spn",
		body:   map[string]any{"name": name, "type": netType, "localVlanId": localVlanID},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetNetwork(ctx context.Context, id string) (*Network, error) {
	var out Network
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/networking/spn/" + pathEscape(id)}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RenameNetwork(ctx context.Context, id, name string) error {
	_, _, err := c.do(ctx, request{method: http.MethodPut, path: "/v1/networking/spn/" + pathEscape(id), body: map[string]string{"name": name}}, nil)
	return err
}

func (c *Client) DeleteNetwork(ctx context.Context, id string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: "/v1/networking/spn/" + pathEscape(id)}, nil)
	return err
}

func (c *Client) ListNetworkDatacenters(ctx context.Context) ([]Datacenter, error) {
	var out []Datacenter
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/networking/spn/available-datacenters"}, &out)
	return out, err
}

type NetworkAttachment struct {
	SegmentID     string   `json:"segmentId"`
	IsNative      *bool    `json:"isNative"`
	Status        string   `json:"status"`
	IPv4Addresses []string `json:"ipv4Addresses"`
	IPv6Addresses []string `json:"ipv6Addresses"`
}

type NetworkService struct {
	Service struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"service"`
	Attachments []NetworkAttachment `json:"attachments"`
}

func (c *Client) ListNetworkServices(ctx context.Context, networkID string) ([]NetworkService, error) {
	return listAll[NetworkService](ctx, c, "/v1/networking/spn/"+pathEscape(networkID)+"/services", nil)
}

// FindAttachment returns the attachment of serverID's segment to the network,
// or nil when there is none.
func (c *Client) FindAttachment(ctx context.Context, networkID, serverID, segmentID string) (*NetworkAttachment, error) {
	services, err := c.ListNetworkServices(ctx, networkID)
	if err != nil {
		return nil, err
	}
	for _, s := range services {
		if s.Service.ID != serverID {
			continue
		}
		for i := range s.Attachments {
			if s.Attachments[i].SegmentID == segmentID {
				return &s.Attachments[i], nil
			}
		}
	}
	return nil, nil
}

func (c *Client) AttachServer(ctx context.Context, networkID, serverID, segmentID string, native bool, idempotencyKey string) error {
	_, _, err := c.do(ctx, request{
		method:         http.MethodPost,
		path:           "/v1/networking/spn/" + pathEscape(networkID) + "/services/assign",
		body:           map[string]any{"serviceId": serverID, "segmentId": segmentID, "isNative": native},
		idempotencyKey: idempotencyKey,
	}, nil)
	return err
}

func (c *Client) DetachServer(ctx context.Context, networkID, serverID, segmentID, idempotencyKey string) error {
	_, _, err := c.do(ctx, request{
		method:         http.MethodPut,
		path:           "/v1/networking/spn/" + pathEscape(networkID) + "/services/unassign",
		body:           map[string]any{"serviceId": serverID, "segmentId": segmentID},
		idempotencyKey: idempotencyKey,
	}, nil)
	return err
}

// ---------------------------------------------------------------- IP blocks

type IPBlockCreated struct {
	PrefixID       string  `json:"prefixId"`
	SubscriptionID *string `json:"subscriptionId"`
}

func (c *Client) CreateIPBlock(ctx context.Context, planID, datacenterID, networkID, idempotencyKey string) (*IPBlockCreated, error) {
	var out IPBlockCreated
	_, _, err := c.do(ctx, request{
		method:         http.MethodPost,
		path:           "/v1/networking/ip-blocks",
		body:           map[string]string{"offeringId": planID, "datacenterId": datacenterID, "publicNetworkId": networkID},
		idempotencyKey: idempotencyKey,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteIPBlock requests cancellation of the block's subscription. An
// unassigned block is released at once.
func (c *Client) DeleteIPBlock(ctx context.Context, prefixID string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: "/v1/networking/ip-blocks/" + pathEscape(prefixID)}, nil)
	return err
}

type IPAddressEntity struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	State   string `json:"state"`
	RDNS    string `json:"rDNS"`
}

type Prefix struct {
	ID           string            `json:"id"`
	Datacenter   Datacenter        `json:"datacenter"`
	Network      string            `json:"network"`
	Gateway      string            `json:"gateway"`
	SubnetMask   string            `json:"subnetMask"`
	PrefixLength int64             `json:"prefixLength"`
	Addresses    []IPAddressEntity `json:"addresses"`
}

// GetPrefix reads one child prefix of a network; family is "v4" or "v6".
func (c *Client) GetPrefix(ctx context.Context, networkID, prefixID, family string) (*Prefix, error) {
	var out Prefix
	_, _, err := c.do(ctx, request{
		method: http.MethodGet,
		path:   "/v1/networking/spn/" + pathEscape(networkID) + "/prefixes/" + family + "/" + pathEscape(prefixID),
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------- Plans

type OfferingPrice struct {
	PriceMillicents            int64   `json:"priceMillicents"`
	EffectiveMonthlyMillicents int64   `json:"effectiveMonthlyMillicents"`
	TermDiscount               float64 `json:"termDiscount"`
}

type Pricing struct {
	Currency string                   `json:"currency"`
	Rates    map[string]OfferingPrice `json:"rates"`
}

type Availability struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
}

type BaremetalPlan struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Specs struct {
		CPU           string `json:"cpu"`
		CPUSockets    int64  `json:"cpuSockets"`
		CPUCores      int64  `json:"cpuCores"`
		BaseClockMhz  int64  `json:"cpuBaseClockMhz"`
		BoostClockMhz int64  `json:"cpuBoostClockMhz"`
		Memory        int64  `json:"memory"`
		MemoryType    string `json:"memoryType"`
		EgressGB      int64  `json:"egressGb"`
		Drives        []struct {
			Type   string `json:"type"`
			SizeGB int64  `json:"sizeGb"`
		} `json:"drives"`
		Interfaces []struct {
			InterfaceSpeed int64 `json:"interfaceSpeed"`
		} `json:"interfaces"`
	} `json:"specs"`
	Pricing      Pricing        `json:"pricing"`
	Availability []Availability `json:"availability"`
}

func (c *Client) ListBaremetalPlans(ctx context.Context) ([]BaremetalPlan, error) {
	return listAll[BaremetalPlan](ctx, c, "/v1/services/baremetal/plans", nil)
}

type IPBlockPlan struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Specs struct {
		IPVersion       string `json:"ipVersion"`
		PrefixLength    int64  `json:"prefixLength"`
		UsableAddresses *int64 `json:"usableAddresses"`
	} `json:"specs"`
	Pricing      Pricing        `json:"pricing"`
	Availability []Availability `json:"availability"`
}

func (c *Client) ListIPBlockPlans(ctx context.Context, datacenterID string) ([]IPBlockPlan, error) {
	q := url.Values{}
	if datacenterID != "" {
		q.Set("datacenter_id", datacenterID)
	}
	return listAll[IPBlockPlan](ctx, c, "/v1/services/ip-blocks/plans", q)
}

// ---------------------------------------------------------------- Servers (read only)

type ServerNetworkPrefix struct {
	ID           string   `json:"id"`
	Network      string   `json:"network"`
	Gateway      *string  `json:"gateway"`
	PrefixLength int64    `json:"prefixLength"`
	Addresses    []string `json:"addresses"`
}

type ServerVirtualNetwork struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Type        string                `json:"type"`
	IsNative    bool                  `json:"isNative"`
	LocalVlanID *int64                `json:"localVlanId"`
	Prefixes    []ServerNetworkPrefix `json:"prefixes"`
}

type ServerLogicalInterface struct {
	LogicalInterfaceID string `json:"logicalInterfaceId"`
	PhysicalInterfaces []struct {
		ID             string  `json:"id"`
		Name           string  `json:"name"`
		MacAddress     string  `json:"macAddress"`
		InterfaceSpeed int64   `json:"interfaceSpeed"`
		SegmentID      *string `json:"segmentId"`
	} `json:"physicalInterfaces"`
	VirtualNetworks []ServerVirtualNetwork `json:"virtualNetworks"`
}

type Server struct {
	ID          string  `json:"id"`
	Name        *string `json:"name"`
	State       string  `json:"state"`
	Locked      bool    `json:"locked"`
	Suspended   bool    `json:"suspended"`
	PrimaryIPv4 *string `json:"primaryIpv4"`
	PrimaryIPv6 *string `json:"primaryIpv6"`
	CreatedAt   string  `json:"createdAt"`
	Region      struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		City     string `json:"city"`
		Country  string `json:"country"`
		Facility string `json:"facility"`
	} `json:"region"`
	OperatingSystem *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"operatingSystem"`
	Interfaces []ServerLogicalInterface `json:"interfaces"`
}

func (c *Client) GetServer(ctx context.Context, id string) (*Server, error) {
	var out Server
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/services/baremetal/" + pathEscape(id)}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
