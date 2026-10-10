package manifest

import (
	"strconv"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/schemadiag"
)

// Коды диагностики manifest — как DiagnosticCode в @cms/manifest.
const (
	CodeSchemaViolation      = "MANIFEST_SCHEMA_VIOLATION"
	CodeIrVersionUnsupported = "MANIFEST_IR_VERSION_UNSUPPORTED"
	CodeNameReserved         = "MANIFEST_NAME_RESERVED"
	CodeNameDuplicate        = "MANIFEST_NAME_DUPLICATE"
	CodeUnknownType          = "MANIFEST_UNKNOWN_TYPE"
	CodeUnknownSchema        = "MANIFEST_UNKNOWN_SCHEMA"
	CodeUnknownCapability    = "MANIFEST_UNKNOWN_CAPABILITY"
	CodeUnknownBreakpoint    = "MANIFEST_UNKNOWN_BREAKPOINT"
	CodeUnknownField         = "MANIFEST_UNKNOWN_FIELD"
	CodeDefaultInvalid       = "MANIFEST_DEFAULT_INVALID"
	CodeRangeInvalid         = "MANIFEST_RANGE_INVALID"
	CodeModifierInvalid      = "MANIFEST_MODIFIER_INVALID"
	CodeFieldReserved        = "MANIFEST_FIELD_RESERVED"
	CodeMigrationInvalid     = "MANIFEST_MIGRATION_INVALID"
)

// Codes — все коды диагностики.
var Codes = []string{
	CodeSchemaViolation, CodeIrVersionUnsupported, CodeNameReserved, CodeNameDuplicate, CodeUnknownType,
	CodeUnknownSchema, CodeUnknownCapability, CodeUnknownBreakpoint, CodeUnknownField, CodeDefaultInvalid,
	CodeRangeInvalid, CodeModifierInvalid, CodeFieldReserved, CodeMigrationInvalid,
}

// Diagnostic — диагностика manifest.
type Diagnostic struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"` // error | warning
	Pointer  string         `json:"pointer"`  // JSON Pointer (RFC 6901)
	Message  string         `json:"message"`
	Params   map[string]any `json:"params,omitempty"`
}

// Pointer собирает JSON Pointer из сегментов; целые числа — индексы массивов.
func Pointer(segments ...any) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteByte('/')
		switch v := s.(type) {
		case int:
			b.WriteString(strconv.Itoa(v))
		case string:
			b.WriteString(schemadiag.PointerSegment(v))
		}
	}
	return b.String()
}

// Встроенные имена — как в builtins.ts @cms/manifest.
var (
	// BuiltinPrimitives — встроенные примитивы runtime (03 §2.1).
	BuiltinPrimitives = []string{
		"Box", "Stack", "Flex", "Grid", "Container", "Divider", "Modal",
		"Heading", "Text", "RichText", "Label", "Image", "Video", "Icon", "Button", "Link",
		"Repeat", "Slot", "Composed",
	}
	// BuiltinActions — встроенные действия runtime (02 §7).
	BuiltinActions = []string{"navigate", "openModal", "closeModal", "scrollTo", "track", "data.loadMore"}
	// ReservedFields — имена полей сущности, зарезервированные системой (CNT-001).
	ReservedFields = []string{"id", "schema", "version", "createdAt", "updatedAt"}
	// SupportedIRVersions — поддерживаемые версии IR.
	SupportedIRVersions = []string{"1.0"}
	contentTypes        = []string{"text", "richText", "asset", "link"}
	uniqueTypes         = []string{"string", "text", "number"}
)
