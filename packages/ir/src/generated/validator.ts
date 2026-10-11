// Сгенерировано scripts/generate-types.ts из schema/ir-1.0.schema.json. Не редактировать.
// @ts-nocheck
"use strict";
export const validate = validate20;
export default validate20;
const schema31 = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  $id: "https://cms.dev/schemas/ir/1.0/document.json",
  title: "CMS IR document 1.0",
  description:
    "Структурная схема (уровень валидации L1). Проверки по manifest, привязкам, дизайну и политикам выполняются отдельными валидаторами (см. 02-ir.md §11).",
  type: "object",
  required: ["irVersion", "kind", "root", "nodes"],
  properties: {
    irVersion: { const: "1.0" },
    kind: { enum: ["page", "component"] },
    root: { $ref: "#/$defs/nodeId" },
    nodes: {
      type: "object",
      minProperties: 1,
      maxProperties: 5000,
      propertyNames: { $ref: "#/$defs/nodeId" },
      additionalProperties: { $ref: "#/$defs/node" },
    },
    policy: { $ref: "#/$defs/policy" },
    content: { $ref: "#/$defs/contentDeclaration" },
    inputs: {
      type: "object",
      propertyNames: { $ref: "#/$defs/identifier" },
      additionalProperties: { $ref: "#/$defs/typeDef" },
    },
    slotDefs: {
      type: "object",
      propertyNames: { $ref: "#/$defs/identifier" },
      additionalProperties: { $ref: "#/$defs/slotDef" },
    },
    dataSources: {
      type: "object",
      propertyNames: { $ref: "#/$defs/identifier" },
      additionalProperties: { $ref: "#/$defs/dataSourceUse" },
    },
    localContent: {
      type: "object",
      propertyNames: { pattern: "^t_[a-z0-9_]{2,40}$" },
      additionalProperties: { $ref: "#/$defs/localContentItem" },
    },
    meta: {
      type: "object",
      properties: {
        title: { type: "string", maxLength: 200 },
        description: { type: "string", maxLength: 2000 },
        thumbnailAssetId: { $ref: "#/$defs/uuid" },
      },
    },
  },
  allOf: [
    {
      if: { properties: { kind: { const: "page" } } },
      then: { properties: { inputs: false, slotDefs: false } },
    },
    {
      if: { properties: { kind: { const: "component" } } },
      then: { properties: { content: false } },
    },
  ],
  $defs: {
    uuid: {
      type: "string",
      pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
    },
    nodeId: { type: "string", pattern: "^[A-Za-z0-9_-]{4,32}$" },
    identifier: { type: "string", pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
    mode: { enum: ["STRICT", "SYSTEM", "FREE", "CODE"] },
    node: {
      type: "object",
      required: ["id", "type"],
      properties: {
        id: { $ref: "#/$defs/nodeId" },
        type: { type: "string", pattern: "^[A-Z][A-Za-z0-9]*$" },
        ref: {
          type: "object",
          required: ["component"],
          properties: {
            component: { $ref: "#/$defs/uuid" },
            version: { oneOf: [{ const: "live" }, { type: "integer", minimum: 1 }] },
          },
          additionalProperties: false,
        },
        name: { type: "string", maxLength: 120 },
        props: { type: "object" },
        bindings: { type: "object", additionalProperties: { $ref: "#/$defs/binding" } },
        design: { $ref: "#/$defs/design" },
        when: { $ref: "#/$defs/predicate" },
        on: {
          type: "object",
          propertyNames: { $ref: "#/$defs/identifier" },
          additionalProperties: {
            oneOf: [
              { $ref: "#/$defs/actionCall" },
              { type: "array", minItems: 1, maxItems: 8, items: { $ref: "#/$defs/actionCall" } },
            ],
          },
        },
        children: { type: "array", items: { $ref: "#/$defs/nodeId" }, uniqueItems: true },
        slots: {
          type: "object",
          propertyNames: { $ref: "#/$defs/identifier" },
          additionalProperties: {
            type: "array",
            items: { $ref: "#/$defs/nodeId" },
            uniqueItems: true,
          },
        },
        zone: { oneOf: [{ type: "null" }, { $ref: "#/$defs/zone" }] },
        locked: { type: "boolean" },
      },
      allOf: [
        {
          if: { properties: { type: { const: "Composed" } }, required: ["type"] },
          then: { required: ["ref"] },
          else: { properties: { ref: false } },
        },
      ],
    },
    path: {
      type: "string",
      pattern:
        "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
    },
    literal: {
      type: "object",
      required: ["lit"],
      properties: { lit: {} },
      additionalProperties: false,
    },
    operand: {
      if: { type: "object" },
      then: { $ref: "#/$defs/literal" },
      else: { $ref: "#/$defs/path" },
    },
    format: {
      type: "object",
      required: ["fn"],
      properties: {
        fn: { type: "string", pattern: "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*$" },
        args: { type: "object" },
      },
      additionalProperties: false,
    },
    binding: {
      if: { type: "string" },
      then: { $ref: "#/$defs/path" },
      else: {
        if: { type: "object", required: ["template"] },
        then: {
          type: "object",
          required: ["template", "vars"],
          properties: {
            template: { type: "string", maxLength: 2000 },
            vars: {
              type: "object",
              propertyNames: { $ref: "#/$defs/identifier" },
              additionalProperties: {
                oneOf: [
                  { $ref: "#/$defs/path" },
                  {
                    type: "object",
                    required: ["expr"],
                    properties: {
                      expr: { $ref: "#/$defs/path" },
                      format: { $ref: "#/$defs/format" },
                      default: {},
                    },
                    additionalProperties: false,
                  },
                ],
              },
            },
          },
          additionalProperties: false,
        },
        else: {
          type: "object",
          required: ["expr"],
          properties: {
            expr: { $ref: "#/$defs/path" },
            format: { $ref: "#/$defs/format" },
            default: {},
          },
          additionalProperties: false,
        },
      },
    },
    predicate: {
      type: "object",
      required: ["op"],
      oneOf: [
        {
          properties: { op: { enum: ["exists", "empty"] }, arg: { $ref: "#/$defs/path" } },
          required: ["arg"],
          additionalProperties: false,
        },
        {
          properties: {
            op: { enum: ["eq", "neq", "gt", "gte", "lt", "lte"] },
            args: {
              type: "array",
              prefixItems: [{ $ref: "#/$defs/operand" }, { $ref: "#/$defs/operand" }],
              minItems: 2,
              maxItems: 2,
            },
          },
          required: ["args"],
          additionalProperties: false,
        },
        {
          properties: {
            op: { const: "in" },
            args: {
              type: "array",
              prefixItems: [
                { $ref: "#/$defs/operand" },
                {
                  type: "object",
                  required: ["lit"],
                  properties: { lit: { type: "array" } },
                  additionalProperties: false,
                },
              ],
              minItems: 2,
              maxItems: 2,
            },
          },
          required: ["args"],
          additionalProperties: false,
        },
        {
          properties: {
            op: { enum: ["and", "or"] },
            args: {
              type: "array",
              minItems: 1,
              maxItems: 16,
              items: { $ref: "#/$defs/predicate" },
            },
          },
          required: ["args"],
          additionalProperties: false,
        },
        {
          properties: { op: { const: "not" }, arg: { $ref: "#/$defs/predicate" } },
          required: ["arg"],
          additionalProperties: false,
        },
      ],
      discriminator: { propertyName: "op" },
    },
    actionCall: {
      type: "object",
      required: ["action"],
      properties: {
        action: {
          type: "string",
          pattern: "^([a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*|pending:[0-9a-f-]{36})$",
        },
        args: { type: "object", additionalProperties: { $ref: "#/$defs/actionArg" } },
      },
      additionalProperties: false,
    },
    link: {
      type: "object",
      required: ["kind"],
      oneOf: [
        {
          properties: {
            kind: { const: "page" },
            page: { $ref: "#/$defs/uuid" },
            params: { type: "object", additionalProperties: { $ref: "#/$defs/operand" } },
          },
          required: ["page"],
          additionalProperties: false,
        },
        {
          properties: {
            kind: { const: "url" },
            url: { type: "string", pattern: "^(https?:|mailto:|tel:|/)" },
          },
          required: ["url"],
          additionalProperties: false,
        },
        {
          properties: { kind: { const: "anchor" }, node: { $ref: "#/$defs/nodeId" } },
          required: ["node"],
          additionalProperties: false,
        },
      ],
      discriminator: { propertyName: "kind" },
    },
    rawValue: {
      type: "object",
      required: ["raw"],
      properties: { raw: { type: ["string", "number"] } },
      additionalProperties: false,
    },
    scalarDesignValue: {
      if: { type: "object" },
      then: { $ref: "#/$defs/rawValue" },
      else: {
        oneOf: [
          { type: "string", pattern: "^[A-Za-z0-9][A-Za-z0-9:/._-]*$" },
          { type: "number" },
          { type: "boolean" },
          {
            type: "array",
            items: { oneOf: [{ type: "string" }, { $ref: "#/$defs/rawValue" }] },
            maxItems: 12,
          },
        ],
      },
    },
    responsiveValue: {
      type: "object",
      required: ["base"],
      properties: { base: { $ref: "#/$defs/scalarDesignValue" } },
      propertyNames: { not: { enum: ["raw", "token", "expr"] } },
      additionalProperties: { $ref: "#/$defs/scalarDesignValue" },
    },
    designValue: {
      if: { type: "object" },
      then: {
        if: { required: ["raw"], maxProperties: 1 },
        then: { $ref: "#/$defs/rawValue" },
        else: { $ref: "#/$defs/responsiveValue" },
      },
      else: { $ref: "#/$defs/scalarDesignValue" },
    },
    design: {
      type: "object",
      properties: { states: { $ref: "#/$defs/designStates" } },
      propertyNames: { pattern: "^[a-z][A-Za-z]*$" },
      additionalProperties: { $ref: "#/$defs/designValue" },
    },
    designStates: {
      type: "object",
      propertyNames: { enum: ["hover", "active", "focusVisible", "disabled"] },
      additionalProperties: {
        type: "object",
        propertyNames: {
          enum: ["background", "color", "borderColor", "shadow", "opacity", "outline"],
        },
        additionalProperties: { $ref: "#/$defs/scalarDesignValue" },
      },
    },
    zone: {
      type: "object",
      required: ["id", "mode"],
      properties: {
        id: { $ref: "#/$defs/identifier" },
        mode: { $ref: "#/$defs/mode" },
        maxMode: { $ref: "#/$defs/mode" },
        allowedTypes: { type: "array", items: { type: "string" } },
        deniedTypes: { type: "array", items: { type: "string" } },
        locked: { type: "boolean" },
        requiredApprovalRole: { type: "string" },
      },
      additionalProperties: false,
    },
    policy: {
      type: "object",
      properties: {
        mode: { $ref: "#/$defs/mode" },
        maxMode: { $ref: "#/$defs/mode" },
        allowedTypes: { type: "array", items: { type: "string" } },
        deniedTypes: { type: "array", items: { type: "string" } },
        allowRawColors: { type: "boolean" },
      },
      additionalProperties: false,
    },
    contentDeclaration: {
      type: "object",
      required: ["schema", "resolve"],
      properties: {
        schema: { type: "string", pattern: "^[A-Z][A-Za-z0-9]{1,63}$" },
        resolve: {
          oneOf: [
            {
              type: "object",
              required: ["by", "field", "param"],
              properties: {
                by: { const: "routeParam" },
                field: { $ref: "#/$defs/identifier" },
                param: { $ref: "#/$defs/identifier" },
              },
              additionalProperties: false,
            },
            {
              type: "object",
              required: ["by", "entityId"],
              properties: { by: { const: "entity" }, entityId: { $ref: "#/$defs/uuid" } },
              additionalProperties: false,
            },
          ],
        },
      },
      additionalProperties: false,
    },
    typeDef: {
      type: "object",
      required: ["type"],
      properties: {
        type: {
          enum: [
            "string",
            "text",
            "richText",
            "number",
            "boolean",
            "enum",
            "date",
            "datetime",
            "url",
            "link",
            "asset",
            "reference",
            "list",
            "object",
            "color",
            "nodeRef",
          ],
        },
        required: { type: "boolean" },
        content: { type: "boolean" },
        responsive: { type: "boolean" },
        localized: { type: "boolean" },
        default: {},
        description: { type: "string" },
        values: { type: "array", items: { type: "string" }, minItems: 1 },
        schema: { type: "string" },
        assetKind: { enum: ["image", "video", "file"] },
        of: { $ref: "#/$defs/typeDef" },
        fields: { type: "object", additionalProperties: { $ref: "#/$defs/typeDef" } },
        min: { type: "number" },
        max: { type: "number" },
        integer: { type: "boolean" },
        minLength: { type: "integer" },
        maxLength: { type: "integer" },
        pattern: { type: "string" },
        multiline: { type: "boolean" },
      },
      allOf: [
        { if: { properties: { type: { const: "enum" } } }, then: { required: ["values"] } },
        { if: { properties: { type: { const: "reference" } } }, then: { required: ["schema"] } },
        { if: { properties: { type: { const: "list" } } }, then: { required: ["of"] } },
      ],
    },
    slotDef: {
      type: "object",
      properties: {
        allowedTypes: { type: "array", items: { type: "string" } },
        min: { type: "integer", minimum: 0 },
        max: { type: "integer", minimum: 1 },
      },
      additionalProperties: false,
    },
    dataSourceUse: {
      type: "object",
      required: ["source"],
      properties: {
        source: { type: "string", pattern: "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)+$" },
        params: { type: "object", additionalProperties: { $ref: "#/$defs/operand" } },
      },
      additionalProperties: false,
    },
    localContentItem: {
      type: "object",
      required: ["type", "value"],
      properties: {
        type: { enum: ["text", "richText", "asset", "link"] },
        value: {
          type: "object",
          minProperties: 1,
          propertyNames: { pattern: "^[a-z]{2,3}(-[A-Z]{2})?$" },
        },
      },
      additionalProperties: false,
    },
    actionArg: {
      if: { type: "object", required: ["kind"] },
      then: { $ref: "#/$defs/link" },
      else: { $ref: "#/$defs/operand" },
    },
  },
};
const schema32 = { type: "string", pattern: "^[A-Za-z0-9_-]{4,32}$" };
const schema39 = { type: "string", pattern: "^[A-Za-z][A-Za-z0-9_]*$" };
const schema82 = {
  type: "object",
  properties: {
    allowedTypes: { type: "array", items: { type: "string" } },
    min: { type: "integer", minimum: 0 },
    max: { type: "integer", minimum: 1 },
  },
  additionalProperties: false,
};
const schema85 = {
  type: "object",
  required: ["type", "value"],
  properties: {
    type: { enum: ["text", "richText", "asset", "link"] },
    value: {
      type: "object",
      minProperties: 1,
      propertyNames: { pattern: "^[a-z]{2,3}(-[A-Z]{2})?$" },
    },
  },
  additionalProperties: false,
};
const schema36 = {
  type: "string",
  pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
};
const pattern4 = new RegExp("^[A-Za-z0-9_-]{4,32}$", "u");
const pattern10 = new RegExp("^[A-Za-z][A-Za-z0-9_]*$", "u");
const pattern38 = new RegExp("^t_[a-z0-9_]{2,40}$", "u");
const pattern39 = new RegExp("^[a-z]{2,3}(-[A-Z]{2})?$", "u");
const pattern8 = new RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", "u");
const schema34 = {
  type: "object",
  required: ["id", "type"],
  properties: {
    id: { $ref: "#/$defs/nodeId" },
    type: { type: "string", pattern: "^[A-Z][A-Za-z0-9]*$" },
    ref: {
      type: "object",
      required: ["component"],
      properties: {
        component: { $ref: "#/$defs/uuid" },
        version: { oneOf: [{ const: "live" }, { type: "integer", minimum: 1 }] },
      },
      additionalProperties: false,
    },
    name: { type: "string", maxLength: 120 },
    props: { type: "object" },
    bindings: { type: "object", additionalProperties: { $ref: "#/$defs/binding" } },
    design: { $ref: "#/$defs/design" },
    when: { $ref: "#/$defs/predicate" },
    on: {
      type: "object",
      propertyNames: { $ref: "#/$defs/identifier" },
      additionalProperties: {
        oneOf: [
          { $ref: "#/$defs/actionCall" },
          { type: "array", minItems: 1, maxItems: 8, items: { $ref: "#/$defs/actionCall" } },
        ],
      },
    },
    children: { type: "array", items: { $ref: "#/$defs/nodeId" }, uniqueItems: true },
    slots: {
      type: "object",
      propertyNames: { $ref: "#/$defs/identifier" },
      additionalProperties: { type: "array", items: { $ref: "#/$defs/nodeId" }, uniqueItems: true },
    },
    zone: { oneOf: [{ type: "null" }, { $ref: "#/$defs/zone" }] },
    locked: { type: "boolean" },
  },
  allOf: [
    {
      if: { properties: { type: { const: "Composed" } }, required: ["type"] },
      then: { required: ["ref"] },
      else: { properties: { ref: false } },
    },
  ],
};
const pattern7 = new RegExp("^[A-Z][A-Za-z0-9]*$", "u");
import func1Module from "ajv/dist/runtime/ucs2length.js";
const func1 = func1Module.default ?? func1Module;
import func0Module from "ajv/dist/runtime/equal.js";
const func0 = func0Module.default ?? func0Module;
const schema37 = {
  if: { type: "string" },
  then: { $ref: "#/$defs/path" },
  else: {
    if: { type: "object", required: ["template"] },
    then: {
      type: "object",
      required: ["template", "vars"],
      properties: {
        template: { type: "string", maxLength: 2000 },
        vars: {
          type: "object",
          propertyNames: { $ref: "#/$defs/identifier" },
          additionalProperties: {
            oneOf: [
              { $ref: "#/$defs/path" },
              {
                type: "object",
                required: ["expr"],
                properties: {
                  expr: { $ref: "#/$defs/path" },
                  format: { $ref: "#/$defs/format" },
                  default: {},
                },
                additionalProperties: false,
              },
            ],
          },
        },
      },
      additionalProperties: false,
    },
    else: {
      type: "object",
      required: ["expr"],
      properties: {
        expr: { $ref: "#/$defs/path" },
        format: { $ref: "#/$defs/format" },
        default: {},
      },
      additionalProperties: false,
    },
  },
};
const schema38 = {
  type: "string",
  pattern:
    "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
};
const schema42 = {
  type: "object",
  required: ["fn"],
  properties: {
    fn: { type: "string", pattern: "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*$" },
    args: { type: "object" },
  },
  additionalProperties: false,
};
const pattern9 = new RegExp(
  "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
  "u",
);
const pattern13 = new RegExp("^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*$", "u");

