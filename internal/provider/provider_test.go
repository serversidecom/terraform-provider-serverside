package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// TestSchemasAreValid runs the framework's own schema validation (defaults
// only on computed attributes, valid names, and so on) for every resource and
// data source, without a Terraform binary or an API.
func TestSchemasAreValid(t *testing.T) {
	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("%s: %s: %s", d.Severity, d.Summary, d.Detail)
	}
	for _, name := range []string{
		"serverside_ssh_key", "serverside_firewall_group", "serverside_firewall_group_assignment",
		"serverside_virtual_network", "serverside_virtual_network_attachment", "serverside_ip_block",
	} {
		if _, ok := resp.ResourceSchemas[name]; !ok {
			t.Errorf("resource %s is not registered", name)
		}
	}
	for _, name := range []string{
		"serverside_datacenters", "serverside_baremetal_plans", "serverside_ip_block_plans",
		"serverside_baremetal_server", "serverside_ip_address",
	} {
		if _, ok := resp.DataSourceSchemas[name]; !ok {
			t.Errorf("data source %s is not registered", name)
		}
	}
}
