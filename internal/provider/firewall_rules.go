package provider

import (
	"reflect"

	"github.com/serversidecom/terraform-provider-serverside/internal/client"
)

// desiredRule is one rule as configured, in position.
type desiredRule struct {
	Spec    client.FirewallRuleSpec
	Enabled bool
}

// existingRule is one rule as stored, in priority order.
type existingRule struct {
	ID      string
	Spec    client.FirewallRuleSpec
	Enabled bool
}

// rulePlan is the set of API calls that turns the stored rules into the
// configured ones.
type rulePlan struct {
	// Updates rewrite a stored rule in place (the API's PATCH replaces every
	// field, so the whole desired rule is sent).
	Updates []ruleUpdate
	// Creates are appended to the group in this order.
	Creates []int // indexes into desired
	Deletes []string
	// Order is the final rule order: for each desired index, either an
	// existing rule id or -1 for a rule that Creates will make. Resolved once
	// the created ids are known.
	Order []orderSlot
	// NeedsReorder is false when the rules end up in the desired order
	// without a ruleOrder call.
	NeedsReorder bool
}

type ruleUpdate struct {
	ID      string
	Desired int
}

type orderSlot struct {
	ExistingID string // set when the slot keeps or updates a stored rule
	Created    int    // position in Creates when ExistingID is empty
}

// planRules matches configured rules to stored ones. Rules whose definition
// is unchanged keep their id, so inserting a rule at the top of a long list
// costs one create and one reorder instead of rewriting every rule. Leftover
// configured rules then reuse leftover stored rules in order (an update), and
// what remains is created or deleted.
func planRules(existing []existingRule, desired []desiredRule) rulePlan {
	used := make([]bool, len(existing))
	match := make([]int, len(desired)) // index into existing, or -1
	for i := range match {
		match[i] = -1
	}

	// Pass 1: identical rules.
	for d := range desired {
		for e := range existing {
			if !used[e] && existing[e].Enabled == desired[d].Enabled && reflect.DeepEqual(existing[e].Spec, desired[d].Spec) {
				used[e], match[d] = true, e
				break
			}
		}
	}

	var p rulePlan

	// Pass 2: pair the rest positionally.
	next := 0
	for d := range desired {
		if match[d] != -1 {
			continue
		}
		for next < len(existing) && used[next] {
			next++
		}
		if next < len(existing) {
			used[next], match[d] = true, next
			p.Updates = append(p.Updates, ruleUpdate{ID: existing[next].ID, Desired: d})
			continue
		}
		p.Creates = append(p.Creates, d)
	}
	for e := range existing {
		if !used[e] {
			p.Deletes = append(p.Deletes, existing[e].ID)
		}
	}

	created := 0
	for d := range desired {
		if match[d] == -1 {
			p.Order = append(p.Order, orderSlot{Created: created})
			created++
		} else {
			p.Order = append(p.Order, orderSlot{ExistingID: existing[match[d]].ID})
		}
	}

	// After the calls, the API holds the kept rules in their old relative
	// order followed by the created ones. Reorder only if that differs.
	lastExisting := -1
	seenCreated := false
	for d := range desired {
		if match[d] == -1 {
			seenCreated = true
			continue
		}
		if seenCreated || match[d] < lastExisting {
			p.NeedsReorder = true
			break
		}
		lastExisting = match[d]
	}
	return p
}
