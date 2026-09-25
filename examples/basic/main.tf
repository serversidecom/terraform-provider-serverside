terraform {
  required_providers {
    serverside = {
      source = "serversidecom/serverside"
    }
  }
}

# The key comes from SERVERSIDE_API_KEY when api_key is not set.
provider "serverside" {}

variable "server_id" {
  description = "Id of an existing bare metal server, from the cloud console."
  type        = string
}

variable "office_cidr" {
  description = "Network allowed to reach SSH."
  type        = string
}

resource "serverside_ssh_key" "deploy" {
  name       = "deploy"
  public_key = file("~/.ssh/id_ed25519.pub")
}

data "serverside_baremetal_server" "web" {
  id = var.server_id
}

# Unmatched traffic is allowed, so SSH is closed by the BLOCK rule after the
# office ALLOW, not by the ALLOW alone.
resource "serverside_firewall_group" "web" {
  name = "web"

  rules = [
    {
      action                 = "ALLOW"
      protocol               = "TCP"
      source_address         = var.office_cidr
      destination_port_start = 22
      destination_port_end   = 22
      description            = "SSH from the office"
    },
    {
      action                 = "BLOCK"
      protocol               = "TCP"
      destination_port_start = 22
      destination_port_end   = 22
      description            = "SSH from anywhere else"
    },
  ]
}

locals {
  public_network = one([for n in data.serverside_baremetal_server.web.networks : n if n.type == "PUBLIC"])
}

data "serverside_ip_address" "web" {
  network_id = local.public_network.id
  address    = data.serverside_baremetal_server.web.primary_ipv4
}

resource "serverside_firewall_group_assignment" "web" {
  firewall_group_id = serverside_firewall_group.web.id
  ip_address_id     = data.serverside_ip_address.web.id
}

resource "serverside_virtual_network" "backend" {
  name    = "backend"
  vlan_id = 100
}

resource "serverside_virtual_network_attachment" "web_backend" {
  network_id = serverside_virtual_network.backend.id
  server_id  = data.serverside_baremetal_server.web.id
  segment_id = data.serverside_baremetal_server.web.segments[0].segment_id
}
