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
		method: http.MethodPatch,
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
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	OrganizationID    string         `json:"organizationId"`
	Rules             []FirewallRule `json:"rules"`
	AssignedAddresses []string       `json:"assignedAddresses"`
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

// AssignFirewallGroup applies the group to one of the organization's IP
// addresses, given as the address itself.
func (c *Client) AssignFirewallGroup(ctx context.Context, groupID, address string) error {
	_, _, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   "/v1/networking/firewall/groups/" + pathEscape(groupID) + "/assignments",
		body:   map[string]string{"address": address},
	}, nil)
	return err
}

func (c *Client) UnassignFirewallGroup(ctx context.Context, groupID, address string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: "/v1/networking/firewall/groups/" + pathEscape(groupID) + "/assignments/" + pathEscape(address)}, nil)
	return err
}

// ---------------------------------------------------------------- Regions

// Region is a datacenter. Request fields call it a datacenter and accept its
// id or its code (such as ams1).
type Region struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	City        string `json:"city"`
	State       string `json:"state"`
	CountryCode string `json:"countryCode"`
	Facility    string `json:"facility"`
}

func (c *Client) ListRegions(ctx context.Context) ([]Region, error) {
	var out []Region
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/regions"}, &out)
	return out, err
}

// ---------------------------------------------------------------- Virtual networks

// IPBlockSummary is an IP block as a network lists it, without its addresses.
type IPBlockSummary struct {
	ID                    string  `json:"id"`
	CreatedAt             string  `json:"createdAt"`
	NetworkID             string  `json:"networkId"`
	Datacenter            Region  `json:"datacenter"`
	IPVersion             string  `json:"ipVersion"`
	CIDR                  string  `json:"cidr"`
	Gateway               *string `json:"gateway"`
	SubnetMask            string  `json:"subnetMask"`
	PrefixLength          int64   `json:"prefixLength"`
	UsedAddressCount      int64   `json:"usedAddressCount"`
	AvailableAddressCount *int64  `json:"availableAddressCount"`
}

type Network struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Type        string           `json:"type"`
	ManagedBy   string           `json:"managedBy"`
	LocalVlanID *int64           `json:"localVlanId"`
	CreatedAt   string           `json:"createdAt"`
	IPBlocks    []IPBlockSummary `json:"ipBlocks"`
}

const networksPath = "/v1/networking/virtual-networks"

func (c *Client) CreateNetwork(ctx context.Context, name, netType string, localVlanID int64) (*Network, error) {
	var out Network
	_, _, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   networksPath,
		body:   map[string]any{"name": name, "type": netType, "localVlanId": localVlanID},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetNetwork(ctx context.Context, id string) (*Network, error) {
	var out Network
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: networksPath + "/" + pathEscape(id)}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RenameNetwork(ctx context.Context, id, name string) error {
	_, _, err := c.do(ctx, request{method: http.MethodPatch, path: networksPath + "/" + pathEscape(id), body: map[string]string{"name": name}}, nil)
	return err
}

func (c *Client) DeleteNetwork(ctx context.Context, id string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: networksPath + "/" + pathEscape(id)}, nil)
	return err
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

func (c *Client) ListNetworkAttachments(ctx context.Context, networkID string) ([]NetworkService, error) {
	return listAll[NetworkService](ctx, c, networksPath+"/"+pathEscape(networkID)+"/attachments", nil)
}

