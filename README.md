# Serverside.com Terraform provider

Manages SSH keys, firewall groups, virtual networks and IP blocks on [Serverside.com](https://serverside.com) bare metal through the [Serverside.com API](https://serverside.com/docs/api-reference) (`https://api.serverside.com`). Bare metal servers are read, not created: see [Not covered](#not-covered).

The provider address is `registry.terraform.io/serversidecom/serverside`. The binary and this repository are both named `terraform-provider-serverside`, which is how Terraform finds the plugin and what the Terraform Registry requires of a published provider's repository.

## Resources and data sources

| Name | Kind | What it does | Serverside.com docs |
| --- | --- | --- | --- |
| `serverside_ssh_key` | resource | An organization SSH key. Renames in place; a new key material replaces it. | [Deploy your first server](https://serverside.com/docs/guides/deploy-first-server) |
| `serverside_firewall_group` | resource | A firewall group with its rules as an ordered list. | [Configuring firewalls](https://serverside.com/docs/guides/configuring-firewalls) |
| `serverside_firewall_group_assignment` | resource | Applies a group to one IP address. | [Configuring firewalls](https://serverside.com/docs/guides/configuring-firewalls) |
| `serverside_virtual_network` | resource | A private (layer 2) or public virtual network with its VLAN tag. | [Setting up virtual networks](https://serverside.com/docs/guides/setting-up-virtual-networks) |
| `serverside_virtual_network_attachment` | resource | Attaches a server's network segment to a virtual network. | [Setting up virtual networks](https://serverside.com/docs/guides/setting-up-virtual-networks) |
| `serverside_ip_block` | resource | An hourly-billed IP block on a public network. | [Managing IP addresses](https://serverside.com/docs/guides/managing-ip-addresses) |
| `serverside_datacenters` | data source | Datacenters for networks and IP blocks. | [Networking concepts](https://serverside.com/docs/concepts/networking) |
| `serverside_baremetal_plans` | data source | Plans with specifications, prices and stock, filterable by datacenter. | [Billing](https://serverside.com/docs/concepts/billing) |
| `serverside_ip_block_plans` | data source | IP block sizes with prices and stock. | [Managing IP addresses](https://serverside.com/docs/guides/managing-ip-addresses) |
| `serverside_baremetal_server` | data source | An existing server's segments and networks. | [Bare metal services](https://serverside.com/docs/concepts/baremetal-services) |
| `serverside_ip_address` | data source | The API id of an address, which firewall assignments need. | [Managing IP addresses](https://serverside.com/docs/guides/managing-ip-addresses) |

[examples/basic/main.tf](examples/basic/main.tf) uses most of them together.

## Authentication

Create an API key in the [Serverside.com cloud console](https://cloud.serverside.com) (steps in [Authentication](https://serverside.com/docs/authentication)), then either set `SERVERSIDE_API_KEY` or pass `api_key` in the provider block. `api_url` (or `SERVERSIDE_API_URL`) points the provider at another API host.

A key carries the default Admin permissions of the organization as they were when the key was created. The API has no way to issue a narrower key yet.

## API behaviour to plan for

- **Firewall rules are first-match, stateless and default-allow.** Rules on an address are matched by priority across every group assigned to it, and traffic no rule matches is allowed, so a group of `ALLOW` rules alone filters nothing. End an allow list with a `BLOCK` rule. `OUTBOUND` rules are accepted by the API, but whether they are enforced is not confirmed.
- **Rule order is the list order.** The provider keeps a rule's API id when its definition is unchanged, so inserting a rule at the top of a long list is one create and one reorder. The API's rule `PATCH` replaces every field, so the provider always sends the whole rule.
- **Attaching a server to a network is asynchronous.** The API answers `202`; the provider polls until the attachment reads `ATTACHED` (or `ATTACH_FAILED`), for up to 15 minutes. Detaching waits the same way. A locked server refuses both.
- **A virtual network cannot be deleted while servers are attached or IP blocks remain on it.** Terraform destroys attachments first when they are in the same configuration.
- **IP blocks are billed hourly** and need the organization's usage-based billing entitlement (`422 USAGE_BILLING_DISABLED` otherwise). Destroying one requests cancellation of its subscription; an unassigned block is released at once.
- **Idempotency keys cover retries within one request only.** Creates that the API supports it for (IP blocks, attachments) send a fresh `Idempotency-Key` and reuse it on the client's own retries after a 429, 502, 503 or 504. A key is not kept between Terraform runs.

## Not covered

- **Bare metal servers.** A customer `DELETE` on a server is a cancellation request: the API answers `204`, opens a ticket, and the server keeps running and reads `ACTIVE` until staff remove it. `terraform destroy` would report success while the server stays up, so the resource waits for the API to change. Use `serverside_baremetal_server` to read servers ordered in the console.
- **Organization, members, roles and API keys.** Those API routes accept a console session only, not an API key.
- **Billing, invoices, power, KVM and virtual media.** These are actions rather than infrastructure.

## Documentation

The provider is a thin layer over the Serverside.com API, and the [Serverside.com documentation](https://serverside.com/docs) describes the same objects in more depth:

- [Quickstart](https://serverside.com/docs/quickstart): an API key, a plan and a first server, step by step.
- [API reference](https://serverside.com/docs/api-reference): every public operation, with request samples in nine languages and a Try it console.
- [API conventions](https://serverside.com/docs/api-conventions): the response envelope, error codes, paging and idempotency keys the provider relies on.
- [Networking concepts](https://serverside.com/docs/concepts/networking): how virtual networks, VLANs, prefixes, IP blocks and firewall groups fit together.

## Import

| Resource | Import id |
| --- | --- |
| `serverside_ssh_key`, `serverside_firewall_group`, `serverside_virtual_network` | the API id |
| `serverside_firewall_group_assignment` | `<firewall_group_id>/<ip_address_id>` |
| `serverside_virtual_network_attachment` | `<network_id>/<server_id>/<segment_id>` |
| `serverside_ip_block` | `<network_id>/<prefix_id>`; set `plan_id` in the configuration, the API cannot report it |

## About Serverside.com

[Serverside.com](https://serverside.com) runs bare metal servers on its own network, AS55285. This provider covers the networking and access around those servers; the servers themselves are ordered in the console or the API.

- [Bare metal cloud servers](https://serverside.com/bare-metal): whole machines with 20 to 200 Gbps per server, deployed from the console or the API. Stock configurations are live in under a minute; custom builds are quoted.
- [Dedicated servers](https://serverside.com/dedicated-servers) in seven metros, with 24/7 engineer support. [Datacenters](https://serverside.com/datacenters) lists the facilities.
- [Private networking](https://serverside.com/private-networking): layer 2 networks between your servers, extendable across datacenters, with jumbo frames and firewall groups at no extra cost. This is what `serverside_virtual_network` and `serverside_firewall_group` manage.
- [DDoS protection](https://serverside.com/ddos-protection): Corero mitigation inline at every location, layers 3 to 7, included with every server.
- [Our network](https://serverside.com/network): open peering at 6 Amsterdam exchanges and dual-stack IPv6. The [looking glass](https://serverside.com/looking-glass) runs ping, traceroute and BGP lookups from our routers.

## Building and local use

Requires Go 1.25.

```sh
go build -o terraform-provider-serverside .
go test ./...
```

To run a local build, point Terraform at it with a `dev_overrides` block in `~/.terraformrc` (`%APPDATA%\terraform.rc` on Windows) and skip `terraform init`:

```hcl
provider_installation {
  dev_overrides {
    "serversidecom/serverside" = "/path/to/terraform-provider-serverside"
  }
  direct {}
}
```

The tests cover the API client against a fake server, the firewall rule reconciliation, and the framework's schema validation. There are no acceptance tests against the live API yet: they would create real networks and IP blocks on a billed organization.
