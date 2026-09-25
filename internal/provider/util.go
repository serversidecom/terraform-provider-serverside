package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

// clientFromResource extracts the API client handed over by Configure.
func clientFromResource(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return nil
	}
	return c
}

func clientFromDataSource(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return nil
	}
	return c
}

func addAPIError(d *diag.Diagnostics, action string, err error) {
	d.AddError("Serverside.com API: "+action, err.Error())
}

func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	x := v.ValueInt64()
	return &x
}

func stringPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	x := v.ValueString()
	return &x
}

func int64Value(p *int64) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

func stringValue(p *string) types.String {
	if p == nil || *p == "" {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// splitID splits a composite import id such as "<group>/<ip>".
func splitID(id string, n int) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != n {
		return nil, fmt.Errorf("expected an id with %d parts separated by '/', got %q", n, id)
	}
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("empty part in id %q", id)
		}
	}
	return parts, nil
}

// sameSSHKey compares the type and key material of two OpenSSH public keys,
// ignoring the comment and surrounding whitespace.
func sameSSHKey(a, b string) bool {
	fa, fb := strings.Fields(a), strings.Fields(b)
	if len(fa) < 2 || len(fb) < 2 {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return fa[0] == fb[0] && fa[1] == fb[1]
}
