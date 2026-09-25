package provider

import (
	"testing"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

func spec(proto string, port int64) client.FirewallRuleSpec {
	return client.FirewallRuleSpec{Type: "BLOCK", Direction: "INBOUND", Protocol: proto, DestPortStart: &port, DestPortEnd: &port}
}

func TestPlanRulesNoChange(t *testing.T) {
	existing := []existingRule{{ID: "a", Spec: spec("TCP", 22), Enabled: true}, {ID: "b", Spec: spec("UDP", 53), Enabled: true}}
	desired := []desiredRule{{Spec: spec("TCP", 22), Enabled: true}, {Spec: spec("UDP", 53), Enabled: true}}
	p := planRules(existing, desired)
	if len(p.Updates)+len(p.Creates)+len(p.Deletes) != 0 || p.NeedsReorder {
		t.Fatalf("expected no calls, got %+v", p)
	}
}

func TestPlanRulesInsertAtTop(t *testing.T) {
	existing := []existingRule{{ID: "a", Spec: spec("TCP", 22), Enabled: true}, {ID: "b", Spec: spec("UDP", 53), Enabled: true}}
	desired := []desiredRule{{Spec: spec("TCP", 443), Enabled: true}, {Spec: spec("TCP", 22), Enabled: true}, {Spec: spec("UDP", 53), Enabled: true}}
	p := planRules(existing, desired)
	if len(p.Creates) != 1 || p.Creates[0] != 0 || len(p.Updates) != 0 || len(p.Deletes) != 0 {
		t.Fatalf("expected one create at index 0, got %+v", p)
	}
	if !p.NeedsReorder {
		t.Fatal("a rule created at the top needs a reorder")
	}
	if p.Order[0].ExistingID != "" || p.Order[1].ExistingID != "a" || p.Order[2].ExistingID != "b" {
		t.Fatalf("wrong order %+v", p.Order)
	}
}

func TestPlanRulesAppendNeedsNoReorder(t *testing.T) {
	existing := []existingRule{{ID: "a", Spec: spec("TCP", 22), Enabled: true}}
	desired := []desiredRule{{Spec: spec("TCP", 22), Enabled: true}, {Spec: spec("TCP", 80), Enabled: true}}
	p := planRules(existing, desired)
	if len(p.Creates) != 1 || p.NeedsReorder {
		t.Fatalf("append should be one create without reorder, got %+v", p)
	}
}

func TestPlanRulesChangedRuleIsUpdatedInPlace(t *testing.T) {
	existing := []existingRule{{ID: "a", Spec: spec("TCP", 22), Enabled: true}, {ID: "b", Spec: spec("UDP", 53), Enabled: true}}
	desired := []desiredRule{{Spec: spec("TCP", 2222), Enabled: true}, {Spec: spec("UDP", 53), Enabled: true}}
	p := planRules(existing, desired)
	if len(p.Updates) != 1 || p.Updates[0].ID != "a" || p.Updates[0].Desired != 0 || len(p.Creates) != 0 || len(p.Deletes) != 0 {
		t.Fatalf("expected rule a updated in place, got %+v", p)
	}
	if p.NeedsReorder {
		t.Fatal("an in-place update keeps the order")
	}
}

func TestPlanRulesDisableIsAnUpdate(t *testing.T) {
	existing := []existingRule{{ID: "a", Spec: spec("TCP", 22), Enabled: true}}
	desired := []desiredRule{{Spec: spec("TCP", 22), Enabled: false}}
	p := planRules(existing, desired)
	if len(p.Updates) != 1 || p.Updates[0].ID != "a" {
		t.Fatalf("expected an update, got %+v", p)
	}
}

func TestPlanRulesRemoveAndSwap(t *testing.T) {
	existing := []existingRule{
		{ID: "a", Spec: spec("TCP", 22), Enabled: true},
		{ID: "b", Spec: spec("UDP", 53), Enabled: true},
		{ID: "c", Spec: spec("TCP", 80), Enabled: true},
	}
	desired := []desiredRule{{Spec: spec("TCP", 80), Enabled: true}, {Spec: spec("TCP", 22), Enabled: true}}
	p := planRules(existing, desired)
	if len(p.Deletes) != 1 || p.Deletes[0] != "b" || len(p.Creates) != 0 || len(p.Updates) != 0 {
		t.Fatalf("expected b deleted only, got %+v", p)
	}
	if !p.NeedsReorder || p.Order[0].ExistingID != "c" || p.Order[1].ExistingID != "a" {
		t.Fatalf("expected reorder to c,a, got %+v", p)
	}
}

func TestPlanRulesEmptyDesiredDeletesAll(t *testing.T) {
	existing := []existingRule{{ID: "a", Spec: spec("TCP", 22), Enabled: true}, {ID: "b", Spec: spec("UDP", 53), Enabled: true}}
	p := planRules(existing, nil)
	if len(p.Deletes) != 2 || p.NeedsReorder {
		t.Fatalf("expected two deletes, got %+v", p)
	}
}
