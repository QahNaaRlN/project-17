// Package agentpolicy evaluates the conditional agent policy (STU-420…439,
// 09 §2.3–2.4). Evaluation is pure: no network, no storage. The policy only
// narrows the agent's ceiling of rights; it never grants a right beyond it.
package agentpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Document — the policy document v1 (STU-420).
type Document struct {
	Version   int            `json:"version"`
	Rules     []Rule         `json:"rules"`
	Risk      []RiskRule     `json:"risk"`
	Approvals []ApprovalRule `json:"approvals"`
}

// Rule — conditional rights of agents.
type Rule struct {
	ID              string   `json:"id"`
	Subject         Subject  `json:"subject"`
	Allow           []string `json:"allow,omitempty"`
	Deny            []string `json:"deny,omitempty"`
	RequireApproval []string `json:"requireApproval,omitempty"`
	When            *When    `json:"when,omitempty"`
}

// Subject — "*" or the name of a registered agent (decision §11.1).
type Subject struct {
	Agent string `json:"agent"`
}

// RiskRule raises the risk of a Change Set (STU-428).
type RiskRule struct {
	ID   string `json:"id"`
	When *When  `json:"when,omitempty"`
	Min  string `json:"min"`
}

// ApprovalRule requires approvals for a Change Set (STU-429).
type ApprovalRule struct {
	ID      string  `json:"id"`
	When    *When   `json:"when,omitempty"`
	Require Require `json:"require"`
}

// Require — approvals count and roles.
type Require struct {
	Count int      `json:"count"`
	Roles []string `json:"roles,omitempty"`
}

// When — conditions: AND between fields, OR inside one field (STU-421).
type When struct {
	Environment   []string `json:"environment,omitempty"`
	PathPrefix    []string `json:"pathPrefix,omitempty"`
	ZoneMode      []string `json:"zoneMode,omitempty"`
	ZoneID        []string `json:"zoneId,omitempty"`
	OperationType []string `json:"operationType,omitempty"`
	Schema        []string `json:"schema,omitempty"`
	Risk          []string `json:"risk,omitempty"`
	Source        []string `json:"source,omitempty"`
}

// Situation — what an operation or a Change Set looks like (STU-423).
type Situation struct {
	Agent         string   `json:"agent,omitempty"`
	Environments  []string `json:"environments,omitempty"`
	Path          string   `json:"path,omitempty"`
	ZoneMode      string   `json:"zoneMode,omitempty"`
	ZoneID        string   `json:"zoneId,omitempty"`
	OperationType string   `json:"operationType,omitempty"`
	Schema        string   `json:"schema,omitempty"`
	Risk          string   `json:"risk,omitempty"`
	Source        string   `json:"source,omitempty"`
}

// Decision — the outcome for one right (STU-422/426).
type Decision struct {
	Allowed   bool   `json:"allowed"`
	Approval  bool   `json:"approval,omitempty"`
	RuleID    string `json:"ruleId,omitempty"`
	Condition string `json:"condition,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Reasons of a refusal (STU-426).
const (
	ReasonNoRule         = "no_rule"
	ReasonDenied         = "denied_by_rule"
	ReasonOutsideCeiling = "outside_ceiling"
)

var (
	idPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,119}$`)
	risks       = []string{"low", "medium", "high"}
	modes       = []string{"STRICT", "SYSTEM", "FREE", "CODE"}
	sources     = []string{"studio", "api", "agent", "migration", "import"}
)

// Known — the project values the conditions may name (STU-421).
type Known struct {
	Rights         []string
	Environments   []string
	OperationTypes []string
	Schemas        []string
}

// Invalid — a refused document with the field and the cause.
type Invalid struct {
	Field, Cause string
}

func (e *Invalid) Error() string { return e.Field + ": " + e.Cause }

// Parse decodes a document strictly: unknown fields are refused.
func Parse(raw []byte) (Document, error) {
	var d Document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, &Invalid{"policy", err.Error()}
	}
	return d, nil
}

