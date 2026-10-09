package ir

// Ограничения документа IR (NFR-011…013, 02-ir.md §6.4). Совпадают с IR_LIMITS в @cms/ir.
const (
	MaxNodes          = 5000
	MaxDepth          = 32
	MaxBodyBytes      = 2 * 1024 * 1024
	MaxPredicateDepth = 8
)

// SupportedVersions — поддерживаемые версии формата IR.
var SupportedVersions = []string{"1.0"}