// FindAttachment returns the attachment of serverID's segment to the network,
// or nil when there is none.
func (c *Client) FindAttachment(ctx context.Context, networkID, serverID, segmentID string) (*NetworkAttachment, error) {
	services, err := c.ListNetworkAttachments(ctx, networkID)
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

// AttachServer starts attaching the segment; the API answers 202 and applies
// the switch change afterwards (204 when the attachment already exists).
func (c *Client) AttachServer(ctx context.Context, networkID, serverID, segmentID string, native bool, idempotencyKey string) error {
	_, _, err := c.do(ctx, request{
		method:         http.MethodPost,
		path:           networksPath + "/" + pathEscape(networkID) + "/attachments",
		body:           map[string]any{"serviceId": serverID, "segmentId": segmentID, "isNative": native},
		idempotencyKey: idempotencyKey,
	}, nil)
	return err
}

func (c *Client) DetachServer(ctx context.Context, networkID, serverID, segmentID, idempotencyKey string) error {
	_, _, err := c.do(ctx, request{
		method:         http.MethodDelete,
		path:           networksPath + "/" + pathEscape(networkID) + "/attachments/" + pathEscape(serverID) + "/" + pathEscape(segmentID),
		idempotencyKey: idempotencyKey,
	}, nil)
	return err
}

// ---------------------------------------------------------------- IP blocks

type ServiceReference struct {
	ServiceID string `json:"serviceId"`
	Name      string `json:"name"`
}

type IPAddress struct {
	Address            string             `json:"address"`
	RDNS               *string            `json:"rdns"`
	State              string             `json:"state"`
	ServiceAssignments []ServiceReference `json:"serviceAssignments"`
}

type IPBlock struct {
	IPBlockSummary
	Addresses []IPAddress `json:"addresses"`
}

const ipBlocksPath = "/v1/networking/ip-blocks"

// CreateIPBlock orders an hourly-billed block. datacenter is the datacenter's
// id or code.
func (c *Client) CreateIPBlock(ctx context.Context, planID, datacenter, networkID, idempotencyKey string) (*IPBlock, error) {
	var out IPBlock
	_, _, err := c.do(ctx, request{
		method:         http.MethodPost,
		path:           ipBlocksPath,
		body:           map[string]string{"plan": planID, "datacenter": datacenter, "networkId": networkID},
		idempotencyKey: idempotencyKey,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetIPBlock(ctx context.Context, id string) (*IPBlock, error) {
	var out IPBlock
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: ipBlocksPath + "/" + pathEscape(id)}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteIPBlock requests cancellation of the block's subscription. An
// unassigned block is released at once; a block bundled with a service is
// refused.
func (c *Client) DeleteIPBlock(ctx context.Context, id string) error {
	_, _, err := c.do(ctx, request{method: http.MethodDelete, path: ipBlocksPath + "/" + pathEscape(id)}, nil)
	return err
}

type Subscription struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Cycle  string `json:"cycle"`
}

func (c *Client) GetIPBlockSubscription(ctx context.Context, id string) (*Subscription, error) {
	var out Subscription
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: ipBlocksPath + "/" + pathEscape(id) + "/subscription"}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------- Plans

type PlanPrice struct {
	PriceMillicents            int64   `json:"priceMillicents"`
	EffectiveMonthlyMillicents int64   `json:"effectiveMonthlyMillicents"`
	TermDiscount               float64 `json:"termDiscount"`
}

type Pricing struct {
	Currency string               `json:"currency"`
	Rates    map[string]PlanPrice `json:"rates"`
}

// Availability is the stock of a plan in one datacenter.
type Availability struct {
	Region
	Quantity int64 `json:"quantity"`
}

type BaremetalPlan struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Specs struct {
		CPU             string `json:"cpu"`
		CPUManufacturer string `json:"cpuManufacturer"`
		CPUSockets      int64  `json:"cpuSockets"`
		CPUCores        int64  `json:"cpuCores"`
		BaseClockMhz    int64  `json:"cpuBaseClockMhz"`
		BoostClockMhz   int64  `json:"cpuBoostClockMhz"`
		MemoryGB        int64  `json:"memoryGb"`
		MemoryType      string `json:"memoryType"`
		Drives          []struct {
			Type      string `json:"type"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"drives"`
		Interfaces []struct {
			SpeedMbps int64 `json:"speedMbps"`
		} `json:"interfaces"`
		// IncludedEgressGB is nil when egress is unlimited.
		IncludedEgressGB *int64 `json:"includedEgressGb"`
	} `json:"specs"`
	Pricing      Pricing        `json:"pricing"`
	Availability []Availability `json:"availability"`
}

func (c *Client) ListBaremetalPlans(ctx context.Context) ([]BaremetalPlan, error) {
	return listAll[BaremetalPlan](ctx, c, "/v1/baremetal/plans", nil)
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

// ListIPBlockPlans lists the plans, only those offered in datacenter (an id
// or a code) when it is set.
func (c *Client) ListIPBlockPlans(ctx context.Context, datacenter string) ([]IPBlockPlan, error) {
	q := url.Values{}
	if datacenter != "" {
		q.Set("datacenter", datacenter)
	}
	return listAll[IPBlockPlan](ctx, c, ipBlocksPath+"/plans", q)
}

// ---------------------------------------------------------------- Servers (read only)

type ServerInterface struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	MacAddress string  `json:"macAddress"`
	SpeedMbps  int64   `json:"speedMbps"`
	SegmentID  *string `json:"segmentId"`
}

type ServerIPBlock struct {
	ID           string   `json:"id"`
	CIDR         string   `json:"cidr"`
	Gateway      *string  `json:"gateway"`
	SubnetMask   string   `json:"subnetMask"`
	PrefixLength int64    `json:"prefixLength"`
	Addresses    []string `json:"addresses"`
}

type ServerNetwork struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Type               string          `json:"type"`
	ManagedBy          string          `json:"managedBy"`
	LocalVlanID        *int64          `json:"localVlanId"`
	ProvisioningVlanID int64           `json:"provisioningVlanId"`
	IsNative           *bool           `json:"isNative"`
	Prefixes           []ServerIPBlock `json:"prefixes"`
}

// ServerSegment is a port or a bond of ports, and the networks on it.
type ServerSegment struct {
	SegmentID          string            `json:"segmentId"`
	Name               string            `json:"name"`
	BondMode           *string           `json:"bondMode"`
	PhysicalInterfaces []ServerInterface `json:"physicalInterfaces"`
	Networks           []ServerNetwork   `json:"networks"`
}

type Server struct {
	ID          string  `json:"id"`
	Name        *string `json:"name"`
	State       string  `json:"state"`
	IsLocked    bool    `json:"isLocked"`
	IsSuspended bool    `json:"isSuspended"`
	PrimaryIPv4 *string `json:"primaryIpv4"`
	PrimaryIPv6 *string `json:"primaryIpv6"`
	CreatedAt   string  `json:"createdAt"`
	Region      struct {
		Region
		Rack *string `json:"rack"`
	} `json:"region"`
	OperatingSystem *struct {
		ID   string  `json:"id"`
		Slug *string `json:"slug"`
		Name string  `json:"name"`
	} `json:"operatingSystem"`
	Interfaces []ServerSegment `json:"interfaces"`
}

func (c *Client) GetServer(ctx context.Context, id string) (*Server, error) {
	var out Server
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/baremetal/" + pathEscape(id)}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
