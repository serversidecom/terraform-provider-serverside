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

resource "serverside_ssh_key" "deploy" {
  name       = "deploy"
  public_key = file("~/.ssh/id_ed25519.pub")
}

data "serverside_baremetal_server" "web" {
  id = var.server_id
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
