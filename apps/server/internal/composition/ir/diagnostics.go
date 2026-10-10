package ir

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/schemadiag"
)

// Code — код диагностики (02-ir.md §11.2). Совпадает с DiagnosticCode в @cms/ir.
type Code string

const (
	CodeIrVersionUnsupported   Code = "IR_VERSION_UNSUPPORTED"
	CodeSchemaViolation        Code = "IR_SCHEMA_VIOLATION"
	CodeNodeNotFound           Code = "NODE_NOT_FOUND"
	CodeNodeIDMismatch         Code = "NODE_ID_MISMATCH"
	CodeNodeIDDuplicate        Code = "NODE_ID_DUPLICATE"
	CodeNodeOrphan             Code = "NODE_ORPHAN"
	CodeNodeMultipleParents    Code = "NODE_MULTIPLE_PARENTS"
	CodeNodeCycle              Code = "NODE_CYCLE"
	CodeLimitExceeded          Code = "LIMIT_EXCEEDED"
	CodePropBothStaticAndBound Code = "PROP_BOTH_STATIC_AND_BOUND"
)

// Severity — уровень диагностики.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic — сообщение валидатора. JSON-представление совпадает с @cms/ir.
type Diagnostic struct {
	Code     Code           `json:"code"`
	Severity Severity       `json:"severity"`
	Pointer  string         `json:"pointer"` // JSON Pointer (RFC 6901)
	NodeID   string         `json:"nodeId,omitempty"`
	Message  string         `json:"message"`
	Params   map[string]any `json:"params,omitempty"`
}

// PointerSegment экранирует сегмент JSON Pointer.
func PointerSegment(s string) string { return schemadiag.PointerSegment(s) }

// Pointer собирает JSON Pointer из сегментов; целые числа — индексы массивов.
func Pointer(segments ...any) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteByte('/')
		switch v := s.(type) {
		case int:
			b.WriteString(strconv.Itoa(v))
		case string:
			b.WriteString(PointerSegment(v))
		default:
			panic("ir.Pointer: unsupported segment type")
		}
	}
	return b.String()
}

var nodePointer = regexp.MustCompile(`^/nodes/([^/]+)`)

// NodeIDFromPointer возвращает ID узла для указателя вида /nodes/<id>/…, если он есть.
func NodeIDFromPointer(ptr string) (string, bool) {
	m := nodePointer.FindStringSubmatch(ptr)
	if m == nil {
		return "", false
	}
	return strings.ReplaceAll(strings.ReplaceAll(m[1], "~1", "/"), "~0", "~"), true
}
