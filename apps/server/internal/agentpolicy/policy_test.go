package agentpolicy

import (
	"errors"
	"testing"
)

var ceiling = []string{"content.read", "content.write", "design.read", "design.compose"}

func doc(rules ...Rule) Document { return Document{Version: 1, Rules: rules} }

// STU-422/423/424 (AGT-010/011): the decision table.
func TestSTU422DecisionTable(t *testing.T) {
	checkout := &When{PathPrefix: []string{"/checkout"}}
	prod := &When{Environment: []string{"production"}}
	high := &When{Risk: []string{"high"}}
	page := Situation{Agent: "copy", Path: "/checkout/cart", Environments: []string{"staging", "production"}}
	for _, c := range []struct {
		name  string
		d     Document
		right string
		s     Situation
		want  Decision
	}{
		{"no rules keeps the ceiling", doc(), "design.compose", page, Decision{Allowed: true}},
		{"outside the ceiling", doc(), "content.publish", page, Decision{Reason: ReasonOutsideCeiling}},
		{"deny by path", doc(Rule{ID: "no-checkout", Subject: Subject{"*"}, Deny: []string{"design.compose"}, When: checkout}), "design.compose", page, Decision{RuleID: "no-checkout", Condition: "pathPrefix=/checkout", Reason: ReasonDenied}},
		{"deny wins over allow", doc(Rule{ID: "a", Subject: Subject{"*"}, Allow: []string{"design.compose"}}, Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"design.compose"}}), "design.compose", page, Decision{RuleID: "d", Reason: ReasonDenied}},
		{"deny on any target", doc(Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"content.write"}, When: prod}), "content.write", page, Decision{RuleID: "d", Condition: "environment=production", Reason: ReasonDenied}},
		{"allow needs all targets", doc(Rule{ID: "a", Subject: Subject{"*"}, Allow: []string{"content.write"}, When: prod}), "content.write", page, Decision{Allowed: true}},
		{"allow of another agent", doc(Rule{ID: "a", Subject: Subject{"other"}, Allow: []string{"design.read"}}), "content.write", page, Decision{Allowed: true}},
		{"allow restricts", doc(Rule{ID: "a", Subject: Subject{"copy"}, Allow: []string{"design.read"}}), "content.write", page, Decision{Reason: ReasonNoRule}},
		{"allow grants", doc(Rule{ID: "a", Subject: Subject{"copy"}, Allow: []string{"content.write"}}), "content.write", page, Decision{Allowed: true, RuleID: "a"}},
		{"require approval", doc(Rule{ID: "a", Subject: Subject{"*"}, Allow: []string{"design.read"}}, Rule{ID: "r", Subject: Subject{"*"}, RequireApproval: []string{"content.write"}}), "content.write", page, Decision{Allowed: true, Approval: true, RuleID: "r"}},
		{"risk waits for review", doc(Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"content.write"}, When: high}), "content.write", page, Decision{Allowed: true}},
		{"risk at review", doc(Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"content.write"}, When: high}), "content.write", Situation{Agent: "copy", Risk: "high"}, Decision{RuleID: "d", Condition: "risk=high", Reason: ReasonDenied}},
		{"zone and operation", doc(Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"design.compose"}, When: &When{ZoneMode: []string{"STRICT"}, ZoneID: []string{"hero"}, OperationType: []string{"node.insert"}, Schema: []string{"Product"}, Source: []string{"agent"}}}), "design.compose", Situation{ZoneMode: "STRICT", ZoneID: "hero", OperationType: "node.insert", Schema: "Product", Source: "agent"}, Decision{RuleID: "d", Condition: "zoneMode=STRICT; zoneId=hero; operationType=node.insert; schema=Product; source=agent", Reason: ReasonDenied}},
		{"zone mismatch", doc(Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"design.compose"}, When: &When{ZoneMode: []string{"STRICT"}}}), "design.compose", Situation{ZoneMode: "FREE"}, Decision{Allowed: true}},
		{"entity has no path", doc(Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"content.write"}, When: checkout}), "content.write", Situation{}, Decision{Allowed: true}},
	} {
		if got := Decide(c.d, ceiling, c.right, c.s); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// STU-428/429/436/432: risk, approvals, the unconditional ceiling and legacy.
func TestSTU428_436RiskApprovalsCeiling(t *testing.T) {
	d := Document{Version: 1,
		Risk:      []RiskRule{{ID: "checkout", When: &When{PathPrefix: []string{"/checkout"}}, Min: "high"}, {ID: "agent", When: &When{Source: []string{"agent"}}, Min: "medium"}},
		Approvals: []ApprovalRule{{ID: "prod", When: &When{Environment: []string{"production"}}, Require: Require{Count: 2, Roles: []string{"lead"}}}, {ID: "agent", When: &When{Source: []string{"agent"}}, Require: Require{Count: 1, Roles: []string{"lead", "qa"}}}},
	}
	if risk, rule := MinRisk(d, Situation{Path: "/checkout", Source: "agent"}, "low"); risk != "high" || rule != "checkout" {
		t.Error("risk", risk, rule)
	}
	if risk, rule := MinRisk(d, Situation{Path: "/store"}, "medium"); risk != "medium" || rule != "" {
		t.Error("risk unchanged", risk, rule)
	}
	count, roles, ids := Approvals(d, Situation{Environments: []string{"production"}, Source: "agent"})
	if count != 2 || len(roles) != 2 || len(ids) != 2 {
		t.Error("approvals", count, roles, ids)
	}
	c := Ceiling(doc(Rule{ID: "d", Subject: Subject{"*"}, Deny: []string{"content.write"}}, Rule{ID: "c", Subject: Subject{"*"}, Deny: []string{"design.read"}, When: &When{PathPrefix: []string{"/x"}}}), "copy", ceiling)
	if len(c) != 3 {
		t.Error("an unconditional deny narrows; a conditional one does not", c)
	}
	if c := Ceiling(doc(Rule{ID: "a", Subject: Subject{"copy"}, Allow: []string{"design.read"}, RequireApproval: []string{"content.write"}}), "copy", ceiling); len(c) != 2 {
		t.Error("allow rules bound the ceiling", c)
	}
	if l := Legacy(nil, nil); len(l.Rules) != 0 || l.Version != 1 {
		t.Error("empty legacy")
	}
	if l := Legacy([]string{"design.read"}, []string{"content.write"}); l.Rules[0].ID != "legacy" {
		t.Error("legacy rule")
	}
}

// STU-420/421: document validation.
func TestSTU420_421Validation(t *testing.T) {
	k := Known{Rights: ceiling, Environments: []string{"staging", "production"}, OperationTypes: []string{"node.insert"}, Schemas: []string{"Product"}}
	good := Document{Version: 1,
		Rules:     []Rule{{ID: "r", Subject: Subject{"copy-assistant"}, Allow: []string{"content.read"}, When: &When{Environment: []string{"staging"}, PathPrefix: []string{"/store"}, ZoneMode: []string{"CODE"}, ZoneID: []string{"lab"}, OperationType: []string{"node.insert"}, Schema: []string{"Product"}, Risk: []string{"low"}, Source: []string{"agent"}}}},
		Risk:      []RiskRule{{ID: "k", Min: "high"}},
		Approvals: []ApprovalRule{{ID: "a", Require: Require{Count: 1}}},
	}
	if err := Validate(good, k); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Document){
		"version":        func(d *Document) { d.Version = 2 },
		"id":             func(d *Document) { d.Rules[0].ID = "Bad" },
		"duplicate":      func(d *Document) { d.Risk[0].ID = "r" },
		"subject":        func(d *Document) { d.Rules[0].Subject.Agent = "" },
		"no effect":      func(d *Document) { d.Rules[0].Allow = nil },
		"right":          func(d *Document) { d.Rules[0].Allow = []string{"project.admin"} },
		"environment":    func(d *Document) { d.Rules[0].When.Environment = []string{"qa"} },
		"path":           func(d *Document) { d.Rules[0].When.PathPrefix = []string{"store"} },
		"empty value":    func(d *Document) { d.Rules[0].When.ZoneID = []string{""} },
		"risk min":       func(d *Document) { d.Risk[0].Min = "huge" },
		"risk when":      func(d *Document) { d.Risk[0].When = &When{Source: []string{"robot"}} },
		"approval count": func(d *Document) { d.Approvals[0].Require.Count = 9 },
		"approval id":    func(d *Document) { d.Approvals[0].ID = "" },
		"approval when":  func(d *Document) { d.Approvals[0].When = &When{Risk: []string{"x"}} },
		"too many":       func(d *Document) { d.Risk = make([]RiskRule, 101) },
		"values":         func(d *Document) { d.Rules[0].When.ZoneID = make([]string, 51) },
	} {
		d := good
		d.Rules = []Rule{good.Rules[0]}
		w := *good.Rules[0].When
		d.Rules[0].When = &w
		d.Risk = []RiskRule{good.Risk[0]}
		d.Approvals = []ApprovalRule{good.Approvals[0]}
		change(&d)
		var invalid *Invalid
		if err := Validate(d, k); !errors.As(err, &invalid) || invalid.Error() == "" {
			t.Error("refused", name, err)
		}
	}
	if _, err := Parse([]byte(`{"version":1,"unknown":true}`)); err == nil {
		t.Error("unknown fields")
	}
	if d, err := Parse([]byte(`{"version":1,"rules":[]}`)); err != nil || d.Version != 1 {
		t.Error("parse", err)
	}
}