// Validate — STU-420/421: limits, unique ids, known values.
func Validate(d Document, k Known) error {
	if d.Version != 1 {
		return &Invalid{"version", "1"}
	}
	if len(d.Rules) > 100 || len(d.Risk) > 100 || len(d.Approvals) > 100 {
		return &Invalid{"rules", "at most 100 of each kind"}
	}
	seen := map[string]bool{}
	id := func(field, v string) error {
		if !idPattern.MatchString(v) || seen[v] {
			return &Invalid{field, "unique id " + idPattern.String()}
		}
		seen[v] = true
		return nil
	}
	for i, r := range d.Rules {
		field := fmt.Sprintf("rules[%d]", i)
		if err := id(field+".id", r.ID); err != nil {
			return err
		}
		if r.Subject.Agent != "*" && !namePattern.MatchString(r.Subject.Agent) {
			return &Invalid{field + ".subject", "agent: \"*\" or an agent name"}
		}
		if len(r.Allow)+len(r.Deny)+len(r.RequireApproval) == 0 {
			return &Invalid{field, "allow, deny or requireApproval"}
		}
		for _, list := range [][]string{r.Allow, r.Deny, r.RequireApproval} {
			for _, right := range list {
				if !slices.Contains(k.Rights, right) {
					return &Invalid{field, "unknown or privileged right " + right}
				}
			}
		}
		if err := validWhen(field+".when", r.When, k); err != nil {
			return err
		}
	}
	for i, r := range d.Risk {
		field := fmt.Sprintf("risk[%d]", i)
		if err := id(field+".id", r.ID); err != nil {
			return err
		}
		if !slices.Contains(risks, r.Min) {
			return &Invalid{field + ".min", "low, medium or high"}
		}
		if err := validWhen(field+".when", r.When, k); err != nil {
			return err
		}
	}
	for i, r := range d.Approvals {
		field := fmt.Sprintf("approvals[%d]", i)
		if err := id(field+".id", r.ID); err != nil {
			return err
		}
		if r.Require.Count < 0 || r.Require.Count > 5 || len(r.Require.Roles) > 10 {
			return &Invalid{field + ".require", "count 0…5, at most 10 roles"}
		}
		if err := validWhen(field+".when", r.When, k); err != nil {
			return err
		}
	}
	return nil
}

func validWhen(field string, w *When, k Known) error {
	if w == nil {
		return nil
	}
	check := func(name string, values, allowed []string, open bool) error {
		if len(values) > 50 {
			return &Invalid{field + "." + name, "at most 50 values"}
		}
		for _, v := range values {
			if v == "" || !open && !slices.Contains(allowed, v) {
				return &Invalid{field + "." + name, "unknown value " + v}
			}
		}
		return nil
	}
	for _, c := range []struct {
		name    string
		values  []string
		allowed []string
		open    bool
	}{
		{"environment", w.Environment, k.Environments, false},
		{"zoneMode", w.ZoneMode, modes, false},
		{"operationType", w.OperationType, k.OperationTypes, false},
		{"schema", w.Schema, k.Schemas, false},
		{"risk", w.Risk, risks, false},
		{"source", w.Source, sources, false},
		{"zoneId", w.ZoneID, nil, true},
		{"pathPrefix", w.PathPrefix, nil, true},
	} {
		if err := check(c.name, c.values, c.allowed, c.open); err != nil {
			return err
		}
	}
	for _, p := range w.PathPrefix {
		if !strings.HasPrefix(p, "/") || len(p) > 500 {
			return &Invalid{field + ".pathPrefix", "starts with /"}
		}
	}
	return nil
}

// matches — STU-421/423/424: an absent condition matches; environment uses
// any target for deny and all targets otherwise; risk is checked only when
// the situation has one (review time).
func matches(w *When, s Situation, deny bool) (bool, string) {
	if w == nil {
		return true, ""
	}
	in := func(values []string, v string) bool { return len(values) == 0 || slices.Contains(values, v) }
	if len(w.Environment) > 0 {
		any, all := false, len(s.Environments) > 0
		for _, e := range s.Environments {
			if slices.Contains(w.Environment, e) {
				any = true
			} else {
				all = false
			}
		}
		if deny && !any || !deny && !all {
			return false, ""
		}
	}
	if len(w.PathPrefix) > 0 {
		hit := false
		for _, p := range w.PathPrefix {
			if s.Path != "" && strings.HasPrefix(s.Path, p) {
				hit = true
			}
		}
		if !hit {
			return false, ""
		}
	}
	if len(w.Risk) > 0 && (s.Risk == "" || !slices.Contains(w.Risk, s.Risk)) {
		return false, ""
	}
	if !in(w.ZoneMode, s.ZoneMode) || !in(w.ZoneID, s.ZoneID) || !in(w.OperationType, s.OperationType) || !in(w.Schema, s.Schema) || !in(w.Source, s.Source) {
		return false, ""
	}
	return true, describe(w)
}

