/** Встроенные примитивы runtime (03-design-system.md §2.1): их имена зарезервированы. */
export const BUILTIN_PRIMITIVES = [
  "Box",
  "Stack",
  "Flex",
  "Grid",
  "Container",
  "Divider",
  "Modal",
  "Heading",
  "Text",
  "RichText",
  "Label",
  "Image",
  "Video",
  "Icon",
  "Button",
  "Link",
  "Repeat",
  "Slot",
  "Composed",
] as const;

/** Встроенные действия runtime (02-ir.md §7). */
export const BUILTIN_ACTIONS = [
  "navigate",
  "openModal",
  "closeModal",
  "scrollTo",
  "track",
  "data.loadMore",
] as const;

/** Имена полей сущности, зарезервированные системой (CNT-001). */
export const RESERVED_FIELDS = ["id", "schema", "version", "createdAt", "updatedAt"] as const;

/** Поддерживаемые версии IR. */
export const SUPPORTED_IR_VERSIONS = ["1.0"] as const;

/** Типы, к которым применим модификатор content (04 §4). */
export const CONTENT_TYPES = ["text", "richText", "asset", "link"] as const;

/** Типы, к которым применим модификатор unique (CNT-003). */
export const UNIQUE_TYPES = ["string", "text", "number"] as const;
