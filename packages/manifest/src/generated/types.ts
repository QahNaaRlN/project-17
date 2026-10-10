// Сгенерировано scripts/generate-types.ts из schema/manifest-1.0.schema.json. Не редактировать.

/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Length".
 */
export type Length = string;
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Description".
 */
export type Description = string;
/**
 * Тип системы типов (04 §4): свойство компонента, аргумент действия, параметр или результат источника, поле схемы.
 *
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Type".
 */
export type Type =
  | StringType
  | TextType
  | RichTextType
  | NumberType
  | BooleanType
  | EnumType
  | DateType
  | DatetimeType
  | UrlType
  | LinkType
  | AssetType
  | ReferenceType
  | ListType
  | ObjectType
  | ColorType
  | NodeRefType;
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "StringType".
 */
export type StringType = TypeModifiers & {
  type: "string";
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  default?: string;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "TextType".
 */
export type TextType = TypeModifiers & {
  type: "text";
  maxLength?: number;
  multiline?: boolean;
  default?: string;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "RichTextType".
 */
export type RichTextType = TypeModifiers & {
  type: "richText";
  marks?: ("bold" | "italic" | "underline" | "strike" | "code" | "link")[];
  blocks?: (
    "paragraph" | "heading" | "bulletList" | "orderedList" | "blockquote" | "image" | "divider"
  )[];
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "NumberType".
 */
export type NumberType = TypeModifiers & {
  type: "number";
  min?: number;
  max?: number;
  integer?: boolean;
  default?: number;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "BooleanType".
 */
export type BooleanType = TypeModifiers & {
  type: "boolean";
  default?: boolean;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "EnumType".
 */
export type EnumType = TypeModifiers & {
  type: "enum";
  /**
   * @minItems 1
   * @maxItems 200
   */
  values: [string, ...string[]];
  default?: string;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "DateType".
 */
export type DateType = TypeModifiers & {
  type: "date";
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "DatetimeType".
 */
export type DatetimeType = TypeModifiers & {
  type: "datetime";
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "UrlType".
 */
export type UrlType = TypeModifiers & {
  type: "url";
  /**
   * @minItems 1
   */
  schemes?: [string, ...string[]];
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "LinkType".
 */
export type LinkType = TypeModifiers & {
  type: "link";
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "AssetType".
 */
export type AssetType = TypeModifiers & {
  type: "asset";
  assetKind?: "image" | "video" | "file";
  /**
   * @minItems 1
   */
  mimeTypes?: [string, ...string[]];
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "ReferenceType".
 */
export type ReferenceType = TypeModifiers & {
  type: "reference";
  schema: string;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "ListType".
 */
export type ListType = TypeModifiers & {
  type: "list";
  of: Type;
  min?: number;
  max?: number;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "ObjectType".
 */
export type ObjectType = TypeModifiers & {
  type: "object";
  fields: Fields;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "ColorType".
 */
export type ColorType = TypeModifiers & {
  type: "color";
  default?: TokenName;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "TokenName".
 */
export type TokenName = string;
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "NodeRefType".
 */
export type NodeRefType = TypeModifiers & {
  type: "nodeRef";
  nodeType?: ComponentName;
};
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "ComponentName".
 */
export type ComponentName = string;
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "DottedName".
 */
export type DottedName = string;
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "FieldName".
 */
export type FieldName = string;
/**
 * Декларативная операция миграции схемы (05 §4.2).
 *
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "MigrationOp".
 */
export type MigrationOp =
  | {
      op: "rename";
      from: FieldName;
      to: FieldName;
    }
  | {
      op: "drop";
      field: FieldName;
    }
  | {
      op: "setDefault";
      field: FieldName;
      value: unknown;
    }
  | {
      op: "localize";
      field: FieldName;
    }
  | {
      op: "delocalize";
      field: FieldName;
      locale: Locale;
    }
  | {
      op: "wrapList";
      field: FieldName;
    }
  | {
      op: "unwrapList";
      field: FieldName;
    }
  | {
      op: "mapEnum";
      field: FieldName;
      map: {
        [k: string]: string;
      };
    }
  | {
      op: "convert";
      field: FieldName;
      to: "toString" | "toNumber" | "textToRichText" | "richTextToText";
    };
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Locale".
 */
export type Locale = string;
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Sha256".
 */
export type Sha256 = string;

/**
 * Описание приложения для CMS: токены, компоненты, действия, источники данных, форматтеры, схемы контента и capabilities (docs/spec/04-manifest.md). Единственный источник истины формата; типы TS/Go генерируются из него.
 */
export interface Manifest {
  manifestVersion: "1.0";
  app: App;
  /**
   * @minItems 1
   */
  irVersions: [string, ...string[]];
  /**
   * Breakpoint'ы: имя → минимальная ширина в px. Имя base зарезервировано.
   */
  breakpoints: {
    [k: string]: number;
  };
  tokens: Tokens;
  /**
   * @maxItems 2000
   */
  icons?: string[];
  /**
   * Расширенные примитивы приложения (03 §2.1): описываются как компоненты.
   */
  primitives?: {
    [k: string]: Component;
  };
  components?: {
    [k: string]: Component;
  };
  actions?: {
    [k: string]: Action;
  };
  dataSources?: {
    [k: string]: DataSource;
  };
  formatters?: {
    [k: string]: Formatter;
  };
  schemas?: {
    [k: string]: ContentSchema;
  };
  capabilities?: {
    [k: string]: Capability;
  };
  codeIndex?: {
    hash: Sha256;
    uploaded: boolean;
  };
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "App".
 */
export interface App {
  id: string;
  version: string;
  build?: string;
  framework: "react" | "vue" | "svelte" | "other";
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Tokens".
 */
export interface Tokens {
  spacing?: TokenMap;
  colors?: TokenMap1;
  radius?: TokenMap2;
  typography?: TokenMap3;
  shadow?: TokenMap4;
  borderWidth?: TokenMap5;
  container?: TokenMap6;
  layer?: TokenMap7;
  transition?: TokenMap8;
}
export interface TokenMap {
  [k: string]: Length;
}
export interface TokenMap1 {
  [k: string]:
    | string
    | {
        value: string;
        modes?: {
          [k: string]: string;
        };
      };
}
export interface TokenMap2 {
  [k: string]: Length;
}
export interface TokenMap3 {
  [k: string]: Typography;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Typography".
 */
export interface Typography {
  fontFamily: string;
  /**
   * Адаптивный размер: ключ base обязателен, прочие — breakpoint'ы manifest.
   */
  fontSize: {
    [k: string]: Length;
  };
  lineHeight?: string;
  fontWeight?: number;
  letterSpacing?: string;
  large?: boolean;
}
export interface TokenMap4 {
  [k: string]: string;
}
export interface TokenMap5 {
  [k: string]: Length;
}
export interface TokenMap6 {
  [k: string]: Length;
}
export interface TokenMap7 {
  [k: string]: number;
}
export interface TokenMap8 {
  [k: string]: string;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Component".
 */
export interface Component {
  origin?: "native" | "generated";
  description?: Description;
  category?: string;
  /**
   * Группа дизайн-свойств для расширенного примитива (03 §2.1).
   */
  group?: "Layout" | "Typography" | "Media" | "Actions";
  container?: boolean;
  props?: Fields;
  slots?: {
    [k: string]: Slot;
  };
  events?: {
    [k: string]: {
      payload?: Fields;
      description?: Description;
    };
  };
  provides?: Fields;
  capabilities?: DottedName[];
  since?: string;
  deprecated?: null | Deprecated;
  sourceRef?: SourceRef;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Fields".
 */
export interface Fields {
  [k: string]: Type;
}
/**
 * Общие модификаторы (04 §4).
 *
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "TypeModifiers".
 */
export interface TypeModifiers {
  required?: boolean;
  responsive?: boolean;
  localized?: boolean;
  unique?: boolean;
  content?: boolean;
  description?: Description;
  deprecated?: Deprecated;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Deprecated".
 */
export interface Deprecated {
  since: string;
  replacement?: string;
  message?: Description;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Slot".
 */
export interface Slot {
  allowedTypes?: ComponentName[];
  min?: number;
  max?: number;
  description?: Description;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "SourceRef".
 */
export interface SourceRef {
  file: string;
  export?: string;
  line?: number;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Action".
 */
export interface Action {
  description?: Description;
  args?: Fields;
  capabilities?: DottedName[];
  deprecated?: null | Deprecated;
  sourceRef?: SourceRef;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "DataSource".
 */
export interface DataSource {
  description?: Description;
  params?: Fields;
  result: Type;
  paginated?: boolean;
  cache?: {
    ttlSeconds: number;
  };
  capabilities?: DottedName[];
  deprecated?: null | Deprecated;
  sourceRef?: SourceRef;
}
/**
 * Форматтер для привязок (02 §4: format.fn). input — допустимые типы входного значения.
 *
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Formatter".
 */
export interface Formatter {
  description?: Description;
  /**
   * @minItems 1
   */
  input: [
    "string" | "text" | "number" | "boolean" | "date" | "datetime" | "url" | "enum",
    ...("string" | "text" | "number" | "boolean" | "date" | "datetime" | "url" | "enum")[]
  ];
  args?: Fields;
  sourceRef?: SourceRef;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "ContentSchema".
 */
export interface ContentSchema {
  version: number;
  title?: string;
  description?: Description;
  singleton?: boolean;
  display?: {
    titleField?: FieldName;
    previewField?: FieldName;
  };
  fields: Fields1;
  /**
   * Миграции с прежних версий схемы (05 §4.2): версия → операции.
   */
  migrateFrom?: {
    /**
     * @minItems 1
     */
    [k: string]: [MigrationOp, ...MigrationOp[]];
  };
}
export interface Fields1 {
  [k: string]: Type;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "Capability".
 */
export interface Capability {
  description?: Description;
  domain?: string;
}
/**
 * This interface was referenced by `Manifest`'s JSON-Schema
 * via the `definition` "TokenMap".
 */
export interface TokenMap9 {}