// describe — a short condition text for refusals (STU-426).
func describe(w *When) string {
	parts := []string{}
	add := func(name string, values []string) {
		if len(values) > 0 {
			parts = append(parts, name+"="+strings.Join(values, ","))
		}
	}
	add("environment", w.Environment)
	add("pathPrefix", w.PathPrefix)
	add("zoneMode", w.ZoneMode)
	add("zoneId", w.ZoneID)
	add("operationType", w.OperationType)
	add("schema", w.Schema)
	add("risk", w.Risk)
	add("source", w.Source)
	return strings.Join(parts, "; ")
}

func subjectMatches(r Rule, agent string) bool {
	return r.Subject.Agent == "*" || r.Subject.Agent == agent
}

// HasRisk — the rule depends on risk, known only at review (STU-424).
func HasRisk(w *When) bool { return w != nil && len(w.Risk) > 0 }

// Decide — AGT-010/011 for one right within the agent's ceiling. Rules with
// a risk condition apply only when the situation carries a risk.
func Decide(d Document, ceiling []string, right string, s Situation) Decision {
	if !slices.Contains(ceiling, right) {
		return Decision{Reason: ReasonOutsideCeiling}
	}
	applicable := func(r Rule) bool {
		return subjectMatches(r, s.Agent) && (s.Risk != "" || !HasRisk(r.When))
	}
	for _, r := range d.Rules {
		if ok, cond := matches(r.When, s, true); applicable(r) && ok && slices.Contains(r.Deny, right) {
			return Decision{RuleID: r.ID, Condition: cond, Reason: ReasonDenied}
		}
	}
	allows := false
	for _, r := range d.Rules {
		if !applicable(r) || len(r.Allow) == 0 {
			continue
		}
		if ok, _ := matches(r.When, s, false); ok {
			allows = true
			if slices.Contains(r.Allow, right) {
				return Decision{Allowed: true, RuleID: r.ID}
			}
		}
	}
	for _, r := range d.Rules {
		if ok, cond := matches(r.When, s, false); applicable(r) && ok && slices.Contains(r.RequireApproval, right) {
			return Decision{Allowed: true, Approval: true, RuleID: r.ID, Condition: cond}
		}
	}
	// Decision §11.6: without a matching allow the agent keeps its ceiling.
	if !allows {
		return Decision{Allowed: true}
	}
	return Decision{Reason: ReasonNoRule}
}

// MinRisk — the highest minimum of matching risk rules (STU-428).
func MinRisk(d Document, s Situation, base string) (string, string) {
	rank := func(r string) int { return slices.Index(risks, r) }
	risk, rule := base, ""
	for _, r := range d.Risk {
		if ok, _ := matches(r.When, s, false); ok && rank(r.Min) > rank(risk) {
			risk, rule = r.Min, r.ID
		}
	}
	return risk, rule
}

// Approvals — the highest count and the union of roles of matching approval
// rules (STU-429).
func Approvals(d Document, s Situation) (int, []string, []string) {
	count, roles, ids := 0, []string{}, []string{}
	for _, r := range d.Approvals {
		if ok, _ := matches(r.When, s, false); !ok {
			continue
		}
		ids = append(ids, r.ID)
		count = max(count, r.Require.Count)
		for _, role := range r.Require.Roles {
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	return count, roles, ids
}

// Ceiling — the unconditional part (STU-436): rights not denied by a rule
// without conditions, and, when the agent has allow rules, only rights some
// allow or requireApproval rule names.
func Ceiling(d Document, agent string, ceiling []string) []string {
	out := []string{}
	allows := map[string]bool{}
	hasAllow := false
	for _, r := range d.Rules {
		if !subjectMatches(r, agent) {
			continue
		}
		if len(r.Allow) > 0 {
			hasAllow = true
		}
		for _, right := range append(append([]string{}, r.Allow...), r.RequireApproval...) {
			allows[right] = true
		}
	}
	for _, right := range ceiling {
		denied := false
		for _, r := range d.Rules {
			if subjectMatches(r, agent) && r.When == nil && slices.Contains(r.Deny, right) {
				denied = true
			}
		}
		if !denied && (!hasAllow || allows[right]) {
			out = append(out, right)
		}
	}
	return out
}

// Legacy — the v1 document of the MVP allow/deny policy (STU-432).
func Legacy(allow, deny []string) Document {
	d := Document{Version: 1, Rules: []Rule{}, Risk: []RiskRule{}, Approvals: []ApprovalRule{}}
	if len(allow) > 0 || len(deny) > 0 {
		d.Rules = append(d.Rules, Rule{ID: "legacy", Subject: Subject{Agent: "*"}, Allow: allow, Deny: deny})
	}
	return d
}
