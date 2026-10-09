// Package auth — права, акторы и аутентификация по API-токенам (docs/spec/09-agent.md §2–3).
package auth

import "slices"

// Right — право в модели доступа (09-agent.md §2.1).
type Right string

const (
	ContentRead             Right = "content.read"
	ContentWrite            Right = "content.write"
	ContentDelete           Right = "content.delete"
	ContentPublish          Right = "content.publish"
	AssetWrite              Right = "asset.write"
	DesignRead              Right = "design.read"
	DesignCompose           Right = "design.compose"
	DesignZonesManage       Right = "design.zones.manage"
	DesignComponentsCertify Right = "design.components.certify"
	DesignTokensModify      Right = "design.tokens.modify"
	ComponentWrite          Right = "component.write"
	BehaviorUse             Right = "behavior.use"
	BehaviorPropose         Right = "behavior.propose"
	CapabilityRequest       Right = "capability.request"
	CodePropose             Right = "code.propose"
	SchemaRead              Right = "schema.read"
	SchemaPropose           Right = "schema.propose"
	SchemaApply             Right = "schema.apply"
	ManifestRegister        Right = "manifest.register"
	AgentDelegate           Right = "agent.delegate"
	ProjectAdmin            Right = "project.admin"
)

// AllRights — полный перечень прав.
var AllRights = []Right{
	ContentRead, ContentWrite, ContentDelete, ContentPublish, AssetWrite,
	DesignRead, DesignCompose, DesignZonesManage, DesignComponentsCertify, DesignTokensModify,
	ComponentWrite, BehaviorUse, BehaviorPropose, CapabilityRequest, CodePropose,
	SchemaRead, SchemaPropose, SchemaApply, ManifestRegister, AgentDelegate, ProjectAdmin,
}

// ScopeAll — область токена «все права ролей актора».
const ScopeAll = "*"

// RightSet — множество прав.
type RightSet map[Right]bool

// Has сообщает, есть ли право.
func (s RightSet) Has(r Right) bool { return s[r] }

// Sorted — права в лексикографическом порядке.
func (s RightSet) Sorted() []Right {
	out := make([]Right, 0, len(s))
	for r := range s {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

// EffectiveRights — права ролей актора, ограниченные областями токена (ScopeAll — без ограничения).
// Неизвестные строки в ролях и областях игнорируются.
func EffectiveRights(roleCapabilities, tokenScopes []string) RightSet {
	known := make(map[Right]bool, len(AllRights))
	for _, r := range AllRights {
		known[r] = true
	}
	all := slices.Contains(tokenScopes, ScopeAll)
	out := RightSet{}
	for _, c := range roleCapabilities {
		r := Right(c)
		if known[r] && (all || slices.Contains(tokenScopes, c)) {
			out[r] = true
		}
	}
	return out
}
