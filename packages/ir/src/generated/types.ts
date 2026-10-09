// Сгенерировано scripts/generate-types.ts из schema/ir-1.0.schema.json. Не редактировать.

/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "nodeId".
 */
export type NodeId = string;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "uuid".
 */
export type Uuid = string;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "binding".
 */
export type Binding =
  | Path
  | {
      expr: Path;
      format?: Format;
      default?: unknown;
    }
  | {
      template: string;
      vars: {
        [k: string]:
          | Path
          | {
              expr: Path;
              format?: Format;
              default?: unknown;
            };
      };
    };
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "path".
 */
export type Path = string;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "scalarDesignValue".
 */
export type ScalarDesignValue = string | number | boolean | (string | RawValue)[] | RawValue;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "designValue".
 */
export type DesignValue = (RawValue | ResponsiveValue) | ScalarDesignValue;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "predicate".
 */
export type Predicate =
  | {
      op?: "exists" | "empty";
      arg: Path;
    }
  | {
      op?: "eq" | "neq" | "gt" | "gte" | "lt" | "lte";
      /**
       * @minItems 2
       * @maxItems 2
       */
      args: [unknown, unknown, ...unknown[]];
    }
  | {
      op?: "in";
      /**
       * @minItems 2
       * @maxItems 2
       */
      args: [unknown, unknown, ...unknown[]];
    }
  | {
      op?: "and" | "or";
      /**
       * @minItems 1
       * @maxItems 16
       */
      args: [Predicate, ...Predicate[]];
    }
  | {
      op?: "not";
      arg: Predicate;
    };
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "actionArg".
 */
export type ActionArg = Link | Operand;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "link".
 */
export type Link =
  | {
      kind?: "page";
      page: Uuid;
      params?: {
        [k: string]: Operand;
      };
    }
  | {
      kind?: "url";
      url: string;
    }
  | {
      kind?: "anchor";
      node: NodeId;
    };
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "operand".
 */
export type Operand = Literal | Path;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "identifier".
 */
export type Identifier = string;
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "mode".
 */
export type Mode = "STRICT" | "SYSTEM" | "FREE" | "CODE";

/**
 * Структурная схема (уровень валидации L1). Проверки по manifest, привязкам, дизайну и политикам выполняются отдельными валидаторами (см. 02-ir.md §11).
 */
export interface IrDocument {
  irVersion: "1.0";
  kind: "page" | "component";
  root: NodeId;
  nodes: {
    [k: string]: Node;
  };
  policy?: Policy;
  content?: ContentDeclaration;
  inputs?: {
    [k: string]: TypeDef;
  };
  slotDefs?: {
    [k: string]: SlotDef;
  };
  dataSources?: {
    [k: string]: DataSourceUse;
  };
  localContent?: {
    [k: string]: LocalContentItem;
  };
  meta?: {
    title?: string;
    description?: string;
    thumbnailAssetId?: Uuid;
  };
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "node".
 */
export interface Node {
  id: NodeId;
  type: string;
  ref?: {
    component: Uuid;
    version?: "live" | number;
  };
  name?: string;
  props?: {};
  bindings?: {
    [k: string]: Binding;
  };
  design?: Design;
  when?: Predicate;
  on?: {
    [k: string]: ActionCall | [ActionCall, ...ActionCall[]];
  };
  children?: NodeId[];
  slots?: {
    [k: string]: NodeId[];
  };
  zone?: null | Zone;
  locked?: boolean;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "format".
 */
export interface Format {
  fn: string;
  args?: {};
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "design".
 */
export interface Design {
  states?: DesignStates;
  [k: string]: DesignValue | DesignStates | undefined;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "designStates".
 */
export interface DesignStates {
  [k: string]: {
    [k: string]: ScalarDesignValue;
  };
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "rawValue".
 */
export interface RawValue {
  raw: string | number;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "responsiveValue".
 */
export interface ResponsiveValue {
  base: ScalarDesignValue;
  [k: string]: ScalarDesignValue;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "actionCall".
 */
export interface ActionCall {
  action: string;
  args?: {
    [k: string]: ActionArg;
  };
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "literal".
 */
export interface Literal {
  lit: unknown;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "zone".
 */
export interface Zone {
  id: Identifier;
  mode: Mode;
  maxMode?: Mode;
  allowedTypes?: string[];
  deniedTypes?: string[];
  locked?: boolean;
  requiredApprovalRole?: string;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "policy".
 */
export interface Policy {
  mode?: Mode;
  maxMode?: Mode;
  allowedTypes?: string[];
  deniedTypes?: string[];
  allowRawColors?: boolean;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "contentDeclaration".
 */
export interface ContentDeclaration {
  schema: string;
  resolve:
    | {
        by: "routeParam";
        field: Identifier;
        param: Identifier;
      }
    | {
        by: "entity";
        entityId: Uuid;
      };
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "typeDef".
 */
export interface TypeDef {
  type:
    | "string"
    | "text"
    | "richText"
    | "number"
    | "boolean"
    | "enum"
    | "date"
    | "datetime"
    | "url"
    | "link"
    | "asset"
    | "reference"
    | "list"
    | "object"
    | "color"
    | "nodeRef";
  required?: boolean;
  content?: boolean;
  responsive?: boolean;
  localized?: boolean;
  default?: unknown;
  description?: string;
  /**
   * @minItems 1
   */
  values?: [string, ...string[]];
  schema?: string;
  assetKind?: "image" | "video" | "file";
  of?: TypeDef;
  fields?: {
    [k: string]: TypeDef;
  };
  min?: number;
  max?: number;
  integer?: boolean;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  multiline?: boolean;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "slotDef".
 */
export interface SlotDef {
  allowedTypes?: string[];
  min?: number;
  max?: number;
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "dataSourceUse".
 */
export interface DataSourceUse {
  source: string;
  params?: {
    [k: string]: Operand;
  };
}
/**
 * This interface was referenced by `IrDocument`'s JSON-Schema
 * via the `definition` "localContentItem".
 */
export interface LocalContentItem {
  type: "text" | "richText" | "asset" | "link";
  value: {};
}