function validate22(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate22.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs0 = errors;
  let valid0 = true;
  const _errs1 = errors;
  if (typeof data !== "string") {
    const err0 = {};
    if (vErrors === null) {
      vErrors = [err0];
    } else {
      vErrors.push(err0);
    }
    errors++;
  }
  var _valid0 = _errs1 === errors;
  errors = _errs0;
  if (vErrors !== null) {
    if (_errs0) {
      vErrors.length = _errs0;
    } else {
      vErrors = null;
    }
  }
  let ifClause0;
  if (_valid0) {
    const _errs3 = errors;
    if (typeof data === "string") {
      if (!pattern9.test(data)) {
        const err1 = {
          instancePath,
          schemaPath: "#/$defs/path/pattern",
          keyword: "pattern",
          params: {
            pattern:
              "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
          },
          message:
            'must match pattern "' +
            "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$" +
            '"',
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    } else {
      const err2 = {
        instancePath,
        schemaPath: "#/$defs/path/type",
        keyword: "type",
        params: { type: "string" },
        message: "must be string",
      };
      if (vErrors === null) {
        vErrors = [err2];
      } else {
        vErrors.push(err2);
      }
      errors++;
    }
    var _valid0 = _errs3 === errors;
    valid0 = _valid0;
    ifClause0 = "then";
  } else {
    const _errs6 = errors;
    const _errs7 = errors;
    let valid2 = true;
    const _errs8 = errors;
    if (errors === _errs8) {
      if (data && typeof data == "object" && !Array.isArray(data)) {
        let missing0;
        if (data.template === undefined && (missing0 = "template")) {
          const err3 = {};
          if (vErrors === null) {
            vErrors = [err3];
          } else {
            vErrors.push(err3);
          }
          errors++;
        }
      } else {
        const err4 = {};
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
    var _valid1 = _errs8 === errors;
    errors = _errs7;
    if (vErrors !== null) {
      if (_errs7) {
        vErrors.length = _errs7;
      } else {
        vErrors = null;
      }
    }
    let ifClause1;
    if (_valid1) {
      const _errs10 = errors;
      if (data && typeof data == "object" && !Array.isArray(data)) {
        if (data.template === undefined) {
          const err5 = {
            instancePath,
            schemaPath: "#/else/then/required",
            keyword: "required",
            params: { missingProperty: "template" },
            message: "must have required property '" + "template" + "'",
          };
          if (vErrors === null) {
            vErrors = [err5];
          } else {
            vErrors.push(err5);
          }
          errors++;
        }
        if (data.vars === undefined) {
          const err6 = {
            instancePath,
            schemaPath: "#/else/then/required",
            keyword: "required",
            params: { missingProperty: "vars" },
            message: "must have required property '" + "vars" + "'",
          };
          if (vErrors === null) {
            vErrors = [err6];
          } else {
            vErrors.push(err6);
          }
          errors++;
        }
        for (const key0 in data) {
          if (!(key0 === "template" || key0 === "vars")) {
            const err7 = {
              instancePath,
              schemaPath: "#/else/then/additionalProperties",
              keyword: "additionalProperties",
              params: { additionalProperty: key0 },
              message: "must NOT have additional properties",
            };
            if (vErrors === null) {
              vErrors = [err7];
            } else {
              vErrors.push(err7);
            }
            errors++;
          }
        }
        if (data.template !== undefined) {
          let data0 = data.template;
          if (typeof data0 === "string") {
            if (func1(data0) > 2000) {
              const err8 = {
                instancePath: instancePath + "/template",
                schemaPath: "#/else/then/properties/template/maxLength",
                keyword: "maxLength",
                params: { limit: 2000 },
                message: "must NOT have more than 2000 characters",
              };
              if (vErrors === null) {
                vErrors = [err8];
              } else {
                vErrors.push(err8);
              }
              errors++;
            }
          } else {
            const err9 = {
              instancePath: instancePath + "/template",
              schemaPath: "#/else/then/properties/template/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err9];
            } else {
              vErrors.push(err9);
            }
            errors++;
          }
        }
        if (data.vars !== undefined) {
          let data1 = data.vars;
          if (data1 && typeof data1 == "object" && !Array.isArray(data1)) {
            for (const key1 in data1) {
              const _errs17 = errors;
              if (typeof key1 === "string") {
                if (!pattern10.test(key1)) {
                  const err10 = {
                    instancePath: instancePath + "/vars",
                    schemaPath: "#/$defs/identifier/pattern",
                    keyword: "pattern",
                    params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                    message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
                    propertyName: key1,
                  };
                  if (vErrors === null) {
                    vErrors = [err10];
                  } else {
                    vErrors.push(err10);
                  }
                  errors++;
                }
              } else {
                const err11 = {
                  instancePath: instancePath + "/vars",
                  schemaPath: "#/$defs/identifier/type",
                  keyword: "type",
                  params: { type: "string" },
                  message: "must be string",
                  propertyName: key1,
                };
                if (vErrors === null) {
                  vErrors = [err11];
                } else {
                  vErrors.push(err11);
                }
                errors++;
              }
              var valid4 = _errs17 === errors;
              if (!valid4) {
                const err12 = {
                  instancePath: instancePath + "/vars",
                  schemaPath: "#/else/then/properties/vars/propertyNames",
                  keyword: "propertyNames",
                  params: { propertyName: key1 },
                  message: "property name must be valid",
                };
                if (vErrors === null) {
                  vErrors = [err12];
                } else {
                  vErrors.push(err12);
                }
                errors++;
              }
            }
            for (const key2 in data1) {
              let data2 = data1[key2];
              const _errs22 = errors;
              let valid7 = false;
              let passing0 = null;
              const _errs23 = errors;
              if (typeof data2 === "string") {
                if (!pattern9.test(data2)) {
                  const err13 = {
                    instancePath:
                      instancePath + "/vars/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),
                    schemaPath: "#/$defs/path/pattern",
                    keyword: "pattern",
                    params: {
                      pattern:
                        "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
                    },
                    message:
                      'must match pattern "' +
                      "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$" +
                      '"',
                  };
                  if (vErrors === null) {
                    vErrors = [err13];
                  } else {
                    vErrors.push(err13);
                  }
                  errors++;
                }
              } else {
                const err14 = {
                  instancePath:
                    instancePath + "/vars/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),
                  schemaPath: "#/$defs/path/type",
                  keyword: "type",
                  params: { type: "string" },
                  message: "must be string",
                };
                if (vErrors === null) {
                  vErrors = [err14];
                } else {
                  vErrors.push(err14);
                }
                errors++;
              }
              var _valid2 = _errs23 === errors;
              if (_valid2) {
                valid7 = true;
                passing0 = 0;
              }
              const _errs26 = errors;
              if (data2 && typeof data2 == "object" && !Array.isArray(data2)) {
                if (data2.expr === undefined) {
                  const err15 = {
                    instancePath:
                      instancePath + "/vars/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),
                    schemaPath: "#/else/then/properties/vars/additionalProperties/oneOf/1/required",
                    keyword: "required",
                    params: { missingProperty: "expr" },
                    message: "must have required property '" + "expr" + "'",
                  };
                  if (vErrors === null) {
                    vErrors = [err15];
                  } else {
                    vErrors.push(err15);
                  }
                  errors++;
                }
                for (const key3 in data2) {
                  if (!(key3 === "expr" || key3 === "format" || key3 === "default")) {
                    const err16 = {
                      instancePath:
                        instancePath + "/vars/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),
                      schemaPath:
                        "#/else/then/properties/vars/additionalProperties/oneOf/1/additionalProperties",
                      keyword: "additionalProperties",
                      params: { additionalProperty: key3 },
                      message: "must NOT have additional properties",
                    };
                    if (vErrors === null) {
                      vErrors = [err16];
                    } else {
                      vErrors.push(err16);
                    }
                    errors++;
                  }
                }
                if (data2.expr !== undefined) {
                  let data3 = data2.expr;
                  if (typeof data3 === "string") {
                    if (!pattern9.test(data3)) {
                      const err17 = {
                        instancePath:
                          instancePath +
                          "/vars/" +
                          key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                          "/expr",
                        schemaPath: "#/$defs/path/pattern",
                        keyword: "pattern",
                        params: {
                          pattern:
                            "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
                        },
                        message:
                          'must match pattern "' +
                          "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$" +
                          '"',
                      };
                      if (vErrors === null) {
                        vErrors = [err17];
                      } else {
                        vErrors.push(err17);
                      }
                      errors++;
                    }
                  } else {
                    const err18 = {
                      instancePath:
                        instancePath +
                        "/vars/" +
                        key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                        "/expr",
                      schemaPath: "#/$defs/path/type",
                      keyword: "type",
                      params: { type: "string" },
                      message: "must be string",
                    };
                    if (vErrors === null) {
                      vErrors = [err18];
                    } else {
                      vErrors.push(err18);
                    }
                    errors++;
                  }
                }
                if (data2.format !== undefined) {
                  let data4 = data2.format;
                  if (data4 && typeof data4 == "object" && !Array.isArray(data4)) {
                    if (data4.fn === undefined) {
                      const err19 = {
                        instancePath:
                          instancePath +
                          "/vars/" +
                          key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                          "/format",
                        schemaPath: "#/$defs/format/required",
                        keyword: "required",
                        params: { missingProperty: "fn" },
                        message: "must have required property '" + "fn" + "'",
                      };
                      if (vErrors === null) {
                        vErrors = [err19];
                      } else {
                        vErrors.push(err19);
                      }
                      errors++;
                    }
                    for (const key4 in data4) {
                      if (!(key4 === "fn" || key4 === "args")) {
                        const err20 = {
                          instancePath:
                            instancePath +
                            "/vars/" +
                            key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                            "/format",
                          schemaPath: "#/$defs/format/additionalProperties",
                          keyword: "additionalProperties",
                          params: { additionalProperty: key4 },
                          message: "must NOT have additional properties",
                        };
                        if (vErrors === null) {
                          vErrors = [err20];
                        } else {
                          vErrors.push(err20);
                        }
                        errors++;
                      }
                    }
                    if (data4.fn !== undefined) {
                      let data5 = data4.fn;
                      if (typeof data5 === "string") {
                        if (!pattern13.test(data5)) {
                          const err21 = {
                            instancePath:
                              instancePath +
                              "/vars/" +
                              key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                              "/format/fn",
                            schemaPath: "#/$defs/format/properties/fn/pattern",
                            keyword: "pattern",
                            params: { pattern: "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*$" },
                            message:
                              'must match pattern "' +
                              "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*$" +
                              '"',
                          };
                          if (vErrors === null) {
                            vErrors = [err21];
                          } else {
                            vErrors.push(err21);
                          }
                          errors++;
                        }
                      } else {
                        const err22 = {
                          instancePath:
                            instancePath +
                            "/vars/" +
                            key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                            "/format/fn",
                          schemaPath: "#/$defs/format/properties/fn/type",
                          keyword: "type",
                          params: { type: "string" },
                          message: "must be string",
                        };
                        if (vErrors === null) {
                          vErrors = [err22];
                        } else {
                          vErrors.push(err22);
                        }
                        errors++;
                      }
                    }
                    if (data4.args !== undefined) {
                      let data6 = data4.args;
                      if (!(data6 && typeof data6 == "object" && !Array.isArray(data6))) {
                        const err23 = {
                          instancePath:
                            instancePath +
                            "/vars/" +
                            key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                            "/format/args",
                          schemaPath: "#/$defs/format/properties/args/type",
                          keyword: "type",
                          params: { type: "object" },
                          message: "must be object",
                        };
                        if (vErrors === null) {
                          vErrors = [err23];
                        } else {
                          vErrors.push(err23);
                        }
                        errors++;
                      }
                    }
                  } else {
                    const err24 = {
                      instancePath:
                        instancePath +
                        "/vars/" +
                        key2.replace(/~/g, "~0").replace(/\//g, "~1") +
                        "/format",
                      schemaPath: "#/$defs/format/type",
                      keyword: "type",
                      params: { type: "object" },
                      message: "must be object",
                    };
                    if (vErrors === null) {
                      vErrors = [err24];
                    } else {
                      vErrors.push(err24);
                    }
                    errors++;
                  }
                }
              } else {
                const err25 = {
                  instancePath:
                    instancePath + "/vars/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),
                  schemaPath: "#/else/then/properties/vars/additionalProperties/oneOf/1/type",
                  keyword: "type",
                  params: { type: "object" },
                  message: "must be object",
                };
                if (vErrors === null) {
                  vErrors = [err25];
                } else {
                  vErrors.push(err25);
                }
                errors++;
              }
              var _valid2 = _errs26 === errors;
              if (_valid2 && valid7) {
                valid7 = false;
                passing0 = [passing0, 1];
              } else {
                if (_valid2) {
                  valid7 = true;
                  passing0 = 1;
                }
              }
              if (!valid7) {
                const err26 = {
                  instancePath:
                    instancePath + "/vars/" + key2.replace(/~/g, "~0").replace(/\//g, "~1"),
                  schemaPath: "#/else/then/properties/vars/additionalProperties/oneOf",
                  keyword: "oneOf",
                  params: { passingSchemas: passing0 },
                  message: "must match exactly one schema in oneOf",
                };
                if (vErrors === null) {
                  vErrors = [err26];
                } else {
                  vErrors.push(err26);
                }
                errors++;
              } else {
                errors = _errs22;
                if (vErrors !== null) {
                  if (_errs22) {
                    vErrors.length = _errs22;
                  } else {
                    vErrors = null;
                  }
                }
              }
            }
          } else {
            const err27 = {
              instancePath: instancePath + "/vars",
              schemaPath: "#/else/then/properties/vars/type",
              keyword: "type",
              params: { type: "object" },
              message: "must be object",
            };
            if (vErrors === null) {
              vErrors = [err27];
            } else {
              vErrors.push(err27);
            }
            errors++;
          }
        }
      } else {
        const err28 = {
          instancePath,
          schemaPath: "#/else/then/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err28];
        } else {
          vErrors.push(err28);
        }
        errors++;
      }
      var _valid1 = _errs10 === errors;
      valid2 = _valid1;
      if (valid2) {
        var props1 = true;
      }
      ifClause1 = "then";
    } else {
      const _errs40 = errors;
      if (data && typeof data == "object" && !Array.isArray(data)) {
        if (data.expr === undefined) {
          const err29 = {
            instancePath,
            schemaPath: "#/else/else/required",
            keyword: "required",
            params: { missingProperty: "expr" },
            message: "must have required property '" + "expr" + "'",
          };
          if (vErrors === null) {
            vErrors = [err29];
          } else {
            vErrors.push(err29);
          }
          errors++;
        }
        for (const key5 in data) {
          if (!(key5 === "expr" || key5 === "format" || key5 === "default")) {
            const err30 = {
              instancePath,
              schemaPath: "#/else/else/additionalProperties",
              keyword: "additionalProperties",
              params: { additionalProperty: key5 },
              message: "must NOT have additional properties",
            };
            if (vErrors === null) {
              vErrors = [err30];
            } else {
              vErrors.push(err30);
            }
            errors++;
          }
        }
        if (data.expr !== undefined) {
          let data7 = data.expr;
          if (typeof data7 === "string") {
            if (!pattern9.test(data7)) {
              const err31 = {
                instancePath: instancePath + "/expr",
                schemaPath: "#/$defs/path/pattern",
                keyword: "pattern",
                params: {
                  pattern:
                    "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
                },
                message:
                  'must match pattern "' +
                  "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$" +
                  '"',
              };
              if (vErrors === null) {
                vErrors = [err31];
              } else {
                vErrors.push(err31);
              }
              errors++;
            }
          } else {
            const err32 = {
              instancePath: instancePath + "/expr",
              schemaPath: "#/$defs/path/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err32];
            } else {
              vErrors.push(err32);
            }
            errors++;
          }
        }
        if (data.format !== undefined) {
          let data8 = data.format;
          if (data8 && typeof data8 == "object" && !Array.isArray(data8)) {
            if (data8.fn === undefined) {
              const err33 = {
                instancePath: instancePath + "/format",
                schemaPath: "#/$defs/format/required",
                keyword: "required",
                params: { missingProperty: "fn" },
                message: "must have required property '" + "fn" + "'",
              };
              if (vErrors === null) {
                vErrors = [err33];
              } else {
                vErrors.push(err33);
              }
              errors++;
            }
            for (const key6 in data8) {
              if (!(key6 === "fn" || key6 === "args")) {
                const err34 = {
                  instancePath: instancePath + "/format",
                  schemaPath: "#/$defs/format/additionalProperties",
                  keyword: "additionalProperties",
                  params: { additionalProperty: key6 },
                  message: "must NOT have additional properties",
                };
                if (vErrors === null) {
                  vErrors = [err34];
                } else {
                  vErrors.push(err34);
                }
                errors++;
              }
            }
            if (data8.fn !== undefined) {
              let data9 = data8.fn;
              if (typeof data9 === "string") {
                if (!pattern13.test(data9)) {
                  const err35 = {
                    instancePath: instancePath + "/format/fn",
                    schemaPath: "#/$defs/format/properties/fn/pattern",
                    keyword: "pattern",
                    params: { pattern: "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*$" },
                    message:
                      'must match pattern "' + "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*$" + '"',
                  };
                  if (vErrors === null) {
                    vErrors = [err35];
                  } else {
                    vErrors.push(err35);
                  }
                  errors++;
                }
              } else {
                const err36 = {
                  instancePath: instancePath + "/format/fn",
                  schemaPath: "#/$defs/format/properties/fn/type",
                  keyword: "type",
                  params: { type: "string" },
                  message: "must be string",
                };
                if (vErrors === null) {
                  vErrors = [err36];
                } else {
                  vErrors.push(err36);
                }
                errors++;
              }
            }
            if (data8.args !== undefined) {
              let data10 = data8.args;
              if (!(data10 && typeof data10 == "object" && !Array.isArray(data10))) {
                const err37 = {
                  instancePath: instancePath + "/format/args",
                  schemaPath: "#/$defs/format/properties/args/type",
                  keyword: "type",
                  params: { type: "object" },
                  message: "must be object",
                };
                if (vErrors === null) {
                  vErrors = [err37];
                } else {
                  vErrors.push(err37);
                }
                errors++;
              }
            }
          } else {
            const err38 = {
              instancePath: instancePath + "/format",
              schemaPath: "#/$defs/format/type",
              keyword: "type",
              params: { type: "object" },
              message: "must be object",
            };
            if (vErrors === null) {
              vErrors = [err38];
            } else {
              vErrors.push(err38);
            }
            errors++;
          }
        }
      } else {
        const err39 = {
          instancePath,
          schemaPath: "#/else/else/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err39];
        } else {
          vErrors.push(err39);
        }
        errors++;
      }
      var _valid1 = _errs40 === errors;
      valid2 = _valid1;
      if (valid2) {
        if (props1 !== true) {
          props1 = true;
        }
      }
      ifClause1 = "else";
    }
    if (!valid2) {
      const err40 = {
        instancePath,
        schemaPath: "#/else/if",
        keyword: "if",
        params: { failingKeyword: ifClause1 },
        message: 'must match "' + ifClause1 + '" schema',
      };
      if (vErrors === null) {
        vErrors = [err40];
      } else {
        vErrors.push(err40);
      }
      errors++;
    }
    var _valid0 = _errs6 === errors;
    valid0 = _valid0;
    ifClause0 = "else";
  }
  if (!valid0) {
    const err41 = {
      instancePath,
      schemaPath: "#/if",
      keyword: "if",
      params: { failingKeyword: ifClause0 },
      message: 'must match "' + ifClause0 + '" schema',
    };
    if (vErrors === null) {
      vErrors = [err41];
    } else {
      vErrors.push(err41);
    }
    errors++;
  }
  validate22.errors = vErrors;
  evaluated0.props = props1;
  return errors === 0;
}
validate22.evaluated = { dynamicProps: true, dynamicItems: false };

const schema45 = {
  type: "object",
  properties: { states: { $ref: "#/$defs/designStates" } },
  propertyNames: { pattern: "^[a-z][A-Za-z]*$" },
  additionalProperties: { $ref: "#/$defs/designValue" },
};
const pattern16 = new RegExp("^[a-z][A-Za-z]*$", "u");
const schema46 = {
  if: { type: "object" },
  then: {
    if: { required: ["raw"], maxProperties: 1 },
    then: { $ref: "#/$defs/rawValue" },
    else: { $ref: "#/$defs/responsiveValue" },
  },
  else: { $ref: "#/$defs/scalarDesignValue" },
};
const schema47 = {
  type: "object",
  required: ["raw"],
  properties: { raw: { type: ["string", "number"] } },
  additionalProperties: false,
};
const schema48 = {
  type: "object",
  required: ["base"],
  properties: { base: { $ref: "#/$defs/scalarDesignValue" } },
  propertyNames: { not: { enum: ["raw", "token", "expr"] } },
  additionalProperties: { $ref: "#/$defs/scalarDesignValue" },
};
const schema49 = {
  if: { type: "object" },
  then: { $ref: "#/$defs/rawValue" },
  else: {
    oneOf: [
      { type: "string", pattern: "^[A-Za-z0-9][A-Za-z0-9:/._-]*$" },
      { type: "number" },
      { type: "boolean" },
      {
        type: "array",
        items: { oneOf: [{ type: "string" }, { $ref: "#/$defs/rawValue" }] },
        maxItems: 12,
      },
    ],
  },
};
const pattern17 = new RegExp("^[A-Za-z0-9][A-Za-z0-9:/._-]*$", "u");

function validate27(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate27.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs0 = errors;
  let valid0 = true;
  const _errs1 = errors;
  if (!(data && typeof data == "object" && !Array.isArray(data))) {
    const err0 = {};
    if (vErrors === null) {
      vErrors = [err0];
    } else {
      vErrors.push(err0);
    }
    errors++;
  }
  var _valid0 = _errs1 === errors;
  errors = _errs0;
  if (vErrors !== null) {
    if (_errs0) {
      vErrors.length = _errs0;
    } else {
      vErrors = null;
    }
  }
  let ifClause0;
  if (_valid0) {
    const _errs3 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.raw === undefined) {
        const err1 = {
          instancePath,
          schemaPath: "#/$defs/rawValue/required",
          keyword: "required",
          params: { missingProperty: "raw" },
          message: "must have required property '" + "raw" + "'",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
      for (const key0 in data) {
        if (!(key0 === "raw")) {
          const err2 = {
            instancePath,
            schemaPath: "#/$defs/rawValue/additionalProperties",
            keyword: "additionalProperties",
            params: { additionalProperty: key0 },
            message: "must NOT have additional properties",
          };
          if (vErrors === null) {
            vErrors = [err2];
          } else {
            vErrors.push(err2);
          }
          errors++;
        }
      }
      if (data.raw !== undefined) {
        let data0 = data.raw;
        if (typeof data0 !== "string" && !(typeof data0 == "number" && isFinite(data0))) {
          const err3 = {
            instancePath: instancePath + "/raw",
            schemaPath: "#/$defs/rawValue/properties/raw/type",
            keyword: "type",
            params: { type: schema47.properties.raw.type },
            message: "must be string,number",
          };
          if (vErrors === null) {
            vErrors = [err3];
          } else {
            vErrors.push(err3);
          }
          errors++;
        }
      }
    } else {
      const err4 = {
        instancePath,
        schemaPath: "#/$defs/rawValue/type",
        keyword: "type",
        params: { type: "object" },
        message: "must be object",
      };
      if (vErrors === null) {
        vErrors = [err4];
      } else {
        vErrors.push(err4);
      }
      errors++;
    }
    var _valid0 = _errs3 === errors;
    valid0 = _valid0;
    if (valid0) {
      var props0 = true;
    }
    ifClause0 = "then";
  } else {
    const _errs9 = errors;
    const _errs10 = errors;
    let valid3 = false;
    let passing0 = null;
    const _errs11 = errors;
    if (typeof data === "string") {
      if (!pattern17.test(data)) {
        const err5 = {
          instancePath,
          schemaPath: "#/else/oneOf/0/pattern",
          keyword: "pattern",
          params: { pattern: "^[A-Za-z0-9][A-Za-z0-9:/._-]*$" },
          message: 'must match pattern "' + "^[A-Za-z0-9][A-Za-z0-9:/._-]*$" + '"',
        };
        if (vErrors === null) {
          vErrors = [err5];
        } else {
          vErrors.push(err5);
        }
        errors++;
      }
    } else {
      const err6 = {
        instancePath,
        schemaPath: "#/else/oneOf/0/type",
        keyword: "type",
        params: { type: "string" },
        message: "must be string",
      };
      if (vErrors === null) {
        vErrors = [err6];
      } else {
        vErrors.push(err6);
      }
      errors++;
    }
    var _valid1 = _errs11 === errors;
    if (_valid1) {
      valid3 = true;
      passing0 = 0;
    }
    const _errs13 = errors;
    if (!(typeof data == "number" && isFinite(data))) {
      const err7 = {
        instancePath,
        schemaPath: "#/else/oneOf/1/type",
        keyword: "type",
        params: { type: "number" },
        message: "must be number",
      };
      if (vErrors === null) {
        vErrors = [err7];
      } else {
        vErrors.push(err7);
      }
      errors++;
    }
    var _valid1 = _errs13 === errors;
    if (_valid1 && valid3) {
      valid3 = false;
      passing0 = [passing0, 1];
    } else {
      if (_valid1) {
        valid3 = true;
        passing0 = 1;
      }
      const _errs15 = errors;
      if (typeof data !== "boolean") {
        const err8 = {
          instancePath,
          schemaPath: "#/else/oneOf/2/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err8];
        } else {
          vErrors.push(err8);
        }
        errors++;
      }
      var _valid1 = _errs15 === errors;
      if (_valid1 && valid3) {
        valid3 = false;
        passing0 = [passing0, 2];
      } else {
        if (_valid1) {
          valid3 = true;
          passing0 = 2;
        }
        const _errs17 = errors;
        if (Array.isArray(data)) {
          if (data.length > 12) {
            const err9 = {
              instancePath,
              schemaPath: "#/else/oneOf/3/maxItems",
              keyword: "maxItems",
              params: { limit: 12 },
              message: "must NOT have more than 12 items",
            };
            if (vErrors === null) {
              vErrors = [err9];
            } else {
              vErrors.push(err9);
            }
            errors++;
          }
          const len0 = data.length;
          for (let i0 = 0; i0 < len0; i0++) {
            let data1 = data[i0];
            const _errs20 = errors;
            let valid6 = false;
            let passing1 = null;
            const _errs21 = errors;
            if (typeof data1 !== "string") {
              const err10 = {
                instancePath: instancePath + "/" + i0,
                schemaPath: "#/else/oneOf/3/items/oneOf/0/type",
                keyword: "type",
                params: { type: "string" },
                message: "must be string",
              };
              if (vErrors === null) {
                vErrors = [err10];
              } else {
                vErrors.push(err10);
              }
              errors++;
            }
            var _valid2 = _errs21 === errors;
            if (_valid2) {
              valid6 = true;
              passing1 = 0;
            }
            const _errs23 = errors;
            if (data1 && typeof data1 == "object" && !Array.isArray(data1)) {
              if (data1.raw === undefined) {
                const err11 = {
                  instancePath: instancePath + "/" + i0,
                  schemaPath: "#/$defs/rawValue/required",
                  keyword: "required",
                  params: { missingProperty: "raw" },
                  message: "must have required property '" + "raw" + "'",
                };
                if (vErrors === null) {
                  vErrors = [err11];
                } else {
                  vErrors.push(err11);
                }
                errors++;
              }
              for (const key1 in data1) {
                if (!(key1 === "raw")) {
                  const err12 = {
                    instancePath: instancePath + "/" + i0,
                    schemaPath: "#/$defs/rawValue/additionalProperties",
                    keyword: "additionalProperties",
                    params: { additionalProperty: key1 },
                    message: "must NOT have additional properties",
                  };
                  if (vErrors === null) {
                    vErrors = [err12];
                  } else {
                    vErrors.push(err12);
                  }
                  errors++;
                }
              }
              if (data1.raw !== undefined) {
                let data2 = data1.raw;
                if (typeof data2 !== "string" && !(typeof data2 == "number" && isFinite(data2))) {
                  const err13 = {
                    instancePath: instancePath + "/" + i0 + "/raw",
                    schemaPath: "#/$defs/rawValue/properties/raw/type",
                    keyword: "type",
                    params: { type: schema47.properties.raw.type },
                    message: "must be string,number",
                  };
                  if (vErrors === null) {
                    vErrors = [err13];
                  } else {
                    vErrors.push(err13);
                  }
                  errors++;
                }
              }
            } else {
              const err14 = {
                instancePath: instancePath + "/" + i0,
                schemaPath: "#/$defs/rawValue/type",
                keyword: "type",
                params: { type: "object" },
                message: "must be object",
              };
              if (vErrors === null) {
                vErrors = [err14];
              } else {
                vErrors.push(err14);
              }
              errors++;
            }
            var _valid2 = _errs23 === errors;
            if (_valid2 && valid6) {
              valid6 = false;
              passing1 = [passing1, 1];
            } else {
              if (_valid2) {
                valid6 = true;
                passing1 = 1;
              }
            }
            if (!valid6) {
              const err15 = {
                instancePath: instancePath + "/" + i0,
                schemaPath: "#/else/oneOf/3/items/oneOf",
                keyword: "oneOf",
                params: { passingSchemas: passing1 },
                message: "must match exactly one schema in oneOf",
              };
              if (vErrors === null) {
                vErrors = [err15];
              } else {
                vErrors.push(err15);
              }
              errors++;
            } else {
              errors = _errs20;
              if (vErrors !== null) {
                if (_errs20) {
                  vErrors.length = _errs20;
                } else {
                  vErrors = null;
                }
              }
            }
          }
        } else {
          const err16 = {
            instancePath,
            schemaPath: "#/else/oneOf/3/type",
            keyword: "type",
            params: { type: "array" },
            message: "must be array",
          };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
        var _valid1 = _errs17 === errors;
        if (_valid1 && valid3) {
          valid3 = false;
          passing0 = [passing0, 3];
        } else {
          if (_valid1) {
            valid3 = true;
            passing0 = 3;
            var items0 = true;
          }
        }
      }
    }
    if (!valid3) {
      const err17 = {
        instancePath,
        schemaPath: "#/else/oneOf",
        keyword: "oneOf",
        params: { passingSchemas: passing0 },
        message: "must match exactly one schema in oneOf",
      };
      if (vErrors === null) {
        vErrors = [err17];
      } else {
        vErrors.push(err17);
      }
      errors++;
    } else {
      errors = _errs10;
      if (vErrors !== null) {
        if (_errs10) {
          vErrors.length = _errs10;
        } else {
          vErrors = null;
        }
      }
    }
    var _valid0 = _errs9 === errors;
    valid0 = _valid0;
    ifClause0 = "else";
  }
  if (!valid0) {
    const err18 = {
      instancePath,
      schemaPath: "#/if",
      keyword: "if",
      params: { failingKeyword: ifClause0 },
      message: 'must match "' + ifClause0 + '" schema',
    };
    if (vErrors === null) {
      vErrors = [err18];
    } else {
      vErrors.push(err18);
    }
    errors++;
  }
  validate27.errors = vErrors;
  evaluated0.props = props0;
  evaluated0.items = items0;
  return errors === 0;
}
validate27.evaluated = { dynamicProps: true, dynamicItems: true };

function validate26(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate26.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.base === undefined) {
      const err0 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "base" },
        message: "must have required property '" + "base" + "'",
      };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    for (const key0 in data) {
      const _errs1 = errors;
      const _errs2 = errors;
      const _errs3 = errors;
      if (!(key0 === "raw" || key0 === "token" || key0 === "expr")) {
        const err1 = {};
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
      var valid1 = _errs3 === errors;
      if (valid1) {
        const err2 = {
          instancePath,
          schemaPath: "#/propertyNames/not",
          keyword: "not",
          params: {},
          message: "must NOT be valid",
          propertyName: key0,
        };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      } else {
        errors = _errs2;
        if (vErrors !== null) {
          if (_errs2) {
            vErrors.length = _errs2;
          } else {
            vErrors = null;
          }
        }
      }
      var valid0 = _errs1 === errors;
      if (!valid0) {
        const err3 = {
          instancePath,
          schemaPath: "#/propertyNames",
          keyword: "propertyNames",
          params: { propertyName: key0 },
          message: "property name must be valid",
        };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    for (const key1 in data) {
      if (!(key1 === "base")) {
        if (
          !validate27(data[key1], {
            instancePath: instancePath + "/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
            parentData: data,
            parentDataProperty: key1,
            rootData,
            dynamicAnchors,
          })
        ) {
          vErrors = vErrors === null ? validate27.errors : vErrors.concat(validate27.errors);
          errors = vErrors.length;
        }
      }
    }
    if (data.base !== undefined) {
      if (
        !validate27(data.base, {
          instancePath: instancePath + "/base",
          parentData: data,
          parentDataProperty: "base",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate27.errors : vErrors.concat(validate27.errors);
        errors = vErrors.length;
      }
    }
  } else {
    const err4 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err4];
    } else {
      vErrors.push(err4);
    }
    errors++;
  }
  validate26.errors = vErrors;
  return errors === 0;
}
validate26.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

function validate25(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate25.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs0 = errors;
  let valid0 = true;
  const _errs1 = errors;
  if (!(data && typeof data == "object" && !Array.isArray(data))) {
    const err0 = {};
    if (vErrors === null) {
      vErrors = [err0];
    } else {
      vErrors.push(err0);
    }
    errors++;
  }
  var _valid0 = _errs1 === errors;
  errors = _errs0;
  if (vErrors !== null) {
    if (_errs0) {
      vErrors.length = _errs0;
    } else {
      vErrors = null;
    }
  }
  let ifClause0;
  if (_valid0) {
    const _errs3 = errors;
    const _errs4 = errors;
    let valid1 = true;
    const _errs5 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (Object.keys(data).length > 1) {
        const err1 = {};
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      } else {
        let missing0;
        if (data.raw === undefined && (missing0 = "raw")) {
          const err2 = {};
          if (vErrors === null) {
            vErrors = [err2];
          } else {
            vErrors.push(err2);
          }
          errors++;
        }
      }
    }
    var _valid1 = _errs5 === errors;
    errors = _errs4;
    if (vErrors !== null) {
      if (_errs4) {
        vErrors.length = _errs4;
      } else {
        vErrors = null;
      }
    }
    let ifClause1;
    if (_valid1) {
      const _errs6 = errors;
      if (data && typeof data == "object" && !Array.isArray(data)) {
        if (data.raw === undefined) {
          const err3 = {
            instancePath,
            schemaPath: "#/$defs/rawValue/required",
            keyword: "required",
            params: { missingProperty: "raw" },
            message: "must have required property '" + "raw" + "'",
          };
          if (vErrors === null) {
            vErrors = [err3];
          } else {
            vErrors.push(err3);
          }
          errors++;
        }
        for (const key0 in data) {
          if (!(key0 === "raw")) {
            const err4 = {
              instancePath,
              schemaPath: "#/$defs/rawValue/additionalProperties",
              keyword: "additionalProperties",
              params: { additionalProperty: key0 },
              message: "must NOT have additional properties",
            };
            if (vErrors === null) {
              vErrors = [err4];
            } else {
              vErrors.push(err4);
            }
            errors++;
          }
        }
        if (data.raw !== undefined) {
          let data0 = data.raw;
          if (typeof data0 !== "string" && !(typeof data0 == "number" && isFinite(data0))) {
            const err5 = {
              instancePath: instancePath + "/raw",
              schemaPath: "#/$defs/rawValue/properties/raw/type",
              keyword: "type",
              params: { type: schema47.properties.raw.type },
              message: "must be string,number",
            };
            if (vErrors === null) {
              vErrors = [err5];
            } else {
              vErrors.push(err5);
            }
            errors++;
          }
        }
      } else {
        const err6 = {
          instancePath,
          schemaPath: "#/$defs/rawValue/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
      var _valid1 = _errs6 === errors;
      valid1 = _valid1;
      if (valid1) {
        var props0 = true;
      }
      ifClause1 = "then";
    } else {
      const _errs12 = errors;
      if (
        !validate26(data, {
          instancePath,
          parentData,
          parentDataProperty,
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate26.errors : vErrors.concat(validate26.errors);
        errors = vErrors.length;
      }
      var _valid1 = _errs12 === errors;
      valid1 = _valid1;
      if (valid1) {
        if (props0 !== true) {
          props0 = true;
        }
      }
      ifClause1 = "else";
    }
    if (!valid1) {
      const err7 = {
        instancePath,
        schemaPath: "#/then/if",
        keyword: "if",
        params: { failingKeyword: ifClause1 },
        message: 'must match "' + ifClause1 + '" schema',
      };
      if (vErrors === null) {
        vErrors = [err7];
      } else {
        vErrors.push(err7);
      }
      errors++;
    }
    var _valid0 = _errs3 === errors;
    valid0 = _valid0;
    ifClause0 = "then";
  } else {
    const _errs13 = errors;
    if (
      !validate27(data, { instancePath, parentData, parentDataProperty, rootData, dynamicAnchors })
    ) {
      vErrors = vErrors === null ? validate27.errors : vErrors.concat(validate27.errors);
      errors = vErrors.length;
    } else {
      var props1 = validate27.evaluated.props;
      var items0 = validate27.evaluated.items;
    }
    var _valid0 = _errs13 === errors;
    valid0 = _valid0;
    if (valid0) {
      if (props0 !== true && props1 !== undefined) {
        if (props1 === true) {
          props0 = true;
        } else {
          props0 = props0 || {};
          Object.assign(props0, props1);
        }
      }
    }
    ifClause0 = "else";
  }
  if (!valid0) {
    const err8 = {
      instancePath,
      schemaPath: "#/if",
      keyword: "if",
      params: { failingKeyword: ifClause0 },
      message: 'must match "' + ifClause0 + '" schema',
    };
    if (vErrors === null) {
      vErrors = [err8];
    } else {
      vErrors.push(err8);
    }
    errors++;
  }
  validate25.errors = vErrors;
  evaluated0.props = props0;
  evaluated0.items = items0;
  return errors === 0;
}
validate25.evaluated = { dynamicProps: true, dynamicItems: true };

const schema52 = {
  type: "object",
  propertyNames: { enum: ["hover", "active", "focusVisible", "disabled"] },
  additionalProperties: {
    type: "object",
    propertyNames: { enum: ["background", "color", "borderColor", "shadow", "opacity", "outline"] },
    additionalProperties: { $ref: "#/$defs/scalarDesignValue" },
  },
};

function validate33(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate33.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    for (const key0 in data) {
      const _errs1 = errors;
      if (!(
        key0 === "hover" ||
        key0 === "active" ||
        key0 === "focusVisible" ||
        key0 === "disabled"
      )) {
        const err0 = {
          instancePath,
          schemaPath: "#/propertyNames/enum",
          keyword: "enum",
          params: { allowedValues: schema52.propertyNames.enum },
          message: "must be equal to one of the allowed values",
          propertyName: key0,
        };
        if (vErrors === null) {
          vErrors = [err0];
        } else {
          vErrors.push(err0);
        }
        errors++;
      }
      var valid0 = _errs1 === errors;
      if (!valid0) {
        const err1 = {
          instancePath,
          schemaPath: "#/propertyNames",
          keyword: "propertyNames",
          params: { propertyName: key0 },
          message: "property name must be valid",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    }
    for (const key1 in data) {
      let data0 = data[key1];
      if (data0 && typeof data0 == "object" && !Array.isArray(data0)) {
        for (const key2 in data0) {
          const _errs5 = errors;
          if (!(
            key2 === "background" ||
            key2 === "color" ||
            key2 === "borderColor" ||
            key2 === "shadow" ||
            key2 === "opacity" ||
            key2 === "outline"
          )) {
            const err2 = {
              instancePath: instancePath + "/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
              schemaPath: "#/additionalProperties/propertyNames/enum",
              keyword: "enum",
              params: { allowedValues: schema52.additionalProperties.propertyNames.enum },
              message: "must be equal to one of the allowed values",
              propertyName: key2,
            };
            if (vErrors === null) {
              vErrors = [err2];
            } else {
              vErrors.push(err2);
            }
            errors++;
          }
          var valid2 = _errs5 === errors;
          if (!valid2) {
            const err3 = {
              instancePath: instancePath + "/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
              schemaPath: "#/additionalProperties/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key2 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err3];
            } else {
              vErrors.push(err3);
            }
            errors++;
          }
        }
        for (const key3 in data0) {
          if (
            !validate27(data0[key3], {
              instancePath:
                instancePath +
                "/" +
                key1.replace(/~/g, "~0").replace(/\//g, "~1") +
                "/" +
                key3.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data0,
              parentDataProperty: key3,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate27.errors : vErrors.concat(validate27.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err4 = {
          instancePath: instancePath + "/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
          schemaPath: "#/additionalProperties/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
  } else {
    const err5 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err5];
    } else {
      vErrors.push(err5);
    }
    errors++;
  }
  validate33.errors = vErrors;
  return errors === 0;
}
validate33.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

function validate24(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate24.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    for (const key0 in data) {
      const _errs1 = errors;
      if (typeof key0 === "string") {
        if (!pattern16.test(key0)) {
          const err0 = {
            instancePath,
            schemaPath: "#/propertyNames/pattern",
            keyword: "pattern",
            params: { pattern: "^[a-z][A-Za-z]*$" },
            message: 'must match pattern "' + "^[a-z][A-Za-z]*$" + '"',
            propertyName: key0,
          };
          if (vErrors === null) {
            vErrors = [err0];
          } else {
            vErrors.push(err0);
          }
          errors++;
        }
      }
      var valid0 = _errs1 === errors;
      if (!valid0) {
        const err1 = {
          instancePath,
          schemaPath: "#/propertyNames",
          keyword: "propertyNames",
          params: { propertyName: key0 },
          message: "property name must be valid",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    }
    for (const key1 in data) {
      if (!(key1 === "states")) {
        if (
          !validate25(data[key1], {
            instancePath: instancePath + "/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
            parentData: data,
            parentDataProperty: key1,
            rootData,
            dynamicAnchors,
          })
        ) {
          vErrors = vErrors === null ? validate25.errors : vErrors.concat(validate25.errors);
          errors = vErrors.length;
        }
      }
    }
    if (data.states !== undefined) {
      if (
        !validate33(data.states, {
          instancePath: instancePath + "/states",
          parentData: data,
          parentDataProperty: "states",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate33.errors : vErrors.concat(validate33.errors);
        errors = vErrors.length;
      }
    }
  } else {
    const err2 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err2];
    } else {
      vErrors.push(err2);
    }
    errors++;
  }
  validate24.errors = vErrors;
  return errors === 0;
}
validate24.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

const schema53 = {
  type: "object",
  required: ["op"],
  oneOf: [
    {
      properties: { op: { enum: ["exists", "empty"] }, arg: { $ref: "#/$defs/path" } },
      required: ["arg"],
      additionalProperties: false,
    },
    {
      properties: {
        op: { enum: ["eq", "neq", "gt", "gte", "lt", "lte"] },
        args: {
          type: "array",
          prefixItems: [{ $ref: "#/$defs/operand" }, { $ref: "#/$defs/operand" }],
          minItems: 2,
          maxItems: 2,
        },
      },
      required: ["args"],
      additionalProperties: false,
    },
    {
      properties: {
        op: { const: "in" },
        args: {
          type: "array",
          prefixItems: [
            { $ref: "#/$defs/operand" },
            {
              type: "object",
              required: ["lit"],
              properties: { lit: { type: "array" } },
              additionalProperties: false,
            },
          ],
          minItems: 2,
          maxItems: 2,
        },
      },
      required: ["args"],
      additionalProperties: false,
    },
    {
      properties: {
        op: { enum: ["and", "or"] },
        args: { type: "array", minItems: 1, maxItems: 16, items: { $ref: "#/$defs/predicate" } },
      },
      required: ["args"],
      additionalProperties: false,
    },
    {
      properties: { op: { const: "not" }, arg: { $ref: "#/$defs/predicate" } },
      required: ["arg"],
      additionalProperties: false,
    },
  ],
  discriminator: { propertyName: "op" },
};
const schema56 = {
  if: { type: "object" },
  then: { $ref: "#/$defs/literal" },
  else: { $ref: "#/$defs/path" },
};
const schema57 = {
  type: "object",
  required: ["lit"],
  properties: { lit: {} },
  additionalProperties: false,
};

function validate38(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate38.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs0 = errors;
  let valid0 = true;
  const _errs1 = errors;
  if (!(data && typeof data == "object" && !Array.isArray(data))) {
    const err0 = {};
    if (vErrors === null) {
      vErrors = [err0];
    } else {
      vErrors.push(err0);
    }
    errors++;
  }
  var _valid0 = _errs1 === errors;
  errors = _errs0;
  if (vErrors !== null) {
    if (_errs0) {
      vErrors.length = _errs0;
    } else {
      vErrors = null;
    }
  }
  let ifClause0;
  if (_valid0) {
    const _errs3 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.lit === undefined) {
        const err1 = {
          instancePath,
          schemaPath: "#/$defs/literal/required",
          keyword: "required",
          params: { missingProperty: "lit" },
          message: "must have required property '" + "lit" + "'",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
      for (const key0 in data) {
        if (!(key0 === "lit")) {
          const err2 = {
            instancePath,
            schemaPath: "#/$defs/literal/additionalProperties",
            keyword: "additionalProperties",
            params: { additionalProperty: key0 },
            message: "must NOT have additional properties",
          };
          if (vErrors === null) {
            vErrors = [err2];
          } else {
            vErrors.push(err2);
          }
          errors++;
        }
      }
    } else {
      const err3 = {
        instancePath,
        schemaPath: "#/$defs/literal/type",
        keyword: "type",
        params: { type: "object" },
        message: "must be object",
      };
      if (vErrors === null) {
        vErrors = [err3];
      } else {
        vErrors.push(err3);
      }
      errors++;
    }
    var _valid0 = _errs3 === errors;
    valid0 = _valid0;
    if (valid0) {
      var props0 = true;
    }
    ifClause0 = "then";
  } else {
    const _errs7 = errors;
    if (typeof data === "string") {
      if (!pattern9.test(data)) {
        const err4 = {
          instancePath,
          schemaPath: "#/$defs/path/pattern",
          keyword: "pattern",
          params: {
            pattern:
              "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
          },
          message:
            'must match pattern "' +
            "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$" +
            '"',
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    } else {
      const err5 = {
        instancePath,
        schemaPath: "#/$defs/path/type",
        keyword: "type",
        params: { type: "string" },
        message: "must be string",
      };
      if (vErrors === null) {
        vErrors = [err5];
      } else {
        vErrors.push(err5);
      }
      errors++;
    }
    var _valid0 = _errs7 === errors;
    valid0 = _valid0;
    ifClause0 = "else";
  }
  if (!valid0) {
    const err6 = {
      instancePath,
      schemaPath: "#/if",
      keyword: "if",
      params: { failingKeyword: ifClause0 },
      message: 'must match "' + ifClause0 + '" schema',
    };
    if (vErrors === null) {
      vErrors = [err6];
    } else {
      vErrors.push(err6);
    }
    errors++;
  }
  validate38.errors = vErrors;
  evaluated0.props = props0;
  return errors === 0;
}
validate38.evaluated = { dynamicProps: true, dynamicItems: false };

const wrapper0 = { validate: validate37 };

function validate37(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate37.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.op === undefined) {
      const err0 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "op" },
        message: "must have required property '" + "op" + "'",
      };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    const tag0 = data.op;
    if (typeof tag0 == "string") {
      if (tag0 === "exists") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.arg === undefined) {
            const err1 = {
              instancePath,
              schemaPath: "#/oneOf/0/required",
              keyword: "required",
              params: { missingProperty: "arg" },
              message: "must have required property '" + "arg" + "'",
            };
            if (vErrors === null) {
              vErrors = [err1];
            } else {
              vErrors.push(err1);
            }
            errors++;
          }
          for (const key0 in data) {
            if (!(key0 === "op" || key0 === "arg")) {
              const err2 = {
                instancePath,
                schemaPath: "#/oneOf/0/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key0 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err2];
              } else {
                vErrors.push(err2);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data0 = data.op;
            if (!(data0 === "exists" || data0 === "empty")) {
              const err3 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/0/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[0].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err3];
              } else {
                vErrors.push(err3);
              }
              errors++;
            }
          }
          if (data.arg !== undefined) {
            let data1 = data.arg;
            if (typeof data1 === "string") {
              if (!pattern9.test(data1)) {
                const err4 = {
                  instancePath: instancePath + "/arg",
                  schemaPath: "#/$defs/path/pattern",
                  keyword: "pattern",
                  params: {
                    pattern:
                      "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
                  },
                  message:
                    'must match pattern "' +
                    "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$" +
                    '"',
                };
                if (vErrors === null) {
                  vErrors = [err4];
                } else {
                  vErrors.push(err4);
                }
                errors++;
              }
            } else {
              const err5 = {
                instancePath: instancePath + "/arg",
                schemaPath: "#/$defs/path/type",
                keyword: "type",
                params: { type: "string" },
                message: "must be string",
              };
              if (vErrors === null) {
                vErrors = [err5];
              } else {
                vErrors.push(err5);
              }
              errors++;
            }
          }
        }
        var props0 = true;
      } else if (tag0 === "empty") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.arg === undefined) {
            const err6 = {
              instancePath,
              schemaPath: "#/oneOf/0/required",
              keyword: "required",
              params: { missingProperty: "arg" },
              message: "must have required property '" + "arg" + "'",
            };
            if (vErrors === null) {
              vErrors = [err6];
            } else {
              vErrors.push(err6);
            }
            errors++;
          }
          for (const key1 in data) {
            if (!(key1 === "op" || key1 === "arg")) {
              const err7 = {
                instancePath,
                schemaPath: "#/oneOf/0/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key1 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err7];
              } else {
                vErrors.push(err7);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data2 = data.op;
            if (!(data2 === "exists" || data2 === "empty")) {
              const err8 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/0/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[0].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err8];
              } else {
                vErrors.push(err8);
              }
              errors++;
            }
          }
          if (data.arg !== undefined) {
            let data3 = data.arg;
            if (typeof data3 === "string") {
              if (!pattern9.test(data3)) {
                const err9 = {
                  instancePath: instancePath + "/arg",
                  schemaPath: "#/$defs/path/pattern",
                  keyword: "pattern",
                  params: {
                    pattern:
                      "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$",
                  },
                  message:
                    'must match pattern "' +
                    "^\\$(content|local|props|item|index|each|data|context)(\\.([A-Za-z][A-Za-z0-9_]*|[0-9]+))*$" +
                    '"',
                };
                if (vErrors === null) {
                  vErrors = [err9];
                } else {
                  vErrors.push(err9);
                }
                errors++;
              }
            } else {
              const err10 = {
                instancePath: instancePath + "/arg",
                schemaPath: "#/$defs/path/type",
                keyword: "type",
                params: { type: "string" },
                message: "must be string",
              };
              if (vErrors === null) {
                vErrors = [err10];
              } else {
                vErrors.push(err10);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "eq") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err11 = {
              instancePath,
              schemaPath: "#/oneOf/1/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err11];
            } else {
              vErrors.push(err11);
            }
            errors++;
          }
          for (const key2 in data) {
            if (!(key2 === "op" || key2 === "args")) {
              const err12 = {
                instancePath,
                schemaPath: "#/oneOf/1/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key2 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err12];
              } else {
                vErrors.push(err12);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data4 = data.op;
            if (!(
              data4 === "eq" ||
              data4 === "neq" ||
              data4 === "gt" ||
              data4 === "gte" ||
              data4 === "lt" ||
              data4 === "lte"
            )) {
              const err13 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/1/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[1].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err13];
              } else {
                vErrors.push(err13);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data5 = data.args;
            if (Array.isArray(data5)) {
              if (data5.length > 2) {
                const err14 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 2 },
                  message: "must NOT have more than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err14];
                } else {
                  vErrors.push(err14);
                }
                errors++;
              }
              if (data5.length < 2) {
                const err15 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 2 },
                  message: "must NOT have fewer than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err15];
                } else {
                  vErrors.push(err15);
                }
                errors++;
              }
              const len0 = data5.length;
              if (len0 > 0) {
                if (
                  !validate38(data5[0], {
                    instancePath: instancePath + "/args/0",
                    parentData: data5,
                    parentDataProperty: 0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
              if (len0 > 1) {
                if (
                  !validate38(data5[1], {
                    instancePath: instancePath + "/args/1",
                    parentData: data5,
                    parentDataProperty: 1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err16 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/1/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err16];
              } else {
                vErrors.push(err16);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "neq") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err17 = {
              instancePath,
              schemaPath: "#/oneOf/1/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err17];
            } else {
              vErrors.push(err17);
            }
            errors++;
          }
          for (const key3 in data) {
            if (!(key3 === "op" || key3 === "args")) {
              const err18 = {
                instancePath,
                schemaPath: "#/oneOf/1/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key3 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err18];
              } else {
                vErrors.push(err18);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data8 = data.op;
            if (!(
              data8 === "eq" ||
              data8 === "neq" ||
              data8 === "gt" ||
              data8 === "gte" ||
              data8 === "lt" ||
              data8 === "lte"
            )) {
              const err19 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/1/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[1].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err19];
              } else {
                vErrors.push(err19);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data9 = data.args;
            if (Array.isArray(data9)) {
              if (data9.length > 2) {
                const err20 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 2 },
                  message: "must NOT have more than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err20];
                } else {
                  vErrors.push(err20);
                }
                errors++;
              }
              if (data9.length < 2) {
                const err21 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 2 },
                  message: "must NOT have fewer than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err21];
                } else {
                  vErrors.push(err21);
                }
                errors++;
              }
              const len1 = data9.length;
              if (len1 > 0) {
                if (
                  !validate38(data9[0], {
                    instancePath: instancePath + "/args/0",
                    parentData: data9,
                    parentDataProperty: 0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
              if (len1 > 1) {
                if (
                  !validate38(data9[1], {
                    instancePath: instancePath + "/args/1",
                    parentData: data9,
                    parentDataProperty: 1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err22 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/1/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err22];
              } else {
                vErrors.push(err22);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "gt") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err23 = {
              instancePath,
              schemaPath: "#/oneOf/1/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err23];
            } else {
              vErrors.push(err23);
            }
            errors++;
          }
          for (const key4 in data) {
            if (!(key4 === "op" || key4 === "args")) {
              const err24 = {
                instancePath,
                schemaPath: "#/oneOf/1/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key4 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err24];
              } else {
                vErrors.push(err24);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data12 = data.op;
            if (!(
              data12 === "eq" ||
              data12 === "neq" ||
              data12 === "gt" ||
              data12 === "gte" ||
              data12 === "lt" ||
              data12 === "lte"
            )) {
              const err25 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/1/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[1].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err25];
              } else {
                vErrors.push(err25);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data13 = data.args;
            if (Array.isArray(data13)) {
              if (data13.length > 2) {
                const err26 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 2 },
                  message: "must NOT have more than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err26];
                } else {
                  vErrors.push(err26);
                }
                errors++;
              }
              if (data13.length < 2) {
                const err27 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 2 },
                  message: "must NOT have fewer than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err27];
                } else {
                  vErrors.push(err27);
                }
                errors++;
              }
              const len2 = data13.length;
              if (len2 > 0) {
                if (
                  !validate38(data13[0], {
                    instancePath: instancePath + "/args/0",
                    parentData: data13,
                    parentDataProperty: 0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
              if (len2 > 1) {
                if (
                  !validate38(data13[1], {
                    instancePath: instancePath + "/args/1",
                    parentData: data13,
                    parentDataProperty: 1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err28 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/1/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err28];
              } else {
                vErrors.push(err28);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "gte") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err29 = {
              instancePath,
              schemaPath: "#/oneOf/1/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err29];
            } else {
              vErrors.push(err29);
            }
            errors++;
          }
          for (const key5 in data) {
            if (!(key5 === "op" || key5 === "args")) {
              const err30 = {
                instancePath,
                schemaPath: "#/oneOf/1/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key5 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err30];
              } else {
                vErrors.push(err30);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data16 = data.op;
            if (!(
              data16 === "eq" ||
              data16 === "neq" ||
              data16 === "gt" ||
              data16 === "gte" ||
              data16 === "lt" ||
              data16 === "lte"
            )) {
              const err31 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/1/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[1].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err31];
              } else {
                vErrors.push(err31);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data17 = data.args;
            if (Array.isArray(data17)) {
              if (data17.length > 2) {
                const err32 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 2 },
                  message: "must NOT have more than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err32];
                } else {
                  vErrors.push(err32);
                }
                errors++;
              }
              if (data17.length < 2) {
                const err33 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 2 },
                  message: "must NOT have fewer than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err33];
                } else {
                  vErrors.push(err33);
                }
                errors++;
              }
              const len3 = data17.length;
              if (len3 > 0) {
                if (
                  !validate38(data17[0], {
                    instancePath: instancePath + "/args/0",
                    parentData: data17,
                    parentDataProperty: 0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
              if (len3 > 1) {
                if (
                  !validate38(data17[1], {
                    instancePath: instancePath + "/args/1",
                    parentData: data17,
                    parentDataProperty: 1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err34 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/1/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err34];
              } else {
                vErrors.push(err34);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "lt") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err35 = {
              instancePath,
              schemaPath: "#/oneOf/1/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err35];
            } else {
              vErrors.push(err35);
            }
            errors++;
          }
          for (const key6 in data) {
            if (!(key6 === "op" || key6 === "args")) {
              const err36 = {
                instancePath,
                schemaPath: "#/oneOf/1/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key6 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err36];
              } else {
                vErrors.push(err36);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data20 = data.op;
            if (!(
              data20 === "eq" ||
              data20 === "neq" ||
              data20 === "gt" ||
              data20 === "gte" ||
              data20 === "lt" ||
              data20 === "lte"
            )) {
              const err37 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/1/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[1].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err37];
              } else {
                vErrors.push(err37);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data21 = data.args;
            if (Array.isArray(data21)) {
              if (data21.length > 2) {
                const err38 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 2 },
                  message: "must NOT have more than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err38];
                } else {
                  vErrors.push(err38);
                }
                errors++;
              }
              if (data21.length < 2) {
                const err39 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 2 },
                  message: "must NOT have fewer than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err39];
                } else {
                  vErrors.push(err39);
                }
                errors++;
              }
              const len4 = data21.length;
              if (len4 > 0) {
                if (
                  !validate38(data21[0], {
                    instancePath: instancePath + "/args/0",
                    parentData: data21,
                    parentDataProperty: 0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
              if (len4 > 1) {
                if (
                  !validate38(data21[1], {
                    instancePath: instancePath + "/args/1",
                    parentData: data21,
                    parentDataProperty: 1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err40 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/1/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err40];
              } else {
                vErrors.push(err40);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "lte") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err41 = {
              instancePath,
              schemaPath: "#/oneOf/1/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err41];
            } else {
              vErrors.push(err41);
            }
            errors++;
          }
          for (const key7 in data) {
            if (!(key7 === "op" || key7 === "args")) {
              const err42 = {
                instancePath,
                schemaPath: "#/oneOf/1/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key7 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err42];
              } else {
                vErrors.push(err42);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data24 = data.op;
            if (!(
              data24 === "eq" ||
              data24 === "neq" ||
              data24 === "gt" ||
              data24 === "gte" ||
              data24 === "lt" ||
              data24 === "lte"
            )) {
              const err43 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/1/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[1].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err43];
              } else {
                vErrors.push(err43);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data25 = data.args;
            if (Array.isArray(data25)) {
              if (data25.length > 2) {
                const err44 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 2 },
                  message: "must NOT have more than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err44];
                } else {
                  vErrors.push(err44);
                }
                errors++;
              }
              if (data25.length < 2) {
                const err45 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/1/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 2 },
                  message: "must NOT have fewer than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err45];
                } else {
                  vErrors.push(err45);
                }
                errors++;
              }
              const len5 = data25.length;
              if (len5 > 0) {
                if (
                  !validate38(data25[0], {
                    instancePath: instancePath + "/args/0",
                    parentData: data25,
                    parentDataProperty: 0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
              if (len5 > 1) {
                if (
                  !validate38(data25[1], {
                    instancePath: instancePath + "/args/1",
                    parentData: data25,
                    parentDataProperty: 1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err46 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/1/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err46];
              } else {
                vErrors.push(err46);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "in") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err47 = {
              instancePath,
              schemaPath: "#/oneOf/2/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err47];
            } else {
              vErrors.push(err47);
            }
            errors++;
          }
          for (const key8 in data) {
            if (!(key8 === "op" || key8 === "args")) {
              const err48 = {
                instancePath,
                schemaPath: "#/oneOf/2/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key8 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err48];
              } else {
                vErrors.push(err48);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            if ("in" !== data.op) {
              const err49 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/2/properties/op/const",
                keyword: "const",
                params: { allowedValue: "in" },
                message: "must be equal to constant",
              };
              if (vErrors === null) {
                vErrors = [err49];
              } else {
                vErrors.push(err49);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data29 = data.args;
            if (Array.isArray(data29)) {
              if (data29.length > 2) {
                const err50 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/2/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 2 },
                  message: "must NOT have more than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err50];
                } else {
                  vErrors.push(err50);
                }
                errors++;
              }
              if (data29.length < 2) {
                const err51 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/2/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 2 },
                  message: "must NOT have fewer than 2 items",
                };
                if (vErrors === null) {
                  vErrors = [err51];
                } else {
                  vErrors.push(err51);
                }
                errors++;
              }
              const len6 = data29.length;
              if (len6 > 0) {
                if (
                  !validate38(data29[0], {
                    instancePath: instancePath + "/args/0",
                    parentData: data29,
                    parentDataProperty: 0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
              if (len6 > 1) {
                let data31 = data29[1];
                if (data31 && typeof data31 == "object" && !Array.isArray(data31)) {
                  if (data31.lit === undefined) {
                    const err52 = {
                      instancePath: instancePath + "/args/1",
                      schemaPath: "#/oneOf/2/properties/args/prefixItems/1/required",
                      keyword: "required",
                      params: { missingProperty: "lit" },
                      message: "must have required property '" + "lit" + "'",
                    };
                    if (vErrors === null) {
                      vErrors = [err52];
                    } else {
                      vErrors.push(err52);
                    }
                    errors++;
                  }
                  for (const key9 in data31) {
                    if (!(key9 === "lit")) {
                      const err53 = {
                        instancePath: instancePath + "/args/1",
                        schemaPath: "#/oneOf/2/properties/args/prefixItems/1/additionalProperties",
                        keyword: "additionalProperties",
                        params: { additionalProperty: key9 },
                        message: "must NOT have additional properties",
                      };
                      if (vErrors === null) {
                        vErrors = [err53];
                      } else {
                        vErrors.push(err53);
                      }
                      errors++;
                    }
                  }
                  if (data31.lit !== undefined) {
                    if (!Array.isArray(data31.lit)) {
                      const err54 = {
                        instancePath: instancePath + "/args/1/lit",
                        schemaPath: "#/oneOf/2/properties/args/prefixItems/1/properties/lit/type",
                        keyword: "type",
                        params: { type: "array" },
                        message: "must be array",
                      };
                      if (vErrors === null) {
                        vErrors = [err54];
                      } else {
                        vErrors.push(err54);
                      }
                      errors++;
                    }
                  }
                } else {
                  const err55 = {
                    instancePath: instancePath + "/args/1",
                    schemaPath: "#/oneOf/2/properties/args/prefixItems/1/type",
                    keyword: "type",
                    params: { type: "object" },
                    message: "must be object",
                  };
                  if (vErrors === null) {
                    vErrors = [err55];
                  } else {
                    vErrors.push(err55);
                  }
                  errors++;
                }
              }
            } else {
              const err56 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/2/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err56];
              } else {
                vErrors.push(err56);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "and") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err57 = {
              instancePath,
              schemaPath: "#/oneOf/3/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err57];
            } else {
              vErrors.push(err57);
            }
            errors++;
          }
          for (const key10 in data) {
            if (!(key10 === "op" || key10 === "args")) {
              const err58 = {
                instancePath,
                schemaPath: "#/oneOf/3/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key10 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err58];
              } else {
                vErrors.push(err58);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data33 = data.op;
            if (!(data33 === "and" || data33 === "or")) {
              const err59 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/3/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[3].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err59];
              } else {
                vErrors.push(err59);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data34 = data.args;
            if (Array.isArray(data34)) {
              if (data34.length > 16) {
                const err60 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/3/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 16 },
                  message: "must NOT have more than 16 items",
                };
                if (vErrors === null) {
                  vErrors = [err60];
                } else {
                  vErrors.push(err60);
                }
                errors++;
              }
              if (data34.length < 1) {
                const err61 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/3/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 1 },
                  message: "must NOT have fewer than 1 items",
                };
                if (vErrors === null) {
                  vErrors = [err61];
                } else {
                  vErrors.push(err61);
                }
                errors++;
              }
              const len7 = data34.length;
              for (let i0 = 0; i0 < len7; i0++) {
                if (
                  !wrapper0.validate(data34[i0], {
                    instancePath: instancePath + "/args/" + i0,
                    parentData: data34,
                    parentDataProperty: i0,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null
                      ? wrapper0.validate.errors
                      : vErrors.concat(wrapper0.validate.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err62 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/3/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err62];
              } else {
                vErrors.push(err62);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "or") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.args === undefined) {
            const err63 = {
              instancePath,
              schemaPath: "#/oneOf/3/required",
              keyword: "required",
              params: { missingProperty: "args" },
              message: "must have required property '" + "args" + "'",
            };
            if (vErrors === null) {
              vErrors = [err63];
            } else {
              vErrors.push(err63);
            }
            errors++;
          }
          for (const key11 in data) {
            if (!(key11 === "op" || key11 === "args")) {
              const err64 = {
                instancePath,
                schemaPath: "#/oneOf/3/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key11 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err64];
              } else {
                vErrors.push(err64);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            let data36 = data.op;
            if (!(data36 === "and" || data36 === "or")) {
              const err65 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/3/properties/op/enum",
                keyword: "enum",
                params: { allowedValues: schema53.oneOf[3].properties.op.enum },
                message: "must be equal to one of the allowed values",
              };
              if (vErrors === null) {
                vErrors = [err65];
              } else {
                vErrors.push(err65);
              }
              errors++;
            }
          }
          if (data.args !== undefined) {
            let data37 = data.args;
            if (Array.isArray(data37)) {
              if (data37.length > 16) {
                const err66 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/3/properties/args/maxItems",
                  keyword: "maxItems",
                  params: { limit: 16 },
                  message: "must NOT have more than 16 items",
                };
                if (vErrors === null) {
                  vErrors = [err66];
                } else {
                  vErrors.push(err66);
                }
                errors++;
              }
              if (data37.length < 1) {
                const err67 = {
                  instancePath: instancePath + "/args",
                  schemaPath: "#/oneOf/3/properties/args/minItems",
                  keyword: "minItems",
                  params: { limit: 1 },
                  message: "must NOT have fewer than 1 items",
                };
                if (vErrors === null) {
                  vErrors = [err67];
                } else {
                  vErrors.push(err67);
                }
                errors++;
              }
              const len8 = data37.length;
              for (let i1 = 0; i1 < len8; i1++) {
                if (
                  !wrapper0.validate(data37[i1], {
                    instancePath: instancePath + "/args/" + i1,
                    parentData: data37,
                    parentDataProperty: i1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null
                      ? wrapper0.validate.errors
                      : vErrors.concat(wrapper0.validate.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err68 = {
                instancePath: instancePath + "/args",
                schemaPath: "#/oneOf/3/properties/args/type",
                keyword: "type",
                params: { type: "array" },
                message: "must be array",
              };
              if (vErrors === null) {
                vErrors = [err68];
              } else {
                vErrors.push(err68);
              }
              errors++;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else if (tag0 === "not") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.arg === undefined) {
            const err69 = {
              instancePath,
              schemaPath: "#/oneOf/4/required",
              keyword: "required",
              params: { missingProperty: "arg" },
              message: "must have required property '" + "arg" + "'",
            };
            if (vErrors === null) {
              vErrors = [err69];
            } else {
              vErrors.push(err69);
            }
            errors++;
          }
          for (const key12 in data) {
            if (!(key12 === "op" || key12 === "arg")) {
              const err70 = {
                instancePath,
                schemaPath: "#/oneOf/4/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key12 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err70];
              } else {
                vErrors.push(err70);
              }
              errors++;
            }
          }
          if (data.op !== undefined) {
            if ("not" !== data.op) {
              const err71 = {
                instancePath: instancePath + "/op",
                schemaPath: "#/oneOf/4/properties/op/const",
                keyword: "const",
                params: { allowedValue: "not" },
                message: "must be equal to constant",
              };
              if (vErrors === null) {
                vErrors = [err71];
              } else {
                vErrors.push(err71);
              }
              errors++;
            }
          }
          if (data.arg !== undefined) {
            if (
              !wrapper0.validate(data.arg, {
                instancePath: instancePath + "/arg",
                parentData: data,
                parentDataProperty: "arg",
                rootData,
                dynamicAnchors,
              })
            ) {
              vErrors =
                vErrors === null
                  ? wrapper0.validate.errors
                  : vErrors.concat(wrapper0.validate.errors);
              errors = vErrors.length;
            }
          }
        }
        if (props0 !== true) {
          props0 = true;
        }
      } else {
        const err72 = {
          instancePath,
          schemaPath: "#/discriminator",
          keyword: "discriminator",
          params: { error: "mapping", tag: "op", tagValue: tag0 },
          message: 'value of tag "op" must be in oneOf',
        };
        if (vErrors === null) {
          vErrors = [err72];
        } else {
          vErrors.push(err72);
        }
        errors++;
      }
    } else {
      const err73 = {
        instancePath,
        schemaPath: "#/discriminator",
        keyword: "discriminator",
        params: { error: "tag", tag: "op", tagValue: tag0 },
        message: 'tag "op" must be string',
      };
      if (vErrors === null) {
        vErrors = [err73];
      } else {
        vErrors.push(err73);
      }
      errors++;
    }
  } else {
    const err74 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err74];
    } else {
      vErrors.push(err74);
    }
    errors++;
  }
  validate37.errors = vErrors;
  evaluated0.props = props0;
  return errors === 0;
}
validate37.evaluated = { dynamicProps: true, dynamicItems: false };

const schema60 = {
  type: "object",
  required: ["action"],
  properties: {
    action: {
      type: "string",
      pattern: "^([a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*|pending:[0-9a-f-]{36})$",
    },
    args: { type: "object", additionalProperties: { $ref: "#/$defs/actionArg" } },
  },
  additionalProperties: false,
};
const pattern22 = new RegExp(
  "^([a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*|pending:[0-9a-f-]{36})$",
  "u",
);
const schema61 = {
  if: { type: "object", required: ["kind"] },
  then: { $ref: "#/$defs/link" },
  else: { $ref: "#/$defs/operand" },
};
const schema62 = {
  type: "object",
  required: ["kind"],
  oneOf: [
    {
      properties: {
        kind: { const: "page" },
        page: { $ref: "#/$defs/uuid" },
        params: { type: "object", additionalProperties: { $ref: "#/$defs/operand" } },
      },
      required: ["page"],
      additionalProperties: false,
    },
    {
      properties: {
        kind: { const: "url" },
        url: { type: "string", pattern: "^(https?:|mailto:|tel:|/)" },
      },
      required: ["url"],
      additionalProperties: false,
    },
    {
      properties: { kind: { const: "anchor" }, node: { $ref: "#/$defs/nodeId" } },
      required: ["node"],
      additionalProperties: false,
    },
  ],
  discriminator: { propertyName: "kind" },
};
const pattern24 = new RegExp("^(https?:|mailto:|tel:|/)", "u");

function validate55(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate55.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.kind === undefined) {
      const err0 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "kind" },
        message: "must have required property '" + "kind" + "'",
      };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    const tag0 = data.kind;
    if (typeof tag0 == "string") {
      if (tag0 === "page") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.page === undefined) {
            const err1 = {
              instancePath,
              schemaPath: "#/oneOf/0/required",
              keyword: "required",
              params: { missingProperty: "page" },
              message: "must have required property '" + "page" + "'",
            };
            if (vErrors === null) {
              vErrors = [err1];
            } else {
              vErrors.push(err1);
            }
            errors++;
          }
          for (const key0 in data) {
            if (!(key0 === "kind" || key0 === "page" || key0 === "params")) {
              const err2 = {
                instancePath,
                schemaPath: "#/oneOf/0/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key0 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err2];
              } else {
                vErrors.push(err2);
              }
              errors++;
            }
          }
          if (data.kind !== undefined) {
            if ("page" !== data.kind) {
              const err3 = {
                instancePath: instancePath + "/kind",
                schemaPath: "#/oneOf/0/properties/kind/const",
                keyword: "const",
                params: { allowedValue: "page" },
                message: "must be equal to constant",
              };
              if (vErrors === null) {
                vErrors = [err3];
              } else {
                vErrors.push(err3);
              }
              errors++;
            }
          }
          if (data.page !== undefined) {
            let data1 = data.page;
            if (typeof data1 === "string") {
              if (!pattern8.test(data1)) {
                const err4 = {
                  instancePath: instancePath + "/page",
                  schemaPath: "#/$defs/uuid/pattern",
                  keyword: "pattern",
                  params: {
                    pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
                  },
                  message:
                    'must match pattern "' +
                    "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" +
                    '"',
                };
                if (vErrors === null) {
                  vErrors = [err4];
                } else {
                  vErrors.push(err4);
                }
                errors++;
              }
            } else {
              const err5 = {
                instancePath: instancePath + "/page",
                schemaPath: "#/$defs/uuid/type",
                keyword: "type",
                params: { type: "string" },
                message: "must be string",
              };
              if (vErrors === null) {
                vErrors = [err5];
              } else {
                vErrors.push(err5);
              }
              errors++;
            }
          }
          if (data.params !== undefined) {
            let data2 = data.params;
            if (data2 && typeof data2 == "object" && !Array.isArray(data2)) {
              for (const key1 in data2) {
                if (
                  !validate38(data2[key1], {
                    instancePath:
                      instancePath + "/params/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
                    parentData: data2,
                    parentDataProperty: key1,
                    rootData,
                    dynamicAnchors,
                  })
                ) {
                  vErrors =
                    vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
                  errors = vErrors.length;
                }
              }
            } else {
              const err6 = {
                instancePath: instancePath + "/params",
                schemaPath: "#/oneOf/0/properties/params/type",
                keyword: "type",
                params: { type: "object" },
                message: "must be object",
              };
              if (vErrors === null) {
                vErrors = [err6];
              } else {
                vErrors.push(err6);
              }
              errors++;
            }
          }
        }
        var props1 = true;
      } else if (tag0 === "url") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.url === undefined) {
            const err7 = {
              instancePath,
              schemaPath: "#/oneOf/1/required",
              keyword: "required",
              params: { missingProperty: "url" },
              message: "must have required property '" + "url" + "'",
            };
            if (vErrors === null) {
              vErrors = [err7];
            } else {
              vErrors.push(err7);
            }
            errors++;
          }
          for (const key2 in data) {
            if (!(key2 === "kind" || key2 === "url")) {
              const err8 = {
                instancePath,
                schemaPath: "#/oneOf/1/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key2 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err8];
              } else {
                vErrors.push(err8);
              }
              errors++;
            }
          }
          if (data.kind !== undefined) {
            if ("url" !== data.kind) {
              const err9 = {
                instancePath: instancePath + "/kind",
                schemaPath: "#/oneOf/1/properties/kind/const",
                keyword: "const",
                params: { allowedValue: "url" },
                message: "must be equal to constant",
              };
              if (vErrors === null) {
                vErrors = [err9];
              } else {
                vErrors.push(err9);
              }
              errors++;
            }
          }
          if (data.url !== undefined) {
            let data5 = data.url;
            if (typeof data5 === "string") {
              if (!pattern24.test(data5)) {
                const err10 = {
                  instancePath: instancePath + "/url",
                  schemaPath: "#/oneOf/1/properties/url/pattern",
                  keyword: "pattern",
                  params: { pattern: "^(https?:|mailto:|tel:|/)" },
                  message: 'must match pattern "' + "^(https?:|mailto:|tel:|/)" + '"',
                };
                if (vErrors === null) {
                  vErrors = [err10];
                } else {
                  vErrors.push(err10);
                }
                errors++;
              }
            } else {
              const err11 = {
                instancePath: instancePath + "/url",
                schemaPath: "#/oneOf/1/properties/url/type",
                keyword: "type",
                params: { type: "string" },
                message: "must be string",
              };
              if (vErrors === null) {
                vErrors = [err11];
              } else {
                vErrors.push(err11);
              }
              errors++;
            }
          }
        }
        if (props1 !== true) {
          props1 = true;
        }
      } else if (tag0 === "anchor") {
        if (data && typeof data == "object" && !Array.isArray(data)) {
          if (data.node === undefined) {
            const err12 = {
              instancePath,
              schemaPath: "#/oneOf/2/required",
              keyword: "required",
              params: { missingProperty: "node" },
              message: "must have required property '" + "node" + "'",
            };
            if (vErrors === null) {
              vErrors = [err12];
            } else {
              vErrors.push(err12);
            }
            errors++;
          }
          for (const key3 in data) {
            if (!(key3 === "kind" || key3 === "node")) {
              const err13 = {
                instancePath,
                schemaPath: "#/oneOf/2/additionalProperties",
                keyword: "additionalProperties",
                params: { additionalProperty: key3 },
                message: "must NOT have additional properties",
              };
              if (vErrors === null) {
                vErrors = [err13];
              } else {
                vErrors.push(err13);
              }
              errors++;
            }
          }
          if (data.kind !== undefined) {
            if ("anchor" !== data.kind) {
              const err14 = {
                instancePath: instancePath + "/kind",
                schemaPath: "#/oneOf/2/properties/kind/const",
                keyword: "const",
                params: { allowedValue: "anchor" },
                message: "must be equal to constant",
              };
              if (vErrors === null) {
                vErrors = [err14];
              } else {
                vErrors.push(err14);
              }
              errors++;
            }
          }
          if (data.node !== undefined) {
            let data7 = data.node;
            if (typeof data7 === "string") {
              if (!pattern4.test(data7)) {
                const err15 = {
                  instancePath: instancePath + "/node",
                  schemaPath: "#/$defs/nodeId/pattern",
                  keyword: "pattern",
                  params: { pattern: "^[A-Za-z0-9_-]{4,32}$" },
                  message: 'must match pattern "' + "^[A-Za-z0-9_-]{4,32}$" + '"',
                };
                if (vErrors === null) {
                  vErrors = [err15];
                } else {
                  vErrors.push(err15);
                }
                errors++;
              }
            } else {
              const err16 = {
                instancePath: instancePath + "/node",
                schemaPath: "#/$defs/nodeId/type",
                keyword: "type",
                params: { type: "string" },
                message: "must be string",
              };
              if (vErrors === null) {
                vErrors = [err16];
              } else {
                vErrors.push(err16);
              }
              errors++;
            }
          }
        }
        if (props1 !== true) {
          props1 = true;
        }
      } else {
        const err17 = {
          instancePath,
          schemaPath: "#/discriminator",
          keyword: "discriminator",
          params: { error: "mapping", tag: "kind", tagValue: tag0 },
          message: 'value of tag "kind" must be in oneOf',
        };
        if (vErrors === null) {
          vErrors = [err17];
        } else {
          vErrors.push(err17);
        }
        errors++;
      }
    } else {
      const err18 = {
        instancePath,
        schemaPath: "#/discriminator",
        keyword: "discriminator",
        params: { error: "tag", tag: "kind", tagValue: tag0 },
        message: 'tag "kind" must be string',
      };
      if (vErrors === null) {
        vErrors = [err18];
      } else {
        vErrors.push(err18);
      }
      errors++;
    }
  } else {
    const err19 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err19];
    } else {
      vErrors.push(err19);
    }
    errors++;
  }
  validate55.errors = vErrors;
  evaluated0.props = props1;
  return errors === 0;
}
validate55.evaluated = { dynamicProps: true, dynamicItems: false };

function validate54(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate54.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs0 = errors;
  let valid0 = true;
  const _errs1 = errors;
  if (errors === _errs1) {
    if (data && typeof data == "object" && !Array.isArray(data)) {
      let missing0;
      if (data.kind === undefined && (missing0 = "kind")) {
        const err0 = {};
        if (vErrors === null) {
          vErrors = [err0];
        } else {
          vErrors.push(err0);
        }
        errors++;
      }
    } else {
      const err1 = {};
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
  }
  var _valid0 = _errs1 === errors;
  errors = _errs0;
  if (vErrors !== null) {
    if (_errs0) {
      vErrors.length = _errs0;
    } else {
      vErrors = null;
    }
  }
  let ifClause0;
  if (_valid0) {
    const _errs3 = errors;
    if (
      !validate55(data, { instancePath, parentData, parentDataProperty, rootData, dynamicAnchors })
    ) {
      vErrors = vErrors === null ? validate55.errors : vErrors.concat(validate55.errors);
      errors = vErrors.length;
    } else {
      var props0 = validate55.evaluated.props;
    }
    var _valid0 = _errs3 === errors;
    valid0 = _valid0;
    ifClause0 = "then";
  } else {
    const _errs4 = errors;
    if (
      !validate38(data, { instancePath, parentData, parentDataProperty, rootData, dynamicAnchors })
    ) {
      vErrors = vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
      errors = vErrors.length;
    } else {
      var props1 = validate38.evaluated.props;
    }
    var _valid0 = _errs4 === errors;
    valid0 = _valid0;
    if (valid0) {
      if (props0 !== true && props1 !== undefined) {
        if (props1 === true) {
          props0 = true;
        } else {
          props0 = props0 || {};
          Object.assign(props0, props1);
        }
      }
    }
    ifClause0 = "else";
  }
  if (!valid0) {
    const err2 = {
      instancePath,
      schemaPath: "#/if",
      keyword: "if",
      params: { failingKeyword: ifClause0 },
      message: 'must match "' + ifClause0 + '" schema',
    };
    if (vErrors === null) {
      vErrors = [err2];
    } else {
      vErrors.push(err2);
    }
    errors++;
  }
  validate54.errors = vErrors;
  evaluated0.props = props0;
  return errors === 0;
}
validate54.evaluated = { dynamicProps: true, dynamicItems: false };

function validate53(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate53.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.action === undefined) {
      const err0 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "action" },
        message: "must have required property '" + "action" + "'",
      };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "action" || key0 === "args")) {
        const err1 = {
          instancePath,
          schemaPath: "#/additionalProperties",
          keyword: "additionalProperties",
          params: { additionalProperty: key0 },
          message: "must NOT have additional properties",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    }
    if (data.action !== undefined) {
      let data0 = data.action;
      if (typeof data0 === "string") {
        if (!pattern22.test(data0)) {
          const err2 = {
            instancePath: instancePath + "/action",
            schemaPath: "#/properties/action/pattern",
            keyword: "pattern",
            params: {
              pattern: "^([a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*|pending:[0-9a-f-]{36})$",
            },
            message:
              'must match pattern "' +
              "^([a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)*|pending:[0-9a-f-]{36})$" +
              '"',
          };
          if (vErrors === null) {
            vErrors = [err2];
          } else {
            vErrors.push(err2);
          }
          errors++;
        }
      } else {
        const err3 = {
          instancePath: instancePath + "/action",
          schemaPath: "#/properties/action/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    if (data.args !== undefined) {
      let data1 = data.args;
      if (data1 && typeof data1 == "object" && !Array.isArray(data1)) {
        for (const key1 in data1) {
          if (
            !validate54(data1[key1], {
              instancePath: instancePath + "/args/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data1,
              parentDataProperty: key1,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate54.errors : vErrors.concat(validate54.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err4 = {
          instancePath: instancePath + "/args",
          schemaPath: "#/properties/args/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
  } else {
    const err5 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err5];
    } else {
      vErrors.push(err5);
    }
    errors++;
  }
  validate53.errors = vErrors;
  return errors === 0;
}
validate53.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

const schema68 = {
  type: "object",
  required: ["id", "mode"],
  properties: {
    id: { $ref: "#/$defs/identifier" },
    mode: { $ref: "#/$defs/mode" },
    maxMode: { $ref: "#/$defs/mode" },
    allowedTypes: { type: "array", items: { type: "string" } },
    deniedTypes: { type: "array", items: { type: "string" } },
    locked: { type: "boolean" },
    requiredApprovalRole: { type: "string" },
  },
  additionalProperties: false,
};
const schema70 = { enum: ["STRICT", "SYSTEM", "FREE", "CODE"] };

function validate62(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate62.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.id === undefined) {
      const err0 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "id" },
        message: "must have required property '" + "id" + "'",
      };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.mode === undefined) {
      const err1 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "mode" },
        message: "must have required property '" + "mode" + "'",
      };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(
        key0 === "id" ||
        key0 === "mode" ||
        key0 === "maxMode" ||
        key0 === "allowedTypes" ||
        key0 === "deniedTypes" ||
        key0 === "locked" ||
        key0 === "requiredApprovalRole"
      )) {
        const err2 = {
          instancePath,
          schemaPath: "#/additionalProperties",
          keyword: "additionalProperties",
          params: { additionalProperty: key0 },
          message: "must NOT have additional properties",
        };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    if (data.id !== undefined) {
      let data0 = data.id;
      if (typeof data0 === "string") {
        if (!pattern10.test(data0)) {
          const err3 = {
            instancePath: instancePath + "/id",
            schemaPath: "#/$defs/identifier/pattern",
            keyword: "pattern",
            params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
            message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
          };
          if (vErrors === null) {
            vErrors = [err3];
          } else {
            vErrors.push(err3);
          }
          errors++;
        }
      } else {
        const err4 = {
          instancePath: instancePath + "/id",
          schemaPath: "#/$defs/identifier/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
    if (data.mode !== undefined) {
      let data1 = data.mode;
      if (!(data1 === "STRICT" || data1 === "SYSTEM" || data1 === "FREE" || data1 === "CODE")) {
        const err5 = {
          instancePath: instancePath + "/mode",
          schemaPath: "#/$defs/mode/enum",
          keyword: "enum",
          params: { allowedValues: schema70.enum },
          message: "must be equal to one of the allowed values",
        };
        if (vErrors === null) {
          vErrors = [err5];
        } else {
          vErrors.push(err5);
        }
        errors++;
      }
    }
    if (data.maxMode !== undefined) {
      let data2 = data.maxMode;
      if (!(data2 === "STRICT" || data2 === "SYSTEM" || data2 === "FREE" || data2 === "CODE")) {
        const err6 = {
          instancePath: instancePath + "/maxMode",
          schemaPath: "#/$defs/mode/enum",
          keyword: "enum",
          params: { allowedValues: schema70.enum },
          message: "must be equal to one of the allowed values",
        };
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
    if (data.allowedTypes !== undefined) {
      let data3 = data.allowedTypes;
      if (Array.isArray(data3)) {
        const len0 = data3.length;
        for (let i0 = 0; i0 < len0; i0++) {
          if (typeof data3[i0] !== "string") {
            const err7 = {
              instancePath: instancePath + "/allowedTypes/" + i0,
              schemaPath: "#/properties/allowedTypes/items/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err7];
            } else {
              vErrors.push(err7);
            }
            errors++;
          }
        }
      } else {
        const err8 = {
          instancePath: instancePath + "/allowedTypes",
          schemaPath: "#/properties/allowedTypes/type",
          keyword: "type",
          params: { type: "array" },
          message: "must be array",
        };
        if (vErrors === null) {
          vErrors = [err8];
        } else {
          vErrors.push(err8);
        }
        errors++;
      }
    }
    if (data.deniedTypes !== undefined) {
      let data5 = data.deniedTypes;
      if (Array.isArray(data5)) {
        const len1 = data5.length;
        for (let i1 = 0; i1 < len1; i1++) {
          if (typeof data5[i1] !== "string") {
            const err9 = {
              instancePath: instancePath + "/deniedTypes/" + i1,
              schemaPath: "#/properties/deniedTypes/items/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err9];
            } else {
              vErrors.push(err9);
            }
            errors++;
          }
        }
      } else {
        const err10 = {
          instancePath: instancePath + "/deniedTypes",
          schemaPath: "#/properties/deniedTypes/type",
          keyword: "type",
          params: { type: "array" },
          message: "must be array",
        };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
    }
    if (data.locked !== undefined) {
      if (typeof data.locked !== "boolean") {
        const err11 = {
          instancePath: instancePath + "/locked",
          schemaPath: "#/properties/locked/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err11];
        } else {
          vErrors.push(err11);
        }
        errors++;
      }
    }
    if (data.requiredApprovalRole !== undefined) {
      if (typeof data.requiredApprovalRole !== "string") {
        const err12 = {
          instancePath: instancePath + "/requiredApprovalRole",
          schemaPath: "#/properties/requiredApprovalRole/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err12];
        } else {
          vErrors.push(err12);
        }
        errors++;
      }
    }
  } else {
    const err13 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err13];
    } else {
      vErrors.push(err13);
    }
    errors++;
  }
  validate62.errors = vErrors;
  return errors === 0;
}
validate62.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

function validate21(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate21.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs2 = errors;
  let valid1 = true;
  const _errs3 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    let missing0;
    if (data.type === undefined && (missing0 = "type")) {
      const err0 = {};
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    } else {
      if (data.type !== undefined) {
        if ("Composed" !== data.type) {
          const err1 = {};
          if (vErrors === null) {
            vErrors = [err1];
          } else {
            vErrors.push(err1);
          }
          errors++;
        }
      }
    }
  }
  var _valid0 = _errs3 === errors;
  errors = _errs2;
  if (vErrors !== null) {
    if (_errs2) {
      vErrors.length = _errs2;
    } else {
      vErrors = null;
    }
  }
  let ifClause0;
  if (_valid0) {
    const _errs5 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.ref === undefined) {
        const err2 = {
          instancePath,
          schemaPath: "#/allOf/0/then/required",
          keyword: "required",
          params: { missingProperty: "ref" },
          message: "must have required property '" + "ref" + "'",
        };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    var _valid0 = _errs5 === errors;
    valid1 = _valid0;
    ifClause0 = "then";
  } else {
    const _errs6 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.ref !== undefined) {
        const err3 = {
          instancePath: instancePath + "/ref",
          schemaPath: "#/allOf/0/else/properties/ref/false schema",
          keyword: "false schema",
          params: {},
          message: "boolean schema is false",
        };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    var _valid0 = _errs6 === errors;
    valid1 = _valid0;
    if (valid1) {
      var props0 = {};
      props0.ref = true;
      props0.type = true;
    }
    ifClause0 = "else";
  }
  if (!valid1) {
    const err4 = {
      instancePath,
      schemaPath: "#/allOf/0/if",
      keyword: "if",
      params: { failingKeyword: ifClause0 },
      message: 'must match "' + ifClause0 + '" schema',
    };
    if (vErrors === null) {
      vErrors = [err4];
    } else {
      vErrors.push(err4);
    }
    errors++;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.id === undefined) {
      const err5 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "id" },
        message: "must have required property '" + "id" + "'",
      };
      if (vErrors === null) {
        vErrors = [err5];
      } else {
        vErrors.push(err5);
      }
      errors++;
    }
    if (data.type === undefined) {
      const err6 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "type" },
        message: "must have required property '" + "type" + "'",
      };
      if (vErrors === null) {
        vErrors = [err6];
      } else {
        vErrors.push(err6);
      }
      errors++;
    }
    if (props0 !== true) {
      props0 = props0 || {};
      props0.id = true;
      props0.type = true;
      props0.ref = true;
      props0.name = true;
      props0.props = true;
      props0.bindings = true;
      props0.design = true;
      props0.when = true;
      props0.on = true;
      props0.children = true;
      props0.slots = true;
      props0.zone = true;
      props0.locked = true;
    }
    if (data.id !== undefined) {
      let data2 = data.id;
      if (typeof data2 === "string") {
        if (!pattern4.test(data2)) {
          const err7 = {
            instancePath: instancePath + "/id",
            schemaPath: "#/$defs/nodeId/pattern",
            keyword: "pattern",
            params: { pattern: "^[A-Za-z0-9_-]{4,32}$" },
            message: 'must match pattern "' + "^[A-Za-z0-9_-]{4,32}$" + '"',
          };
          if (vErrors === null) {
            vErrors = [err7];
          } else {
            vErrors.push(err7);
          }
          errors++;
        }
      } else {
        const err8 = {
          instancePath: instancePath + "/id",
          schemaPath: "#/$defs/nodeId/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err8];
        } else {
          vErrors.push(err8);
        }
        errors++;
      }
    }
    if (data.type !== undefined) {
      let data3 = data.type;
      if (typeof data3 === "string") {
        if (!pattern7.test(data3)) {
          const err9 = {
            instancePath: instancePath + "/type",
            schemaPath: "#/properties/type/pattern",
            keyword: "pattern",
            params: { pattern: "^[A-Z][A-Za-z0-9]*$" },
            message: 'must match pattern "' + "^[A-Z][A-Za-z0-9]*$" + '"',
          };
          if (vErrors === null) {
            vErrors = [err9];
          } else {
            vErrors.push(err9);
          }
          errors++;
        }
      } else {
        const err10 = {
          instancePath: instancePath + "/type",
          schemaPath: "#/properties/type/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
    }
    if (data.ref !== undefined) {
      let data4 = data.ref;
      if (data4 && typeof data4 == "object" && !Array.isArray(data4)) {
        if (data4.component === undefined) {
          const err11 = {
            instancePath: instancePath + "/ref",
            schemaPath: "#/properties/ref/required",
            keyword: "required",
            params: { missingProperty: "component" },
            message: "must have required property '" + "component" + "'",
          };
          if (vErrors === null) {
            vErrors = [err11];
          } else {
            vErrors.push(err11);
          }
          errors++;
        }
        for (const key0 in data4) {
          if (!(key0 === "component" || key0 === "version")) {
            const err12 = {
              instancePath: instancePath + "/ref",
              schemaPath: "#/properties/ref/additionalProperties",
              keyword: "additionalProperties",
              params: { additionalProperty: key0 },
              message: "must NOT have additional properties",
            };
            if (vErrors === null) {
              vErrors = [err12];
            } else {
              vErrors.push(err12);
            }
            errors++;
          }
        }
        if (data4.component !== undefined) {
          let data5 = data4.component;
          if (typeof data5 === "string") {
            if (!pattern8.test(data5)) {
              const err13 = {
                instancePath: instancePath + "/ref/component",
                schemaPath: "#/$defs/uuid/pattern",
                keyword: "pattern",
                params: {
                  pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
                },
                message:
                  'must match pattern "' +
                  "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" +
                  '"',
              };
              if (vErrors === null) {
                vErrors = [err13];
              } else {
                vErrors.push(err13);
              }
              errors++;
            }
          } else {
            const err14 = {
              instancePath: instancePath + "/ref/component",
              schemaPath: "#/$defs/uuid/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err14];
            } else {
              vErrors.push(err14);
            }
            errors++;
          }
        }
        if (data4.version !== undefined) {
          let data6 = data4.version;
          const _errs19 = errors;
          let valid8 = false;
          let passing0 = null;
          const _errs20 = errors;
          if ("live" !== data6) {
            const err15 = {
              instancePath: instancePath + "/ref/version",
              schemaPath: "#/properties/ref/properties/version/oneOf/0/const",
              keyword: "const",
              params: { allowedValue: "live" },
              message: "must be equal to constant",
            };
            if (vErrors === null) {
              vErrors = [err15];
            } else {
              vErrors.push(err15);
            }
            errors++;
          }
          var _valid1 = _errs20 === errors;
          if (_valid1) {
            valid8 = true;
            passing0 = 0;
          }
          const _errs21 = errors;
          if (!(typeof data6 == "number" && !(data6 % 1) && !isNaN(data6) && isFinite(data6))) {
            const err16 = {
              instancePath: instancePath + "/ref/version",
              schemaPath: "#/properties/ref/properties/version/oneOf/1/type",
              keyword: "type",
              params: { type: "integer" },
              message: "must be integer",
            };
            if (vErrors === null) {
              vErrors = [err16];
            } else {
              vErrors.push(err16);
            }
            errors++;
          }
          if (typeof data6 == "number" && isFinite(data6)) {
            if (data6 < 1 || isNaN(data6)) {
              const err17 = {
                instancePath: instancePath + "/ref/version",
                schemaPath: "#/properties/ref/properties/version/oneOf/1/minimum",
                keyword: "minimum",
                params: { comparison: ">=", limit: 1 },
                message: "must be >= 1",
              };
              if (vErrors === null) {
                vErrors = [err17];
              } else {
                vErrors.push(err17);
              }
              errors++;
            }
          }
          var _valid1 = _errs21 === errors;
          if (_valid1 && valid8) {
            valid8 = false;
            passing0 = [passing0, 1];
          } else {
            if (_valid1) {
              valid8 = true;
              passing0 = 1;
            }
          }
          if (!valid8) {
            const err18 = {
              instancePath: instancePath + "/ref/version",
              schemaPath: "#/properties/ref/properties/version/oneOf",
              keyword: "oneOf",
              params: { passingSchemas: passing0 },
              message: "must match exactly one schema in oneOf",
            };
            if (vErrors === null) {
              vErrors = [err18];
            } else {
              vErrors.push(err18);
            }
            errors++;
          } else {
            errors = _errs19;
            if (vErrors !== null) {
              if (_errs19) {
                vErrors.length = _errs19;
              } else {
                vErrors = null;
              }
            }
          }
        }
      } else {
        const err19 = {
          instancePath: instancePath + "/ref",
          schemaPath: "#/properties/ref/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err19];
        } else {
          vErrors.push(err19);
        }
        errors++;
      }
    }
    if (data.name !== undefined) {
      let data7 = data.name;
      if (typeof data7 === "string") {
        if (func1(data7) > 120) {
          const err20 = {
            instancePath: instancePath + "/name",
            schemaPath: "#/properties/name/maxLength",
            keyword: "maxLength",
            params: { limit: 120 },
            message: "must NOT have more than 120 characters",
          };
          if (vErrors === null) {
            vErrors = [err20];
          } else {
            vErrors.push(err20);
          }
          errors++;
        }
      } else {
        const err21 = {
          instancePath: instancePath + "/name",
          schemaPath: "#/properties/name/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err21];
        } else {
          vErrors.push(err21);
        }
        errors++;
      }
    }
    if (data.props !== undefined) {
      let data8 = data.props;
      if (!(data8 && typeof data8 == "object" && !Array.isArray(data8))) {
        const err22 = {
          instancePath: instancePath + "/props",
          schemaPath: "#/properties/props/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err22];
        } else {
          vErrors.push(err22);
        }
        errors++;
      }
    }
    if (data.bindings !== undefined) {
      let data9 = data.bindings;
      if (data9 && typeof data9 == "object" && !Array.isArray(data9)) {
        for (const key1 in data9) {
          if (
            !validate22(data9[key1], {
              instancePath:
                instancePath + "/bindings/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data9,
              parentDataProperty: key1,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate22.errors : vErrors.concat(validate22.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err23 = {
          instancePath: instancePath + "/bindings",
          schemaPath: "#/properties/bindings/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err23];
        } else {
          vErrors.push(err23);
        }
        errors++;
      }
    }
    if (data.design !== undefined) {
      if (
        !validate24(data.design, {
          instancePath: instancePath + "/design",
          parentData: data,
          parentDataProperty: "design",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate24.errors : vErrors.concat(validate24.errors);
        errors = vErrors.length;
      }
    }
    if (data.when !== undefined) {
      if (
        !validate37(data.when, {
          instancePath: instancePath + "/when",
          parentData: data,
          parentDataProperty: "when",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate37.errors : vErrors.concat(validate37.errors);
        errors = vErrors.length;
      }
    }
    if (data.on !== undefined) {
      let data13 = data.on;
      if (data13 && typeof data13 == "object" && !Array.isArray(data13)) {
        for (const key2 in data13) {
          const _errs35 = errors;
          if (typeof key2 === "string") {
            if (!pattern10.test(key2)) {
              const err24 = {
                instancePath: instancePath + "/on",
                schemaPath: "#/$defs/identifier/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
                propertyName: key2,
              };
              if (vErrors === null) {
                vErrors = [err24];
              } else {
                vErrors.push(err24);
              }
              errors++;
            }
          } else {
            const err25 = {
              instancePath: instancePath + "/on",
              schemaPath: "#/$defs/identifier/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
              propertyName: key2,
            };
            if (vErrors === null) {
              vErrors = [err25];
            } else {
              vErrors.push(err25);
            }
            errors++;
          }
          var valid10 = _errs35 === errors;
          if (!valid10) {
            const err26 = {
              instancePath: instancePath + "/on",
              schemaPath: "#/properties/on/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key2 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err26];
            } else {
              vErrors.push(err26);
            }
            errors++;
          }
        }
        for (const key3 in data13) {
          let data14 = data13[key3];
          const _errs40 = errors;
          let valid13 = false;
          let passing1 = null;
          const _errs41 = errors;
          if (
            !validate53(data14, {
              instancePath: instancePath + "/on/" + key3.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data13,
              parentDataProperty: key3,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate53.errors : vErrors.concat(validate53.errors);
            errors = vErrors.length;
          }
          var _valid2 = _errs41 === errors;
          if (_valid2) {
            valid13 = true;
            passing1 = 0;
          }
          const _errs42 = errors;
          if (Array.isArray(data14)) {
            if (data14.length > 8) {
              const err27 = {
                instancePath: instancePath + "/on/" + key3.replace(/~/g, "~0").replace(/\//g, "~1"),
                schemaPath: "#/properties/on/additionalProperties/oneOf/1/maxItems",
                keyword: "maxItems",
                params: { limit: 8 },
                message: "must NOT have more than 8 items",
              };
              if (vErrors === null) {
                vErrors = [err27];
              } else {
                vErrors.push(err27);
              }
              errors++;
            }
            if (data14.length < 1) {
              const err28 = {
                instancePath: instancePath + "/on/" + key3.replace(/~/g, "~0").replace(/\//g, "~1"),
                schemaPath: "#/properties/on/additionalProperties/oneOf/1/minItems",
                keyword: "minItems",
                params: { limit: 1 },
                message: "must NOT have fewer than 1 items",
              };
              if (vErrors === null) {
                vErrors = [err28];
              } else {
                vErrors.push(err28);
              }
              errors++;
            }
            const len0 = data14.length;
            for (let i0 = 0; i0 < len0; i0++) {
              if (
                !validate53(data14[i0], {
                  instancePath:
                    instancePath +
                    "/on/" +
                    key3.replace(/~/g, "~0").replace(/\//g, "~1") +
                    "/" +
                    i0,
                  parentData: data14,
                  parentDataProperty: i0,
                  rootData,
                  dynamicAnchors,
                })
              ) {
                vErrors = vErrors === null ? validate53.errors : vErrors.concat(validate53.errors);
                errors = vErrors.length;
              }
            }
          } else {
            const err29 = {
              instancePath: instancePath + "/on/" + key3.replace(/~/g, "~0").replace(/\//g, "~1"),
              schemaPath: "#/properties/on/additionalProperties/oneOf/1/type",
              keyword: "type",
              params: { type: "array" },
              message: "must be array",
            };
            if (vErrors === null) {
              vErrors = [err29];
            } else {
              vErrors.push(err29);
            }
            errors++;
          }
          var _valid2 = _errs42 === errors;
          if (_valid2 && valid13) {
            valid13 = false;
            passing1 = [passing1, 1];
          } else {
            if (_valid2) {
              valid13 = true;
              passing1 = 1;
            }
          }
          if (!valid13) {
            const err30 = {
              instancePath: instancePath + "/on/" + key3.replace(/~/g, "~0").replace(/\//g, "~1"),
              schemaPath: "#/properties/on/additionalProperties/oneOf",
              keyword: "oneOf",
              params: { passingSchemas: passing1 },
              message: "must match exactly one schema in oneOf",
            };
            if (vErrors === null) {
              vErrors = [err30];
            } else {
              vErrors.push(err30);
            }
            errors++;
          } else {
            errors = _errs40;
            if (vErrors !== null) {
              if (_errs40) {
                vErrors.length = _errs40;
              } else {
                vErrors = null;
              }
            }
          }
        }
      } else {
        const err31 = {
          instancePath: instancePath + "/on",
          schemaPath: "#/properties/on/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err31];
        } else {
          vErrors.push(err31);
        }
        errors++;
      }
    }
    if (data.children !== undefined) {
      let data16 = data.children;
      if (Array.isArray(data16)) {
        const len1 = data16.length;
        for (let i1 = 0; i1 < len1; i1++) {
          let data17 = data16[i1];
          if (typeof data17 === "string") {
            if (!pattern4.test(data17)) {
              const err32 = {
                instancePath: instancePath + "/children/" + i1,
                schemaPath: "#/$defs/nodeId/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z0-9_-]{4,32}$" },
                message: 'must match pattern "' + "^[A-Za-z0-9_-]{4,32}$" + '"',
              };
              if (vErrors === null) {
                vErrors = [err32];
              } else {
                vErrors.push(err32);
              }
              errors++;
            }
          } else {
            const err33 = {
              instancePath: instancePath + "/children/" + i1,
              schemaPath: "#/$defs/nodeId/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err33];
            } else {
              vErrors.push(err33);
            }
            errors++;
          }
        }
        let i2 = data16.length;
        let j0;
        if (i2 > 1) {
          outer0: for (; i2--;) {
            for (j0 = i2; j0--;) {
              if (func0(data16[i2], data16[j0])) {
                const err34 = {
                  instancePath: instancePath + "/children",
                  schemaPath: "#/properties/children/uniqueItems",
                  keyword: "uniqueItems",
                  params: { i: i2, j: j0 },
                  message:
                    "must NOT have duplicate items (items ## " +
                    j0 +
                    " and " +
                    i2 +
                    " are identical)",
                };
                if (vErrors === null) {
                  vErrors = [err34];
                } else {
                  vErrors.push(err34);
                }
                errors++;
                break outer0;
              }
            }
          }
        }
      } else {
        const err35 = {
          instancePath: instancePath + "/children",
          schemaPath: "#/properties/children/type",
          keyword: "type",
          params: { type: "array" },
          message: "must be array",
        };
        if (vErrors === null) {
          vErrors = [err35];
        } else {
          vErrors.push(err35);
        }
        errors++;
      }
    }
    if (data.slots !== undefined) {
      let data18 = data.slots;
      if (data18 && typeof data18 == "object" && !Array.isArray(data18)) {
        for (const key4 in data18) {
          const _errs52 = errors;
          if (typeof key4 === "string") {
            if (!pattern10.test(key4)) {
              const err36 = {
                instancePath: instancePath + "/slots",
                schemaPath: "#/$defs/identifier/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
                propertyName: key4,
              };
              if (vErrors === null) {
                vErrors = [err36];
              } else {
                vErrors.push(err36);
              }
              errors++;
            }
          } else {
            const err37 = {
              instancePath: instancePath + "/slots",
              schemaPath: "#/$defs/identifier/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
              propertyName: key4,
            };
            if (vErrors === null) {
              vErrors = [err37];
            } else {
              vErrors.push(err37);
            }
            errors++;
          }
          var valid20 = _errs52 === errors;
          if (!valid20) {
            const err38 = {
              instancePath: instancePath + "/slots",
              schemaPath: "#/properties/slots/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key4 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err38];
            } else {
              vErrors.push(err38);
            }
            errors++;
          }
        }
        for (const key5 in data18) {
          let data19 = data18[key5];
          if (Array.isArray(data19)) {
            const len2 = data19.length;
            for (let i3 = 0; i3 < len2; i3++) {
              let data20 = data19[i3];
              if (typeof data20 === "string") {
                if (!pattern4.test(data20)) {
                  const err39 = {
                    instancePath:
                      instancePath +
                      "/slots/" +
                      key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                      "/" +
                      i3,
                    schemaPath: "#/$defs/nodeId/pattern",
                    keyword: "pattern",
                    params: { pattern: "^[A-Za-z0-9_-]{4,32}$" },
                    message: 'must match pattern "' + "^[A-Za-z0-9_-]{4,32}$" + '"',
                  };
                  if (vErrors === null) {
                    vErrors = [err39];
                  } else {
                    vErrors.push(err39);
                  }
                  errors++;
                }
              } else {
                const err40 = {
                  instancePath:
                    instancePath +
                    "/slots/" +
                    key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                    "/" +
                    i3,
                  schemaPath: "#/$defs/nodeId/type",
                  keyword: "type",
                  params: { type: "string" },
                  message: "must be string",
                };
                if (vErrors === null) {
                  vErrors = [err40];
                } else {
                  vErrors.push(err40);
                }
                errors++;
              }
            }
            let i4 = data19.length;
            let j1;
            if (i4 > 1) {
              outer1: for (; i4--;) {
                for (j1 = i4; j1--;) {
                  if (func0(data19[i4], data19[j1])) {
                    const err41 = {
                      instancePath:
                        instancePath + "/slots/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),
                      schemaPath: "#/properties/slots/additionalProperties/uniqueItems",
                      keyword: "uniqueItems",
                      params: { i: i4, j: j1 },
                      message:
                        "must NOT have duplicate items (items ## " +
                        j1 +
                        " and " +
                        i4 +
                        " are identical)",
                    };
                    if (vErrors === null) {
                      vErrors = [err41];
                    } else {
                      vErrors.push(err41);
                    }
                    errors++;
                    break outer1;
                  }
                }
              }
            }
          } else {
            const err42 = {
              instancePath:
                instancePath + "/slots/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),
              schemaPath: "#/properties/slots/additionalProperties/type",
              keyword: "type",
              params: { type: "array" },
              message: "must be array",
            };
            if (vErrors === null) {
              vErrors = [err42];
            } else {
              vErrors.push(err42);
            }
            errors++;
          }
        }
      } else {
        const err43 = {
          instancePath: instancePath + "/slots",
          schemaPath: "#/properties/slots/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err43];
        } else {
          vErrors.push(err43);
        }
        errors++;
      }
    }
    if (data.zone !== undefined) {
      let data21 = data.zone;
      const _errs62 = errors;
      let valid27 = false;
      let passing2 = null;
      const _errs63 = errors;
      if (data21 !== null) {
        const err44 = {
          instancePath: instancePath + "/zone",
          schemaPath: "#/properties/zone/oneOf/0/type",
          keyword: "type",
          params: { type: "null" },
          message: "must be null",
        };
        if (vErrors === null) {
          vErrors = [err44];
        } else {
          vErrors.push(err44);
        }
        errors++;
      }
      var _valid3 = _errs63 === errors;
      if (_valid3) {
        valid27 = true;
        passing2 = 0;
      }
      const _errs65 = errors;
      if (
        !validate62(data21, {
          instancePath: instancePath + "/zone",
          parentData: data,
          parentDataProperty: "zone",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate62.errors : vErrors.concat(validate62.errors);
        errors = vErrors.length;
      }
      var _valid3 = _errs65 === errors;
      if (_valid3 && valid27) {
        valid27 = false;
        passing2 = [passing2, 1];
      } else {
        if (_valid3) {
          valid27 = true;
          passing2 = 1;
        }
      }
      if (!valid27) {
        const err45 = {
          instancePath: instancePath + "/zone",
          schemaPath: "#/properties/zone/oneOf",
          keyword: "oneOf",
          params: { passingSchemas: passing2 },
          message: "must match exactly one schema in oneOf",
        };
        if (vErrors === null) {
          vErrors = [err45];
        } else {
          vErrors.push(err45);
        }
        errors++;
      } else {
        errors = _errs62;
        if (vErrors !== null) {
          if (_errs62) {
            vErrors.length = _errs62;
          } else {
            vErrors = null;
          }
        }
      }
    }
    if (data.locked !== undefined) {
      if (typeof data.locked !== "boolean") {
        const err46 = {
          instancePath: instancePath + "/locked",
          schemaPath: "#/properties/locked/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err46];
        } else {
          vErrors.push(err46);
        }
        errors++;
      }
    }
  } else {
    const err47 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err47];
    } else {
      vErrors.push(err47);
    }
    errors++;
  }
  validate21.errors = vErrors;
  evaluated0.props = props0;
  return errors === 0;
}
validate21.evaluated = { dynamicProps: true, dynamicItems: false };

const schema72 = {
  type: "object",
  properties: {
    mode: { $ref: "#/$defs/mode" },
    maxMode: { $ref: "#/$defs/mode" },
    allowedTypes: { type: "array", items: { type: "string" } },
    deniedTypes: { type: "array", items: { type: "string" } },
    allowRawColors: { type: "boolean" },
  },
  additionalProperties: false,
};

function validate65(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate65.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    for (const key0 in data) {
      if (!(
        key0 === "mode" ||
        key0 === "maxMode" ||
        key0 === "allowedTypes" ||
        key0 === "deniedTypes" ||
        key0 === "allowRawColors"
      )) {
        const err0 = {
          instancePath,
          schemaPath: "#/additionalProperties",
          keyword: "additionalProperties",
          params: { additionalProperty: key0 },
          message: "must NOT have additional properties",
        };
        if (vErrors === null) {
          vErrors = [err0];
        } else {
          vErrors.push(err0);
        }
        errors++;
      }
    }
    if (data.mode !== undefined) {
      let data0 = data.mode;
      if (!(data0 === "STRICT" || data0 === "SYSTEM" || data0 === "FREE" || data0 === "CODE")) {
        const err1 = {
          instancePath: instancePath + "/mode",
          schemaPath: "#/$defs/mode/enum",
          keyword: "enum",
          params: { allowedValues: schema70.enum },
          message: "must be equal to one of the allowed values",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    }
    if (data.maxMode !== undefined) {
      let data1 = data.maxMode;
      if (!(data1 === "STRICT" || data1 === "SYSTEM" || data1 === "FREE" || data1 === "CODE")) {
        const err2 = {
          instancePath: instancePath + "/maxMode",
          schemaPath: "#/$defs/mode/enum",
          keyword: "enum",
          params: { allowedValues: schema70.enum },
          message: "must be equal to one of the allowed values",
        };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    if (data.allowedTypes !== undefined) {
      let data2 = data.allowedTypes;
      if (Array.isArray(data2)) {
        const len0 = data2.length;
        for (let i0 = 0; i0 < len0; i0++) {
          if (typeof data2[i0] !== "string") {
            const err3 = {
              instancePath: instancePath + "/allowedTypes/" + i0,
              schemaPath: "#/properties/allowedTypes/items/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err3];
            } else {
              vErrors.push(err3);
            }
            errors++;
          }
        }
      } else {
        const err4 = {
          instancePath: instancePath + "/allowedTypes",
          schemaPath: "#/properties/allowedTypes/type",
          keyword: "type",
          params: { type: "array" },
          message: "must be array",
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
    if (data.deniedTypes !== undefined) {
      let data4 = data.deniedTypes;
      if (Array.isArray(data4)) {
        const len1 = data4.length;
        for (let i1 = 0; i1 < len1; i1++) {
          if (typeof data4[i1] !== "string") {
            const err5 = {
              instancePath: instancePath + "/deniedTypes/" + i1,
              schemaPath: "#/properties/deniedTypes/items/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err5];
            } else {
              vErrors.push(err5);
            }
            errors++;
          }
        }
      } else {
        const err6 = {
          instancePath: instancePath + "/deniedTypes",
          schemaPath: "#/properties/deniedTypes/type",
          keyword: "type",
          params: { type: "array" },
          message: "must be array",
        };
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
    if (data.allowRawColors !== undefined) {
      if (typeof data.allowRawColors !== "boolean") {
        const err7 = {
          instancePath: instancePath + "/allowRawColors",
          schemaPath: "#/properties/allowRawColors/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err7];
        } else {
          vErrors.push(err7);
        }
        errors++;
      }
    }
  } else {
    const err8 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err8];
    } else {
      vErrors.push(err8);
    }
    errors++;
  }
  validate65.errors = vErrors;
  return errors === 0;
}
validate65.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

const schema75 = {
  type: "object",
  required: ["schema", "resolve"],
  properties: {
    schema: { type: "string", pattern: "^[A-Z][A-Za-z0-9]{1,63}$" },
    resolve: {
      oneOf: [
        {
          type: "object",
          required: ["by", "field", "param"],
          properties: {
            by: { const: "routeParam" },
            field: { $ref: "#/$defs/identifier" },
            param: { $ref: "#/$defs/identifier" },
          },
          additionalProperties: false,
        },
        {
          type: "object",
          required: ["by", "entityId"],
          properties: { by: { const: "entity" }, entityId: { $ref: "#/$defs/uuid" } },
          additionalProperties: false,
        },
      ],
    },
  },
  additionalProperties: false,
};
const pattern30 = new RegExp("^[A-Z][A-Za-z0-9]{1,63}$", "u");

function validate67(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate67.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.schema === undefined) {
      const err0 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "schema" },
        message: "must have required property '" + "schema" + "'",
      };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.resolve === undefined) {
      const err1 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "resolve" },
        message: "must have required property '" + "resolve" + "'",
      };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "schema" || key0 === "resolve")) {
        const err2 = {
          instancePath,
          schemaPath: "#/additionalProperties",
          keyword: "additionalProperties",
          params: { additionalProperty: key0 },
          message: "must NOT have additional properties",
        };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    if (data.schema !== undefined) {
      let data0 = data.schema;
      if (typeof data0 === "string") {
        if (!pattern30.test(data0)) {
          const err3 = {
            instancePath: instancePath + "/schema",
            schemaPath: "#/properties/schema/pattern",
            keyword: "pattern",
            params: { pattern: "^[A-Z][A-Za-z0-9]{1,63}$" },
            message: 'must match pattern "' + "^[A-Z][A-Za-z0-9]{1,63}$" + '"',
          };
          if (vErrors === null) {
            vErrors = [err3];
          } else {
            vErrors.push(err3);
          }
          errors++;
        }
      } else {
        const err4 = {
          instancePath: instancePath + "/schema",
          schemaPath: "#/properties/schema/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
    if (data.resolve !== undefined) {
      let data1 = data.resolve;
      const _errs5 = errors;
      let valid1 = false;
      let passing0 = null;
      const _errs6 = errors;
      if (data1 && typeof data1 == "object" && !Array.isArray(data1)) {
        if (data1.by === undefined) {
          const err5 = {
            instancePath: instancePath + "/resolve",
            schemaPath: "#/properties/resolve/oneOf/0/required",
            keyword: "required",
            params: { missingProperty: "by" },
            message: "must have required property '" + "by" + "'",
          };
          if (vErrors === null) {
            vErrors = [err5];
          } else {
            vErrors.push(err5);
          }
          errors++;
        }
        if (data1.field === undefined) {
          const err6 = {
            instancePath: instancePath + "/resolve",
            schemaPath: "#/properties/resolve/oneOf/0/required",
            keyword: "required",
            params: { missingProperty: "field" },
            message: "must have required property '" + "field" + "'",
          };
          if (vErrors === null) {
            vErrors = [err6];
          } else {
            vErrors.push(err6);
          }
          errors++;
        }
        if (data1.param === undefined) {
          const err7 = {
            instancePath: instancePath + "/resolve",
            schemaPath: "#/properties/resolve/oneOf/0/required",
            keyword: "required",
            params: { missingProperty: "param" },
            message: "must have required property '" + "param" + "'",
          };
          if (vErrors === null) {
            vErrors = [err7];
          } else {
            vErrors.push(err7);
          }
          errors++;
        }
        for (const key1 in data1) {
          if (!(key1 === "by" || key1 === "field" || key1 === "param")) {
            const err8 = {
              instancePath: instancePath + "/resolve",
              schemaPath: "#/properties/resolve/oneOf/0/additionalProperties",
              keyword: "additionalProperties",
              params: { additionalProperty: key1 },
              message: "must NOT have additional properties",
            };
            if (vErrors === null) {
              vErrors = [err8];
            } else {
              vErrors.push(err8);
            }
            errors++;
          }
        }
        if (data1.by !== undefined) {
          if ("routeParam" !== data1.by) {
            const err9 = {
              instancePath: instancePath + "/resolve/by",
              schemaPath: "#/properties/resolve/oneOf/0/properties/by/const",
              keyword: "const",
              params: { allowedValue: "routeParam" },
              message: "must be equal to constant",
            };
            if (vErrors === null) {
              vErrors = [err9];
            } else {
              vErrors.push(err9);
            }
            errors++;
          }
        }
        if (data1.field !== undefined) {
          let data3 = data1.field;
          if (typeof data3 === "string") {
            if (!pattern10.test(data3)) {
              const err10 = {
                instancePath: instancePath + "/resolve/field",
                schemaPath: "#/$defs/identifier/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
              };
              if (vErrors === null) {
                vErrors = [err10];
              } else {
                vErrors.push(err10);
              }
              errors++;
            }
          } else {
            const err11 = {
              instancePath: instancePath + "/resolve/field",
              schemaPath: "#/$defs/identifier/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err11];
            } else {
              vErrors.push(err11);
            }
            errors++;
          }
        }
        if (data1.param !== undefined) {
          let data4 = data1.param;
          if (typeof data4 === "string") {
            if (!pattern10.test(data4)) {
              const err12 = {
                instancePath: instancePath + "/resolve/param",
                schemaPath: "#/$defs/identifier/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
              };
              if (vErrors === null) {
                vErrors = [err12];
              } else {
                vErrors.push(err12);
              }
              errors++;
            }
          } else {
            const err13 = {
              instancePath: instancePath + "/resolve/param",
              schemaPath: "#/$defs/identifier/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err13];
            } else {
              vErrors.push(err13);
            }
            errors++;
          }
        }
      } else {
        const err14 = {
          instancePath: instancePath + "/resolve",
          schemaPath: "#/properties/resolve/oneOf/0/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err14];
        } else {
          vErrors.push(err14);
        }
        errors++;
      }
      var _valid0 = _errs6 === errors;
      if (_valid0) {
        valid1 = true;
        passing0 = 0;
        var props0 = true;
      }
      const _errs16 = errors;
      if (data1 && typeof data1 == "object" && !Array.isArray(data1)) {
        if (data1.by === undefined) {
          const err15 = {
            instancePath: instancePath + "/resolve",
            schemaPath: "#/properties/resolve/oneOf/1/required",
            keyword: "required",
            params: { missingProperty: "by" },
            message: "must have required property '" + "by" + "'",
          };
          if (vErrors === null) {
            vErrors = [err15];
          } else {
            vErrors.push(err15);
          }
          errors++;
        }
        if (data1.entityId === undefined) {
          const err16 = {
            instancePath: instancePath + "/resolve",
            schemaPath: "#/properties/resolve/oneOf/1/required",
            keyword: "required",
            params: { missingProperty: "entityId" },
            message: "must have required property '" + "entityId" + "'",
          };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
        for (const key2 in data1) {
          if (!(key2 === "by" || key2 === "entityId")) {
            const err17 = {
              instancePath: instancePath + "/resolve",
              schemaPath: "#/properties/resolve/oneOf/1/additionalProperties",
              keyword: "additionalProperties",
              params: { additionalProperty: key2 },
              message: "must NOT have additional properties",
            };
            if (vErrors === null) {
              vErrors = [err17];
            } else {
              vErrors.push(err17);
            }
            errors++;
          }
        }
        if (data1.by !== undefined) {
          if ("entity" !== data1.by) {
            const err18 = {
              instancePath: instancePath + "/resolve/by",
              schemaPath: "#/properties/resolve/oneOf/1/properties/by/const",
              keyword: "const",
              params: { allowedValue: "entity" },
              message: "must be equal to constant",
            };
            if (vErrors === null) {
              vErrors = [err18];
            } else {
              vErrors.push(err18);
            }
            errors++;
          }
        }
        if (data1.entityId !== undefined) {
          let data6 = data1.entityId;
          if (typeof data6 === "string") {
            if (!pattern8.test(data6)) {
              const err19 = {
                instancePath: instancePath + "/resolve/entityId",
                schemaPath: "#/$defs/uuid/pattern",
                keyword: "pattern",
                params: {
                  pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
                },
                message:
                  'must match pattern "' +
                  "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" +
                  '"',
              };
              if (vErrors === null) {
                vErrors = [err19];
              } else {
                vErrors.push(err19);
              }
              errors++;
            }
          } else {
            const err20 = {
              instancePath: instancePath + "/resolve/entityId",
              schemaPath: "#/$defs/uuid/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err20];
            } else {
              vErrors.push(err20);
            }
            errors++;
          }
        }
      } else {
        const err21 = {
          instancePath: instancePath + "/resolve",
          schemaPath: "#/properties/resolve/oneOf/1/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err21];
        } else {
          vErrors.push(err21);
        }
        errors++;
      }
      var _valid0 = _errs16 === errors;
      if (_valid0 && valid1) {
        valid1 = false;
        passing0 = [passing0, 1];
      } else {
        if (_valid0) {
          valid1 = true;
          passing0 = 1;
          if (props0 !== true) {
            props0 = true;
          }
        }
      }
      if (!valid1) {
        const err22 = {
          instancePath: instancePath + "/resolve",
          schemaPath: "#/properties/resolve/oneOf",
          keyword: "oneOf",
          params: { passingSchemas: passing0 },
          message: "must match exactly one schema in oneOf",
        };
        if (vErrors === null) {
          vErrors = [err22];
        } else {
          vErrors.push(err22);
        }
        errors++;
      } else {
        errors = _errs5;
        if (vErrors !== null) {
          if (_errs5) {
            vErrors.length = _errs5;
          } else {
            vErrors = null;
          }
        }
      }
    }
  } else {
    const err23 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err23];
    } else {
      vErrors.push(err23);
    }
    errors++;
  }
  validate67.errors = vErrors;
  return errors === 0;
}
validate67.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

const schema80 = {
  type: "object",
  required: ["type"],
  properties: {
    type: {
      enum: [
        "string",
        "text",
        "richText",
        "number",
        "boolean",
        "enum",
        "date",
        "datetime",
        "url",
        "link",
        "asset",
        "reference",
        "list",
        "object",
        "color",
        "nodeRef",
      ],
    },
    required: { type: "boolean" },
    content: { type: "boolean" },
    responsive: { type: "boolean" },
    localized: { type: "boolean" },
    default: {},
    description: { type: "string" },
    values: { type: "array", items: { type: "string" }, minItems: 1 },
    schema: { type: "string" },
    assetKind: { enum: ["image", "video", "file"] },
    of: { $ref: "#/$defs/typeDef" },
    fields: { type: "object", additionalProperties: { $ref: "#/$defs/typeDef" } },
    min: { type: "number" },
    max: { type: "number" },
    integer: { type: "boolean" },
    minLength: { type: "integer" },
    maxLength: { type: "integer" },
    pattern: { type: "string" },
    multiline: { type: "boolean" },
  },
  allOf: [
    { if: { properties: { type: { const: "enum" } } }, then: { required: ["values"] } },
    { if: { properties: { type: { const: "reference" } } }, then: { required: ["schema"] } },
    { if: { properties: { type: { const: "list" } } }, then: { required: ["of"] } },
  ],
};
const wrapper3 = { validate: validate69 };

function validate69(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate69.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs2 = errors;
  let valid1 = true;
  const _errs3 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.type !== undefined) {
      if ("enum" !== data.type) {
        const err0 = {};
        if (vErrors === null) {
          vErrors = [err0];
        } else {
          vErrors.push(err0);
        }
        errors++;
      }
    }
  }
  var _valid0 = _errs3 === errors;
  errors = _errs2;
  if (vErrors !== null) {
    if (_errs2) {
      vErrors.length = _errs2;
    } else {
      vErrors = null;
    }
  }
  if (_valid0) {
    const _errs5 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.values === undefined) {
        const err1 = {
          instancePath,
          schemaPath: "#/allOf/0/then/required",
          keyword: "required",
          params: { missingProperty: "values" },
          message: "must have required property '" + "values" + "'",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    }
    var _valid0 = _errs5 === errors;
    valid1 = _valid0;
  }
  if (!valid1) {
    const err2 = {
      instancePath,
      schemaPath: "#/allOf/0/if",
      keyword: "if",
      params: { failingKeyword: "then" },
      message: 'must match "then" schema',
    };
    if (vErrors === null) {
      vErrors = [err2];
    } else {
      vErrors.push(err2);
    }
    errors++;
  }
  const _errs7 = errors;
  let valid3 = true;
  const _errs8 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.type !== undefined) {
      if ("reference" !== data.type) {
        const err3 = {};
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
  }
  var _valid1 = _errs8 === errors;
  errors = _errs7;
  if (vErrors !== null) {
    if (_errs7) {
      vErrors.length = _errs7;
    } else {
      vErrors = null;
    }
  }
  if (_valid1) {
    const _errs10 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.schema === undefined) {
        const err4 = {
          instancePath,
          schemaPath: "#/allOf/1/then/required",
          keyword: "required",
          params: { missingProperty: "schema" },
          message: "must have required property '" + "schema" + "'",
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
    var _valid1 = _errs10 === errors;
    valid3 = _valid1;
  }
  if (!valid3) {
    const err5 = {
      instancePath,
      schemaPath: "#/allOf/1/if",
      keyword: "if",
      params: { failingKeyword: "then" },
      message: 'must match "then" schema',
    };
    if (vErrors === null) {
      vErrors = [err5];
    } else {
      vErrors.push(err5);
    }
    errors++;
  }
  const _errs12 = errors;
  let valid5 = true;
  const _errs13 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.type !== undefined) {
      if ("list" !== data.type) {
        const err6 = {};
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
  }
  var _valid2 = _errs13 === errors;
  errors = _errs12;
  if (vErrors !== null) {
    if (_errs12) {
      vErrors.length = _errs12;
    } else {
      vErrors = null;
    }
  }
  if (_valid2) {
    const _errs15 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.of === undefined) {
        const err7 = {
          instancePath,
          schemaPath: "#/allOf/2/then/required",
          keyword: "required",
          params: { missingProperty: "of" },
          message: "must have required property '" + "of" + "'",
        };
        if (vErrors === null) {
          vErrors = [err7];
        } else {
          vErrors.push(err7);
        }
        errors++;
      }
    }
    var _valid2 = _errs15 === errors;
    valid5 = _valid2;
  }
  if (!valid5) {
    const err8 = {
      instancePath,
      schemaPath: "#/allOf/2/if",
      keyword: "if",
      params: { failingKeyword: "then" },
      message: 'must match "then" schema',
    };
    if (vErrors === null) {
      vErrors = [err8];
    } else {
      vErrors.push(err8);
    }
    errors++;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.type === undefined) {
      const err9 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "type" },
        message: "must have required property '" + "type" + "'",
      };
      if (vErrors === null) {
        vErrors = [err9];
      } else {
        vErrors.push(err9);
      }
      errors++;
    }
    if (data.type !== undefined) {
      let data3 = data.type;
      if (!(
        data3 === "string" ||
        data3 === "text" ||
        data3 === "richText" ||
        data3 === "number" ||
        data3 === "boolean" ||
        data3 === "enum" ||
        data3 === "date" ||
        data3 === "datetime" ||
        data3 === "url" ||
        data3 === "link" ||
        data3 === "asset" ||
        data3 === "reference" ||
        data3 === "list" ||
        data3 === "object" ||
        data3 === "color" ||
        data3 === "nodeRef"
      )) {
        const err10 = {
          instancePath: instancePath + "/type",
          schemaPath: "#/properties/type/enum",
          keyword: "enum",
          params: { allowedValues: schema80.properties.type.enum },
          message: "must be equal to one of the allowed values",
        };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
    }
    if (data.required !== undefined) {
      if (typeof data.required !== "boolean") {
        const err11 = {
          instancePath: instancePath + "/required",
          schemaPath: "#/properties/required/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err11];
        } else {
          vErrors.push(err11);
        }
        errors++;
      }
    }
    if (data.content !== undefined) {
      if (typeof data.content !== "boolean") {
        const err12 = {
          instancePath: instancePath + "/content",
          schemaPath: "#/properties/content/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err12];
        } else {
          vErrors.push(err12);
        }
        errors++;
      }
    }
    if (data.responsive !== undefined) {
      if (typeof data.responsive !== "boolean") {
        const err13 = {
          instancePath: instancePath + "/responsive",
          schemaPath: "#/properties/responsive/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err13];
        } else {
          vErrors.push(err13);
        }
        errors++;
      }
    }
    if (data.localized !== undefined) {
      if (typeof data.localized !== "boolean") {
        const err14 = {
          instancePath: instancePath + "/localized",
          schemaPath: "#/properties/localized/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err14];
        } else {
          vErrors.push(err14);
        }
        errors++;
      }
    }
    if (data.description !== undefined) {
      if (typeof data.description !== "string") {
        const err15 = {
          instancePath: instancePath + "/description",
          schemaPath: "#/properties/description/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err15];
        } else {
          vErrors.push(err15);
        }
        errors++;
      }
    }
    if (data.values !== undefined) {
      let data9 = data.values;
      if (Array.isArray(data9)) {
        if (data9.length < 1) {
          const err16 = {
            instancePath: instancePath + "/values",
            schemaPath: "#/properties/values/minItems",
            keyword: "minItems",
            params: { limit: 1 },
            message: "must NOT have fewer than 1 items",
          };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
        const len0 = data9.length;
        for (let i0 = 0; i0 < len0; i0++) {
          if (typeof data9[i0] !== "string") {
            const err17 = {
              instancePath: instancePath + "/values/" + i0,
              schemaPath: "#/properties/values/items/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err17];
            } else {
              vErrors.push(err17);
            }
            errors++;
          }
        }
      } else {
        const err18 = {
          instancePath: instancePath + "/values",
          schemaPath: "#/properties/values/type",
          keyword: "type",
          params: { type: "array" },
          message: "must be array",
        };
        if (vErrors === null) {
          vErrors = [err18];
        } else {
          vErrors.push(err18);
        }
        errors++;
      }
    }
    if (data.schema !== undefined) {
      if (typeof data.schema !== "string") {
        const err19 = {
          instancePath: instancePath + "/schema",
          schemaPath: "#/properties/schema/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err19];
        } else {
          vErrors.push(err19);
        }
        errors++;
      }
    }
    if (data.assetKind !== undefined) {
      let data12 = data.assetKind;
      if (!(data12 === "image" || data12 === "video" || data12 === "file")) {
        const err20 = {
          instancePath: instancePath + "/assetKind",
          schemaPath: "#/properties/assetKind/enum",
          keyword: "enum",
          params: { allowedValues: schema80.properties.assetKind.enum },
          message: "must be equal to one of the allowed values",
        };
        if (vErrors === null) {
          vErrors = [err20];
        } else {
          vErrors.push(err20);
        }
        errors++;
      }
    }
    if (data.of !== undefined) {
      if (
        !wrapper3.validate(data.of, {
          instancePath: instancePath + "/of",
          parentData: data,
          parentDataProperty: "of",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors =
          vErrors === null ? wrapper3.validate.errors : vErrors.concat(wrapper3.validate.errors);
        errors = vErrors.length;
      }
    }
    if (data.fields !== undefined) {
      let data14 = data.fields;
      if (data14 && typeof data14 == "object" && !Array.isArray(data14)) {
        for (const key0 in data14) {
          if (
            !wrapper3.validate(data14[key0], {
              instancePath:
                instancePath + "/fields/" + key0.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data14,
              parentDataProperty: key0,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors =
              vErrors === null
                ? wrapper3.validate.errors
                : vErrors.concat(wrapper3.validate.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err21 = {
          instancePath: instancePath + "/fields",
          schemaPath: "#/properties/fields/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err21];
        } else {
          vErrors.push(err21);
        }
        errors++;
      }
    }
    if (data.min !== undefined) {
      let data16 = data.min;
      if (!(typeof data16 == "number" && isFinite(data16))) {
        const err22 = {
          instancePath: instancePath + "/min",
          schemaPath: "#/properties/min/type",
          keyword: "type",
          params: { type: "number" },
          message: "must be number",
        };
        if (vErrors === null) {
          vErrors = [err22];
        } else {
          vErrors.push(err22);
        }
        errors++;
      }
    }
    if (data.max !== undefined) {
      let data17 = data.max;
      if (!(typeof data17 == "number" && isFinite(data17))) {
        const err23 = {
          instancePath: instancePath + "/max",
          schemaPath: "#/properties/max/type",
          keyword: "type",
          params: { type: "number" },
          message: "must be number",
        };
        if (vErrors === null) {
          vErrors = [err23];
        } else {
          vErrors.push(err23);
        }
        errors++;
      }
    }
    if (data.integer !== undefined) {
      if (typeof data.integer !== "boolean") {
        const err24 = {
          instancePath: instancePath + "/integer",
          schemaPath: "#/properties/integer/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err24];
        } else {
          vErrors.push(err24);
        }
        errors++;
      }
    }
    if (data.minLength !== undefined) {
      let data19 = data.minLength;
      if (!(typeof data19 == "number" && !(data19 % 1) && !isNaN(data19) && isFinite(data19))) {
        const err25 = {
          instancePath: instancePath + "/minLength",
          schemaPath: "#/properties/minLength/type",
          keyword: "type",
          params: { type: "integer" },
          message: "must be integer",
        };
        if (vErrors === null) {
          vErrors = [err25];
        } else {
          vErrors.push(err25);
        }
        errors++;
      }
    }
    if (data.maxLength !== undefined) {
      let data20 = data.maxLength;
      if (!(typeof data20 == "number" && !(data20 % 1) && !isNaN(data20) && isFinite(data20))) {
        const err26 = {
          instancePath: instancePath + "/maxLength",
          schemaPath: "#/properties/maxLength/type",
          keyword: "type",
          params: { type: "integer" },
          message: "must be integer",
        };
        if (vErrors === null) {
          vErrors = [err26];
        } else {
          vErrors.push(err26);
        }
        errors++;
      }
    }
    if (data.pattern !== undefined) {
      if (typeof data.pattern !== "string") {
        const err27 = {
          instancePath: instancePath + "/pattern",
          schemaPath: "#/properties/pattern/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err27];
        } else {
          vErrors.push(err27);
        }
        errors++;
      }
    }
    if (data.multiline !== undefined) {
      if (typeof data.multiline !== "boolean") {
        const err28 = {
          instancePath: instancePath + "/multiline",
          schemaPath: "#/properties/multiline/type",
          keyword: "type",
          params: { type: "boolean" },
          message: "must be boolean",
        };
        if (vErrors === null) {
          vErrors = [err28];
        } else {
          vErrors.push(err28);
        }
        errors++;
      }
    }
  } else {
    const err29 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err29];
    } else {
      vErrors.push(err29);
    }
    errors++;
  }
  validate69.errors = vErrors;
  return errors === 0;
}
validate69.evaluated = {
  props: {
    type: true,
    required: true,
    content: true,
    responsive: true,
    localized: true,
    default: true,
    description: true,
    values: true,
    schema: true,
    assetKind: true,
    of: true,
    fields: true,
    min: true,
    max: true,
    integer: true,
    minLength: true,
    maxLength: true,
    pattern: true,
    multiline: true,
  },
  dynamicProps: false,
  dynamicItems: false,
};

const schema84 = {
  type: "object",
  required: ["source"],
  properties: {
    source: { type: "string", pattern: "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)+$" },
    params: { type: "object", additionalProperties: { $ref: "#/$defs/operand" } },
  },
  additionalProperties: false,
};
const pattern37 = new RegExp("^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)+$", "u");

function validate71(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate71.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.source === undefined) {
      const err0 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "source" },
        message: "must have required property '" + "source" + "'",
      };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "source" || key0 === "params")) {
        const err1 = {
          instancePath,
          schemaPath: "#/additionalProperties",
          keyword: "additionalProperties",
          params: { additionalProperty: key0 },
          message: "must NOT have additional properties",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    }
    if (data.source !== undefined) {
      let data0 = data.source;
      if (typeof data0 === "string") {
        if (!pattern37.test(data0)) {
          const err2 = {
            instancePath: instancePath + "/source",
            schemaPath: "#/properties/source/pattern",
            keyword: "pattern",
            params: { pattern: "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)+$" },
            message: 'must match pattern "' + "^[a-z][A-Za-z0-9]*(\\.[a-z][A-Za-z0-9]*)+$" + '"',
          };
          if (vErrors === null) {
            vErrors = [err2];
          } else {
            vErrors.push(err2);
          }
          errors++;
        }
      } else {
        const err3 = {
          instancePath: instancePath + "/source",
          schemaPath: "#/properties/source/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    if (data.params !== undefined) {
      let data1 = data.params;
      if (data1 && typeof data1 == "object" && !Array.isArray(data1)) {
        for (const key1 in data1) {
          if (
            !validate38(data1[key1], {
              instancePath:
                instancePath + "/params/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data1,
              parentDataProperty: key1,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate38.errors : vErrors.concat(validate38.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err4 = {
          instancePath: instancePath + "/params",
          schemaPath: "#/properties/params/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
  } else {
    const err5 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err5];
    } else {
      vErrors.push(err5);
    }
    errors++;
  }
  validate71.errors = vErrors;
  return errors === 0;
}
validate71.evaluated = { props: true, dynamicProps: false, dynamicItems: false };

function validate20(
  data,
  { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {},
) {
  /*# sourceURL="https://cms.dev/schemas/ir/1.0/document.json" */ let vErrors = null;
  let errors = 0;
  const evaluated0 = validate20.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = undefined;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = undefined;
  }
  const _errs2 = errors;
  let valid1 = true;
  const _errs3 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.kind !== undefined) {
      if ("page" !== data.kind) {
        const err0 = {};
        if (vErrors === null) {
          vErrors = [err0];
        } else {
          vErrors.push(err0);
        }
        errors++;
      }
    }
  }
  var _valid0 = _errs3 === errors;
  errors = _errs2;
  if (vErrors !== null) {
    if (_errs2) {
      vErrors.length = _errs2;
    } else {
      vErrors = null;
    }
  }
  if (_valid0) {
    const _errs5 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.inputs !== undefined) {
        const err1 = {
          instancePath: instancePath + "/inputs",
          schemaPath: "#/allOf/0/then/properties/inputs/false schema",
          keyword: "false schema",
          params: {},
          message: "boolean schema is false",
        };
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
      if (data.slotDefs !== undefined) {
        const err2 = {
          instancePath: instancePath + "/slotDefs",
          schemaPath: "#/allOf/0/then/properties/slotDefs/false schema",
          keyword: "false schema",
          params: {},
          message: "boolean schema is false",
        };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    var _valid0 = _errs5 === errors;
    valid1 = _valid0;
    if (valid1) {
      var props0 = {};
      props0.inputs = true;
      props0.slotDefs = true;
      props0.kind = true;
    }
  }
  if (!valid1) {
    const err3 = {
      instancePath,
      schemaPath: "#/allOf/0/if",
      keyword: "if",
      params: { failingKeyword: "then" },
      message: 'must match "then" schema',
    };
    if (vErrors === null) {
      vErrors = [err3];
    } else {
      vErrors.push(err3);
    }
    errors++;
  }
  const _errs7 = errors;
  let valid4 = true;
  const _errs8 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.kind !== undefined) {
      if ("component" !== data.kind) {
        const err4 = {};
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
  }
  var _valid1 = _errs8 === errors;
  errors = _errs7;
  if (vErrors !== null) {
    if (_errs7) {
      vErrors.length = _errs7;
    } else {
      vErrors = null;
    }
  }
  if (_valid1) {
    const _errs10 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.content !== undefined) {
        const err5 = {
          instancePath: instancePath + "/content",
          schemaPath: "#/allOf/1/then/properties/content/false schema",
          keyword: "false schema",
          params: {},
          message: "boolean schema is false",
        };
        if (vErrors === null) {
          vErrors = [err5];
        } else {
          vErrors.push(err5);
        }
        errors++;
      }
    }
    var _valid1 = _errs10 === errors;
    valid4 = _valid1;
    if (valid4) {
      var props1 = {};
      props1.content = true;
      props1.kind = true;
    }
  }
  if (!valid4) {
    const err6 = {
      instancePath,
      schemaPath: "#/allOf/1/if",
      keyword: "if",
      params: { failingKeyword: "then" },
      message: 'must match "then" schema',
    };
    if (vErrors === null) {
      vErrors = [err6];
    } else {
      vErrors.push(err6);
    }
    errors++;
  }
  if (props0 !== true && props1 !== undefined) {
    if (props1 === true) {
      props0 = true;
    } else {
      props0 = props0 || {};
      Object.assign(props0, props1);
    }
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.irVersion === undefined) {
      const err7 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "irVersion" },
        message: "must have required property '" + "irVersion" + "'",
      };
      if (vErrors === null) {
        vErrors = [err7];
      } else {
        vErrors.push(err7);
      }
      errors++;
    }
    if (data.kind === undefined) {
      const err8 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "kind" },
        message: "must have required property '" + "kind" + "'",
      };
      if (vErrors === null) {
        vErrors = [err8];
      } else {
        vErrors.push(err8);
      }
      errors++;
    }
    if (data.root === undefined) {
      const err9 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "root" },
        message: "must have required property '" + "root" + "'",
      };
      if (vErrors === null) {
        vErrors = [err9];
      } else {
        vErrors.push(err9);
      }
      errors++;
    }
    if (data.nodes === undefined) {
      const err10 = {
        instancePath,
        schemaPath: "#/required",
        keyword: "required",
        params: { missingProperty: "nodes" },
        message: "must have required property '" + "nodes" + "'",
      };
      if (vErrors === null) {
        vErrors = [err10];
      } else {
        vErrors.push(err10);
      }
      errors++;
    }
    if (props0 !== true) {
      props0 = props0 || {};
      props0.irVersion = true;
      props0.kind = true;
      props0.root = true;
      props0.nodes = true;
      props0.policy = true;
      props0.content = true;
      props0.inputs = true;
      props0.slotDefs = true;
      props0.dataSources = true;
      props0.localContent = true;
      props0.meta = true;
    }
    if (data.irVersion !== undefined) {
      if ("1.0" !== data.irVersion) {
        const err11 = {
          instancePath: instancePath + "/irVersion",
          schemaPath: "#/properties/irVersion/const",
          keyword: "const",
          params: { allowedValue: "1.0" },
          message: "must be equal to constant",
        };
        if (vErrors === null) {
          vErrors = [err11];
        } else {
          vErrors.push(err11);
        }
        errors++;
      }
    }
    if (data.kind !== undefined) {
      let data6 = data.kind;
      if (!(data6 === "page" || data6 === "component")) {
        const err12 = {
          instancePath: instancePath + "/kind",
          schemaPath: "#/properties/kind/enum",
          keyword: "enum",
          params: { allowedValues: schema31.properties.kind.enum },
          message: "must be equal to one of the allowed values",
        };
        if (vErrors === null) {
          vErrors = [err12];
        } else {
          vErrors.push(err12);
        }
        errors++;
      }
    }
    if (data.root !== undefined) {
      let data7 = data.root;
      if (typeof data7 === "string") {
        if (!pattern4.test(data7)) {
          const err13 = {
            instancePath: instancePath + "/root",
            schemaPath: "#/$defs/nodeId/pattern",
            keyword: "pattern",
            params: { pattern: "^[A-Za-z0-9_-]{4,32}$" },
            message: 'must match pattern "' + "^[A-Za-z0-9_-]{4,32}$" + '"',
          };
          if (vErrors === null) {
            vErrors = [err13];
          } else {
            vErrors.push(err13);
          }
          errors++;
        }
      } else {
        const err14 = {
          instancePath: instancePath + "/root",
          schemaPath: "#/$defs/nodeId/type",
          keyword: "type",
          params: { type: "string" },
          message: "must be string",
        };
        if (vErrors === null) {
          vErrors = [err14];
        } else {
          vErrors.push(err14);
        }
        errors++;
      }
    }
    if (data.nodes !== undefined) {
      let data8 = data.nodes;
      if (data8 && typeof data8 == "object" && !Array.isArray(data8)) {
        if (Object.keys(data8).length > 5000) {
          const err15 = {
            instancePath: instancePath + "/nodes",
            schemaPath: "#/properties/nodes/maxProperties",
            keyword: "maxProperties",
            params: { limit: 5000 },
            message: "must NOT have more than 5000 properties",
          };
          if (vErrors === null) {
            vErrors = [err15];
          } else {
            vErrors.push(err15);
          }
          errors++;
        }
        if (Object.keys(data8).length < 1) {
          const err16 = {
            instancePath: instancePath + "/nodes",
            schemaPath: "#/properties/nodes/minProperties",
            keyword: "minProperties",
            params: { limit: 1 },
            message: "must NOT have fewer than 1 properties",
          };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
        for (const key0 in data8) {
          const _errs18 = errors;
          if (typeof key0 === "string") {
            if (!pattern4.test(key0)) {
              const err17 = {
                instancePath: instancePath + "/nodes",
                schemaPath: "#/$defs/nodeId/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z0-9_-]{4,32}$" },
                message: 'must match pattern "' + "^[A-Za-z0-9_-]{4,32}$" + '"',
                propertyName: key0,
              };
              if (vErrors === null) {
                vErrors = [err17];
              } else {
                vErrors.push(err17);
              }
              errors++;
            }
          } else {
            const err18 = {
              instancePath: instancePath + "/nodes",
              schemaPath: "#/$defs/nodeId/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
              propertyName: key0,
            };
            if (vErrors === null) {
              vErrors = [err18];
            } else {
              vErrors.push(err18);
            }
            errors++;
          }
          var valid9 = _errs18 === errors;
          if (!valid9) {
            const err19 = {
              instancePath: instancePath + "/nodes",
              schemaPath: "#/properties/nodes/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key0 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err19];
            } else {
              vErrors.push(err19);
            }
            errors++;
          }
        }
        for (const key1 in data8) {
          if (
            !validate21(data8[key1], {
              instancePath:
                instancePath + "/nodes/" + key1.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data8,
              parentDataProperty: key1,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate21.errors : vErrors.concat(validate21.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err20 = {
          instancePath: instancePath + "/nodes",
          schemaPath: "#/properties/nodes/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err20];
        } else {
          vErrors.push(err20);
        }
        errors++;
      }
    }
    if (data.policy !== undefined) {
      if (
        !validate65(data.policy, {
          instancePath: instancePath + "/policy",
          parentData: data,
          parentDataProperty: "policy",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate65.errors : vErrors.concat(validate65.errors);
        errors = vErrors.length;
      }
    }
    if (data.content !== undefined) {
      if (
        !validate67(data.content, {
          instancePath: instancePath + "/content",
          parentData: data,
          parentDataProperty: "content",
          rootData,
          dynamicAnchors,
        })
      ) {
        vErrors = vErrors === null ? validate67.errors : vErrors.concat(validate67.errors);
        errors = vErrors.length;
      }
    }
    if (data.inputs !== undefined) {
      let data12 = data.inputs;
      if (data12 && typeof data12 == "object" && !Array.isArray(data12)) {
        for (const key2 in data12) {
          const _errs27 = errors;
          if (typeof key2 === "string") {
            if (!pattern10.test(key2)) {
              const err21 = {
                instancePath: instancePath + "/inputs",
                schemaPath: "#/$defs/identifier/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
                propertyName: key2,
              };
              if (vErrors === null) {
                vErrors = [err21];
              } else {
                vErrors.push(err21);
              }
              errors++;
            }
          } else {
            const err22 = {
              instancePath: instancePath + "/inputs",
              schemaPath: "#/$defs/identifier/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
              propertyName: key2,
            };
            if (vErrors === null) {
              vErrors = [err22];
            } else {
              vErrors.push(err22);
            }
            errors++;
          }
          var valid12 = _errs27 === errors;
          if (!valid12) {
            const err23 = {
              instancePath: instancePath + "/inputs",
              schemaPath: "#/properties/inputs/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key2 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err23];
            } else {
              vErrors.push(err23);
            }
            errors++;
          }
        }
        for (const key3 in data12) {
          if (
            !validate69(data12[key3], {
              instancePath:
                instancePath + "/inputs/" + key3.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data12,
              parentDataProperty: key3,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate69.errors : vErrors.concat(validate69.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err24 = {
          instancePath: instancePath + "/inputs",
          schemaPath: "#/properties/inputs/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err24];
        } else {
          vErrors.push(err24);
        }
        errors++;
      }
    }
    if (data.slotDefs !== undefined) {
      let data14 = data.slotDefs;
      if (data14 && typeof data14 == "object" && !Array.isArray(data14)) {
        for (const key4 in data14) {
          const _errs34 = errors;
          if (typeof key4 === "string") {
            if (!pattern10.test(key4)) {
              const err25 = {
                instancePath: instancePath + "/slotDefs",
                schemaPath: "#/$defs/identifier/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
                propertyName: key4,
              };
              if (vErrors === null) {
                vErrors = [err25];
              } else {
                vErrors.push(err25);
              }
              errors++;
            }
          } else {
            const err26 = {
              instancePath: instancePath + "/slotDefs",
              schemaPath: "#/$defs/identifier/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
              propertyName: key4,
            };
            if (vErrors === null) {
              vErrors = [err26];
            } else {
              vErrors.push(err26);
            }
            errors++;
          }
          var valid15 = _errs34 === errors;
          if (!valid15) {
            const err27 = {
              instancePath: instancePath + "/slotDefs",
              schemaPath: "#/properties/slotDefs/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key4 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err27];
            } else {
              vErrors.push(err27);
            }
            errors++;
          }
        }
        for (const key5 in data14) {
          let data15 = data14[key5];
          if (data15 && typeof data15 == "object" && !Array.isArray(data15)) {
            for (const key6 in data15) {
              if (!(key6 === "allowedTypes" || key6 === "min" || key6 === "max")) {
                const err28 = {
                  instancePath:
                    instancePath + "/slotDefs/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),
                  schemaPath: "#/$defs/slotDef/additionalProperties",
                  keyword: "additionalProperties",
                  params: { additionalProperty: key6 },
                  message: "must NOT have additional properties",
                };
                if (vErrors === null) {
                  vErrors = [err28];
                } else {
                  vErrors.push(err28);
                }
                errors++;
              }
            }
            if (data15.allowedTypes !== undefined) {
              let data16 = data15.allowedTypes;
              if (Array.isArray(data16)) {
                const len0 = data16.length;
                for (let i0 = 0; i0 < len0; i0++) {
                  if (typeof data16[i0] !== "string") {
                    const err29 = {
                      instancePath:
                        instancePath +
                        "/slotDefs/" +
                        key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                        "/allowedTypes/" +
                        i0,
                      schemaPath: "#/$defs/slotDef/properties/allowedTypes/items/type",
                      keyword: "type",
                      params: { type: "string" },
                      message: "must be string",
                    };
                    if (vErrors === null) {
                      vErrors = [err29];
                    } else {
                      vErrors.push(err29);
                    }
                    errors++;
                  }
                }
              } else {
                const err30 = {
                  instancePath:
                    instancePath +
                    "/slotDefs/" +
                    key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                    "/allowedTypes",
                  schemaPath: "#/$defs/slotDef/properties/allowedTypes/type",
                  keyword: "type",
                  params: { type: "array" },
                  message: "must be array",
                };
                if (vErrors === null) {
                  vErrors = [err30];
                } else {
                  vErrors.push(err30);
                }
                errors++;
              }
            }
            if (data15.min !== undefined) {
              let data18 = data15.min;
              if (!(
                typeof data18 == "number" &&
                !(data18 % 1) &&
                !isNaN(data18) &&
                isFinite(data18)
              )) {
                const err31 = {
                  instancePath:
                    instancePath +
                    "/slotDefs/" +
                    key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                    "/min",
                  schemaPath: "#/$defs/slotDef/properties/min/type",
                  keyword: "type",
                  params: { type: "integer" },
                  message: "must be integer",
                };
                if (vErrors === null) {
                  vErrors = [err31];
                } else {
                  vErrors.push(err31);
                }
                errors++;
              }
              if (typeof data18 == "number" && isFinite(data18)) {
                if (data18 < 0 || isNaN(data18)) {
                  const err32 = {
                    instancePath:
                      instancePath +
                      "/slotDefs/" +
                      key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                      "/min",
                    schemaPath: "#/$defs/slotDef/properties/min/minimum",
                    keyword: "minimum",
                    params: { comparison: ">=", limit: 0 },
                    message: "must be >= 0",
                  };
                  if (vErrors === null) {
                    vErrors = [err32];
                  } else {
                    vErrors.push(err32);
                  }
                  errors++;
                }
              }
            }
            if (data15.max !== undefined) {
              let data19 = data15.max;
              if (!(
                typeof data19 == "number" &&
                !(data19 % 1) &&
                !isNaN(data19) &&
                isFinite(data19)
              )) {
                const err33 = {
                  instancePath:
                    instancePath +
                    "/slotDefs/" +
                    key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                    "/max",
                  schemaPath: "#/$defs/slotDef/properties/max/type",
                  keyword: "type",
                  params: { type: "integer" },
                  message: "must be integer",
                };
                if (vErrors === null) {
                  vErrors = [err33];
                } else {
                  vErrors.push(err33);
                }
                errors++;
              }
              if (typeof data19 == "number" && isFinite(data19)) {
                if (data19 < 1 || isNaN(data19)) {
                  const err34 = {
                    instancePath:
                      instancePath +
                      "/slotDefs/" +
                      key5.replace(/~/g, "~0").replace(/\//g, "~1") +
                      "/max",
                    schemaPath: "#/$defs/slotDef/properties/max/minimum",
                    keyword: "minimum",
                    params: { comparison: ">=", limit: 1 },
                    message: "must be >= 1",
                  };
                  if (vErrors === null) {
                    vErrors = [err34];
                  } else {
                    vErrors.push(err34);
                  }
                  errors++;
                }
              }
            }
          } else {
            const err35 = {
              instancePath:
                instancePath + "/slotDefs/" + key5.replace(/~/g, "~0").replace(/\//g, "~1"),
              schemaPath: "#/$defs/slotDef/type",
              keyword: "type",
              params: { type: "object" },
              message: "must be object",
            };
            if (vErrors === null) {
              vErrors = [err35];
            } else {
              vErrors.push(err35);
            }
            errors++;
          }
        }
      } else {
        const err36 = {
          instancePath: instancePath + "/slotDefs",
          schemaPath: "#/properties/slotDefs/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err36];
        } else {
          vErrors.push(err36);
        }
        errors++;
      }
    }
    if (data.dataSources !== undefined) {
      let data20 = data.dataSources;
      if (data20 && typeof data20 == "object" && !Array.isArray(data20)) {
        for (const key7 in data20) {
          const _errs52 = errors;
          if (typeof key7 === "string") {
            if (!pattern10.test(key7)) {
              const err37 = {
                instancePath: instancePath + "/dataSources",
                schemaPath: "#/$defs/identifier/pattern",
                keyword: "pattern",
                params: { pattern: "^[A-Za-z][A-Za-z0-9_]*$" },
                message: 'must match pattern "' + "^[A-Za-z][A-Za-z0-9_]*$" + '"',
                propertyName: key7,
              };
              if (vErrors === null) {
                vErrors = [err37];
              } else {
                vErrors.push(err37);
              }
              errors++;
            }
          } else {
            const err38 = {
              instancePath: instancePath + "/dataSources",
              schemaPath: "#/$defs/identifier/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
              propertyName: key7,
            };
            if (vErrors === null) {
              vErrors = [err38];
            } else {
              vErrors.push(err38);
            }
            errors++;
          }
          var valid22 = _errs52 === errors;
          if (!valid22) {
            const err39 = {
              instancePath: instancePath + "/dataSources",
              schemaPath: "#/properties/dataSources/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key7 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err39];
            } else {
              vErrors.push(err39);
            }
            errors++;
          }
        }
        for (const key8 in data20) {
          if (
            !validate71(data20[key8], {
              instancePath:
                instancePath + "/dataSources/" + key8.replace(/~/g, "~0").replace(/\//g, "~1"),
              parentData: data20,
              parentDataProperty: key8,
              rootData,
              dynamicAnchors,
            })
          ) {
            vErrors = vErrors === null ? validate71.errors : vErrors.concat(validate71.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err40 = {
          instancePath: instancePath + "/dataSources",
          schemaPath: "#/properties/dataSources/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err40];
        } else {
          vErrors.push(err40);
        }
        errors++;
      }
    }
    if (data.localContent !== undefined) {
      let data22 = data.localContent;
      if (data22 && typeof data22 == "object" && !Array.isArray(data22)) {
        for (const key9 in data22) {
          const _errs59 = errors;
          if (typeof key9 === "string") {
            if (!pattern38.test(key9)) {
              const err41 = {
                instancePath: instancePath + "/localContent",
                schemaPath: "#/properties/localContent/propertyNames/pattern",
                keyword: "pattern",
                params: { pattern: "^t_[a-z0-9_]{2,40}$" },
                message: 'must match pattern "' + "^t_[a-z0-9_]{2,40}$" + '"',
                propertyName: key9,
              };
              if (vErrors === null) {
                vErrors = [err41];
              } else {
                vErrors.push(err41);
              }
              errors++;
            }
          }
          var valid25 = _errs59 === errors;
          if (!valid25) {
            const err42 = {
              instancePath: instancePath + "/localContent",
              schemaPath: "#/properties/localContent/propertyNames",
              keyword: "propertyNames",
              params: { propertyName: key9 },
              message: "property name must be valid",
            };
            if (vErrors === null) {
              vErrors = [err42];
            } else {
              vErrors.push(err42);
            }
            errors++;
          }
        }
        for (const key10 in data22) {
          let data23 = data22[key10];
          if (data23 && typeof data23 == "object" && !Array.isArray(data23)) {
            if (data23.type === undefined) {
              const err43 = {
                instancePath:
                  instancePath + "/localContent/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),
                schemaPath: "#/$defs/localContentItem/required",
                keyword: "required",
                params: { missingProperty: "type" },
                message: "must have required property '" + "type" + "'",
              };
              if (vErrors === null) {
                vErrors = [err43];
              } else {
                vErrors.push(err43);
              }
              errors++;
            }
            if (data23.value === undefined) {
              const err44 = {
                instancePath:
                  instancePath + "/localContent/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),
                schemaPath: "#/$defs/localContentItem/required",
                keyword: "required",
                params: { missingProperty: "value" },
                message: "must have required property '" + "value" + "'",
              };
              if (vErrors === null) {
                vErrors = [err44];
              } else {
                vErrors.push(err44);
              }
              errors++;
            }
            for (const key11 in data23) {
              if (!(key11 === "type" || key11 === "value")) {
                const err45 = {
                  instancePath:
                    instancePath +
                    "/localContent/" +
                    key10.replace(/~/g, "~0").replace(/\//g, "~1"),
                  schemaPath: "#/$defs/localContentItem/additionalProperties",
                  keyword: "additionalProperties",
                  params: { additionalProperty: key11 },
                  message: "must NOT have additional properties",
                };
                if (vErrors === null) {
                  vErrors = [err45];
                } else {
                  vErrors.push(err45);
                }
                errors++;
              }
            }
            if (data23.type !== undefined) {
              let data24 = data23.type;
              if (!(
                data24 === "text" ||
                data24 === "richText" ||
                data24 === "asset" ||
                data24 === "link"
              )) {
                const err46 = {
                  instancePath:
                    instancePath +
                    "/localContent/" +
                    key10.replace(/~/g, "~0").replace(/\//g, "~1") +
                    "/type",
                  schemaPath: "#/$defs/localContentItem/properties/type/enum",
                  keyword: "enum",
                  params: { allowedValues: schema85.properties.type.enum },
                  message: "must be equal to one of the allowed values",
                };
                if (vErrors === null) {
                  vErrors = [err46];
                } else {
                  vErrors.push(err46);
                }
                errors++;
              }
            }
            if (data23.value !== undefined) {
              let data25 = data23.value;
              if (data25 && typeof data25 == "object" && !Array.isArray(data25)) {
                if (Object.keys(data25).length < 1) {
                  const err47 = {
                    instancePath:
                      instancePath +
                      "/localContent/" +
                      key10.replace(/~/g, "~0").replace(/\//g, "~1") +
                      "/value",
                    schemaPath: "#/$defs/localContentItem/properties/value/minProperties",
                    keyword: "minProperties",
                    params: { limit: 1 },
                    message: "must NOT have fewer than 1 properties",
                  };
                  if (vErrors === null) {
                    vErrors = [err47];
                  } else {
                    vErrors.push(err47);
                  }
                  errors++;
                }
                for (const key12 in data25) {
                  const _errs68 = errors;
                  if (typeof key12 === "string") {
                    if (!pattern39.test(key12)) {
                      const err48 = {
                        instancePath:
                          instancePath +
                          "/localContent/" +
                          key10.replace(/~/g, "~0").replace(/\//g, "~1") +
                          "/value",
                        schemaPath:
                          "#/$defs/localContentItem/properties/value/propertyNames/pattern",
                        keyword: "pattern",
                        params: { pattern: "^[a-z]{2,3}(-[A-Z]{2})?$" },
                        message: 'must match pattern "' + "^[a-z]{2,3}(-[A-Z]{2})?$" + '"',
                        propertyName: key12,
                      };
                      if (vErrors === null) {
                        vErrors = [err48];
                      } else {
                        vErrors.push(err48);
                      }
                      errors++;
                    }
                  }
                  var valid29 = _errs68 === errors;
                  if (!valid29) {
                    const err49 = {
                      instancePath:
                        instancePath +
                        "/localContent/" +
                        key10.replace(/~/g, "~0").replace(/\//g, "~1") +
                        "/value",
                      schemaPath: "#/$defs/localContentItem/properties/value/propertyNames",
                      keyword: "propertyNames",
                      params: { propertyName: key12 },
                      message: "property name must be valid",
                    };
                    if (vErrors === null) {
                      vErrors = [err49];
                    } else {
                      vErrors.push(err49);
                    }
                    errors++;
                  }
                }
              } else {
                const err50 = {
                  instancePath:
                    instancePath +
                    "/localContent/" +
                    key10.replace(/~/g, "~0").replace(/\//g, "~1") +
                    "/value",
                  schemaPath: "#/$defs/localContentItem/properties/value/type",
                  keyword: "type",
                  params: { type: "object" },
                  message: "must be object",
                };
                if (vErrors === null) {
                  vErrors = [err50];
                } else {
                  vErrors.push(err50);
                }
                errors++;
              }
            }
          } else {
            const err51 = {
              instancePath:
                instancePath + "/localContent/" + key10.replace(/~/g, "~0").replace(/\//g, "~1"),
              schemaPath: "#/$defs/localContentItem/type",
              keyword: "type",
              params: { type: "object" },
              message: "must be object",
            };
            if (vErrors === null) {
              vErrors = [err51];
            } else {
              vErrors.push(err51);
            }
            errors++;
          }
        }
      } else {
        const err52 = {
          instancePath: instancePath + "/localContent",
          schemaPath: "#/properties/localContent/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err52];
        } else {
          vErrors.push(err52);
        }
        errors++;
      }
    }
    if (data.meta !== undefined) {
      let data26 = data.meta;
      if (data26 && typeof data26 == "object" && !Array.isArray(data26)) {
        if (data26.title !== undefined) {
          let data27 = data26.title;
          if (typeof data27 === "string") {
            if (func1(data27) > 200) {
              const err53 = {
                instancePath: instancePath + "/meta/title",
                schemaPath: "#/properties/meta/properties/title/maxLength",
                keyword: "maxLength",
                params: { limit: 200 },
                message: "must NOT have more than 200 characters",
              };
              if (vErrors === null) {
                vErrors = [err53];
              } else {
                vErrors.push(err53);
              }
              errors++;
            }
          } else {
            const err54 = {
              instancePath: instancePath + "/meta/title",
              schemaPath: "#/properties/meta/properties/title/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err54];
            } else {
              vErrors.push(err54);
            }
            errors++;
          }
        }
        if (data26.description !== undefined) {
          let data28 = data26.description;
          if (typeof data28 === "string") {
            if (func1(data28) > 2000) {
              const err55 = {
                instancePath: instancePath + "/meta/description",
                schemaPath: "#/properties/meta/properties/description/maxLength",
                keyword: "maxLength",
                params: { limit: 2000 },
                message: "must NOT have more than 2000 characters",
              };
              if (vErrors === null) {
                vErrors = [err55];
              } else {
                vErrors.push(err55);
              }
              errors++;
            }
          } else {
            const err56 = {
              instancePath: instancePath + "/meta/description",
              schemaPath: "#/properties/meta/properties/description/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err56];
            } else {
              vErrors.push(err56);
            }
            errors++;
          }
        }
        if (data26.thumbnailAssetId !== undefined) {
          let data29 = data26.thumbnailAssetId;
          if (typeof data29 === "string") {
            if (!pattern8.test(data29)) {
              const err57 = {
                instancePath: instancePath + "/meta/thumbnailAssetId",
                schemaPath: "#/$defs/uuid/pattern",
                keyword: "pattern",
                params: {
                  pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
                },
                message:
                  'must match pattern "' +
                  "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" +
                  '"',
              };
              if (vErrors === null) {
                vErrors = [err57];
              } else {
                vErrors.push(err57);
              }
              errors++;
            }
          } else {
            const err58 = {
              instancePath: instancePath + "/meta/thumbnailAssetId",
              schemaPath: "#/$defs/uuid/type",
              keyword: "type",
              params: { type: "string" },
              message: "must be string",
            };
            if (vErrors === null) {
              vErrors = [err58];
            } else {
              vErrors.push(err58);
            }
            errors++;
          }
        }
      } else {
        const err59 = {
          instancePath: instancePath + "/meta",
          schemaPath: "#/properties/meta/type",
          keyword: "type",
          params: { type: "object" },
          message: "must be object",
        };
        if (vErrors === null) {
          vErrors = [err59];
        } else {
          vErrors.push(err59);
        }
        errors++;
      }
    }
  } else {
    const err60 = {
      instancePath,
      schemaPath: "#/type",
      keyword: "type",
      params: { type: "object" },
      message: "must be object",
    };
    if (vErrors === null) {
      vErrors = [err60];
    } else {
      vErrors.push(err60);
    }
    errors++;
  }
  validate20.errors = vErrors;
  evaluated0.props = props0;
  return errors === 0;
}
validate20.evaluated = { dynamicProps: true, dynamicItems: false };
