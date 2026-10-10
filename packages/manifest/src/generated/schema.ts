// Сгенерировано scripts/generate-types.ts из schema/manifest-1.0.schema.json. Не редактировать.

export const manifestSchema = {
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://cms.dev/schemas/manifest-1.0/manifest.json",
  "title": "Manifest приложения 1.0",
  "description": "Описание приложения для CMS: токены, компоненты, действия, источники данных, форматтеры, схемы контента и capabilities (docs/spec/04-manifest.md). Единственный источник истины формата; типы TS/Go генерируются из него.",
  "type": "object",
  "required": [
    "manifestVersion",
    "app",
    "irVersions",
    "breakpoints",
    "tokens"
  ],
  "additionalProperties": false,
  "properties": {
    "manifestVersion": {
      "const": "1.0"
    },
    "app": {
      "$ref": "#/$defs/App"
    },
    "irVersions": {
      "type": "array",
      "minItems": 1,
      "uniqueItems": true,
      "items": {
        "type": "string",
        "pattern": "^[0-9]+\\.[0-9]+$"
      }
    },
    "breakpoints": {
      "description": "Breakpoint'ы: имя → минимальная ширина в px. Имя base зарезервировано.",
      "type": "object",
      "maxProperties": 8,
      "propertyNames": {
        "$ref": "#/$defs/TokenName",
        "not": {
          "const": "base"
        }
      },
      "additionalProperties": {
        "type": "integer",
        "minimum": 1,
        "maximum": 10000
      }
    },
    "tokens": {
      "$ref": "#/$defs/Tokens"
    },
    "icons": {
      "type": "array",
      "uniqueItems": true,
      "maxItems": 2000,
      "items": {
        "type": "string",
        "pattern": "^[a-z][a-z0-9-]{0,63}$"
      }
    },
    "primitives": {
      "description": "Расширенные примитивы приложения (03 §2.1): описываются как компоненты.",
      "type": "object",
      "propertyNames": {
        "$ref": "#/$defs/ComponentName"
      },
      "additionalProperties": {
        "$ref": "#/$defs/Component"
      }
    },
    "components": {
      "type": "object",
      "propertyNames": {
        "$ref": "#/$defs/ComponentName"
      },
      "additionalProperties": {
        "$ref": "#/$defs/Component"
      }
    },
    "actions": {
      "type": "object",
      "propertyNames": {
        "$ref": "#/$defs/DottedName"
      },
      "additionalProperties": {
        "$ref": "#/$defs/Action"
      }
    },
    "dataSources": {
      "type": "object",
      "propertyNames": {
        "$ref": "#/$defs/DottedName"
      },
      "additionalProperties": {
        "$ref": "#/$defs/DataSource"
      }
    },
    "formatters": {
      "type": "object",
      "propertyNames": {
        "pattern": "^[a-z][a-zA-Z0-9]*(\\.[a-z][a-zA-Z0-9]*)*$",
        "maxLength": 128
      },
      "additionalProperties": {
        "$ref": "#/$defs/Formatter"
      }
    },
    "schemas": {
      "type": "object",
      "propertyNames": {
        "pattern": "^[A-Z][A-Za-z0-9]{1,63}$"
      },
      "additionalProperties": {
        "$ref": "#/$defs/ContentSchema"
      }
    },
    "capabilities": {
      "type": "object",
      "propertyNames": {
        "$ref": "#/$defs/DottedName"
      },
      "additionalProperties": {
        "$ref": "#/$defs/Capability"
      }
    },
    "codeIndex": {
      "type": "object",
      "required": [
        "hash",
        "uploaded"
      ],
      "additionalProperties": false,
      "properties": {
        "hash": {
          "$ref": "#/$defs/Sha256"
        },
        "uploaded": {
          "type": "boolean"
        }
      }
    }
  },
  "$defs": {
    "Sha256": {
      "type": "string",
      "pattern": "^sha256:[0-9a-f]{64}$"
    },
    "TokenName": {
      "type": "string",
      "pattern": "^[a-z][A-Za-z0-9]*$",
      "maxLength": 64
    },
    "ComponentName": {
      "type": "string",
      "pattern": "^[A-Z][A-Za-z0-9]{0,63}$"
    },
    "DottedName": {
      "type": "string",
      "pattern": "^[a-z][a-zA-Z0-9]*(\\.[a-z][a-zA-Z0-9]*)+$",
      "maxLength": 128
    },
    "FieldName": {
      "type": "string",
      "pattern": "^[a-z][A-Za-z0-9]{0,63}$"
    },
    "Description": {
      "type": "string",
      "maxLength": 1000
    },
    "App": {
      "type": "object",
      "required": [
        "id",
        "version",
        "framework"
      ],
      "additionalProperties": false,
      "properties": {
        "id": {
          "type": "string",
          "pattern": "^[a-z0-9][a-z0-9-]{0,63}$"
        },
        "version": {
          "type": "string",
          "minLength": 1,
          "maxLength": 64
        },
        "build": {
          "type": "string",
          "maxLength": 128
        },
        "framework": {
          "enum": [
            "react",
            "vue",
            "svelte",
            "other"
          ]
        }
      }
    },
    "Length": {
      "type": "string",
      "pattern": "^(0|-?[0-9]+(\\.[0-9]+)?(px|rem|em|%|vw|vh|ch))$"
    },
    "TokenMap": {
      "type": "object",
      "propertyNames": {
        "$ref": "#/$defs/TokenName"
      },
      "maxProperties": 200
    },
    "Tokens": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "spacing": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "$ref": "#/$defs/Length"
          }
        },
        "colors": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "anyOf": [
              {
                "type": "string",
                "minLength": 1,
                "maxLength": 64
              },
              {
                "type": "object",
                "required": [
                  "value"
                ],
                "additionalProperties": false,
                "properties": {
                  "value": {
                    "type": "string",
                    "minLength": 1,
                    "maxLength": 64
                  },
                  "modes": {
                    "type": "object",
                    "propertyNames": {
                      "$ref": "#/$defs/TokenName"
                    },
                    "additionalProperties": {
                      "type": "string",
                      "minLength": 1,
                      "maxLength": 64
                    }
                  }
                }
              }
            ]
          }
        },
        "radius": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "$ref": "#/$defs/Length"
          }
        },
        "typography": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "$ref": "#/$defs/Typography"
          }
        },
        "shadow": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "type": "string",
            "maxLength": 200
          }
        },
        "borderWidth": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "$ref": "#/$defs/Length"
          }
        },
        "container": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "$ref": "#/$defs/Length"
          }
        },
        "layer": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "type": "integer",
            "minimum": 0,
            "maximum": 100000
          }
        },
        "transition": {
          "$ref": "#/$defs/TokenMap",
          "additionalProperties": {
            "type": "string",
            "maxLength": 100
          }
        }
      }
    },
    "Typography": {
      "type": "object",
      "required": [
        "fontFamily",
        "fontSize"
      ],
      "additionalProperties": false,
      "properties": {
        "fontFamily": {
          "type": "string",
          "minLength": 1,
          "maxLength": 200
        },
        "fontSize": {
          "description": "Адаптивный размер: ключ base обязателен, прочие — breakpoint'ы manifest.",
          "type": "object",
          "required": [
            "base"
          ],
          "propertyNames": {
            "$ref": "#/$defs/TokenName"
          },
          "additionalProperties": {
            "$ref": "#/$defs/Length"
          }
        },
        "lineHeight": {
          "type": "string",
          "maxLength": 32
        },
        "fontWeight": {
          "type": "integer",
          "minimum": 100,
          "maximum": 900,
          "multipleOf": 100
        },
        "letterSpacing": {
          "type": "string",
          "maxLength": 32
        },
        "large": {
          "type": "boolean"
        }
      }
    },
    "SourceRef": {
      "type": "object",
      "required": [
        "file"
      ],
      "additionalProperties": false,
      "properties": {
        "file": {
          "type": "string",
          "minLength": 1,
          "maxLength": 512
        },
        "export": {
          "type": "string",
          "maxLength": 128
        },
        "line": {
          "type": "integer",
          "minimum": 1
        }
      }
    },
    "Deprecated": {
      "type": "object",
      "required": [
        "since"
      ],
      "additionalProperties": false,
      "properties": {
        "since": {
          "type": "string",
          "minLength": 1,
          "maxLength": 64
        },
        "replacement": {
          "type": "string",
          "maxLength": 128
        },
        "message": {
          "$ref": "#/$defs/Description"
        }
      }
    },
    "Fields": {
      "type": "object",
      "propertyNames": {
        "$ref": "#/$defs/FieldName"
      },
      "maxProperties": 200,
      "additionalProperties": {
        "$ref": "#/$defs/Type"
      }
    },
    "Type": {
      "description": "Тип системы типов (04 §4): свойство компонента, аргумент действия, параметр или результат источника, поле схемы.",
      "type": "object",
      "required": [
        "type"
      ],
      "discriminator": {
        "propertyName": "type"
      },
      "oneOf": [
        {
          "$ref": "#/$defs/StringType"
        },
        {
          "$ref": "#/$defs/TextType"
        },
        {
          "$ref": "#/$defs/RichTextType"
        },
        {
          "$ref": "#/$defs/NumberType"
        },
        {
          "$ref": "#/$defs/BooleanType"
        },
        {
          "$ref": "#/$defs/EnumType"
        },
        {
          "$ref": "#/$defs/DateType"
        },
        {
          "$ref": "#/$defs/DatetimeType"
        },
        {
          "$ref": "#/$defs/UrlType"
        },
        {
          "$ref": "#/$defs/LinkType"
        },
        {
          "$ref": "#/$defs/AssetType"
        },
        {
          "$ref": "#/$defs/ReferenceType"
        },
        {
          "$ref": "#/$defs/ListType"
        },
        {
          "$ref": "#/$defs/ObjectType"
        },
        {
          "$ref": "#/$defs/ColorType"
        },
        {
          "$ref": "#/$defs/NodeRefType"
        }
      ]
    },
    "TypeModifiers": {
      "description": "Общие модификаторы (04 §4).",
      "type": "object",
      "properties": {
        "required": {
          "type": "boolean"
        },
        "responsive": {
          "type": "boolean"
        },
        "localized": {
          "type": "boolean"
        },
        "unique": {
          "type": "boolean"
        },
        "content": {
          "type": "boolean"
        },
        "description": {
          "$ref": "#/$defs/Description"
        },
        "deprecated": {
          "$ref": "#/$defs/Deprecated"
        }
      }
    },
    "StringType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "string"
        },
        "minLength": {
          "type": "integer",
          "minimum": 0
        },
        "maxLength": {
          "type": "integer",
          "minimum": 1
        },
        "pattern": {
          "type": "string",
          "maxLength": 500
        },
        "default": {
          "type": "string"
        }
      }
    },
    "TextType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "text"
        },
        "maxLength": {
          "type": "integer",
          "minimum": 1
        },
        "multiline": {
          "type": "boolean"
        },
        "default": {
          "type": "string"
        }
      }
    },
    "RichTextType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "richText"
        },
        "marks": {
          "type": "array",
          "uniqueItems": true,
          "items": {
            "enum": [
              "bold",
              "italic",
              "underline",
              "strike",
              "code",
              "link"
            ]
          }
        },
        "blocks": {
          "type": "array",
          "uniqueItems": true,
          "items": {
            "enum": [
              "paragraph",
              "heading",
              "bulletList",
              "orderedList",
              "blockquote",
              "image",
              "divider"
            ]
          }
        }
      }
    },
    "NumberType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "number"
        },
        "min": {
          "type": "number"
        },
        "max": {
          "type": "number"
        },
        "integer": {
          "type": "boolean"
        },
        "default": {
          "type": "number"
        }
      }
    },
    "BooleanType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "boolean"
        },
        "default": {
          "type": "boolean"
        }
      }
    },
    "EnumType": {
      "type": "object",
      "required": [
        "type",
        "values"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "enum"
        },
        "values": {
          "type": "array",
          "minItems": 1,
          "maxItems": 200,
          "uniqueItems": true,
          "items": {
            "type": "string",
            "minLength": 1,
            "maxLength": 64
          }
        },
        "default": {
          "type": "string"
        }
      }
    },
    "DateType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "date"
        }
      }
    },
    "DatetimeType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "datetime"
        }
      }
    },
    "UrlType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "url"
        },
        "schemes": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": {
            "type": "string",
            "pattern": "^[a-z][a-z0-9+.-]*$"
          }
        }
      }
    },
    "LinkType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "link"
        }
      }
    },
    "AssetType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "asset"
        },
        "assetKind": {
          "enum": [
            "image",
            "video",
            "file"
          ]
        },
        "mimeTypes": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": {
            "type": "string",
            "pattern": "^[a-z]+/[a-z0-9.+-]+$"
          }
        }
      }
    },
    "ReferenceType": {
      "type": "object",
      "required": [
        "type",
        "schema"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "reference"
        },
        "schema": {
          "type": "string",
          "pattern": "^[A-Z][A-Za-z0-9]{1,63}$"
        }
      }
    },
    "ListType": {
      "type": "object",
      "required": [
        "type",
        "of"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "list"
        },
        "of": {
          "$ref": "#/$defs/Type"
        },
        "min": {
          "type": "integer",
          "minimum": 0
        },
        "max": {
          "type": "integer",
          "minimum": 1
        }
      }
    },
    "ObjectType": {
      "type": "object",
      "required": [
        "type",
        "fields"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "object"
        },
        "fields": {
          "$ref": "#/$defs/Fields"
        }
      }
    },
    "ColorType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "color"
        },
        "default": {
          "$ref": "#/$defs/TokenName"
        }
      }
    },
    "NodeRefType": {
      "type": "object",
      "required": [
        "type"
      ],
      "unevaluatedProperties": false,
      "allOf": [
        {
          "$ref": "#/$defs/TypeModifiers"
        }
      ],
      "properties": {
        "type": {
          "const": "nodeRef"
        },
        "nodeType": {
          "$ref": "#/$defs/ComponentName"
        }
      }
    },
    "Slot": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "allowedTypes": {
          "type": "array",
          "uniqueItems": true,
          "items": {
            "$ref": "#/$defs/ComponentName"
          }
        },
        "min": {
          "type": "integer",
          "minimum": 0
        },
        "max": {
          "type": "integer",
          "minimum": 1
        },
        "description": {
          "$ref": "#/$defs/Description"
        }
      }
    },
    "Component": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "origin": {
          "enum": [
            "native",
            "generated"
          ]
        },
        "description": {
          "$ref": "#/$defs/Description"
        },
        "category": {
          "type": "string",
          "maxLength": 64
        },
        "group": {
          "enum": [
            "Layout",
            "Typography",
            "Media",
            "Actions"
          ],
          "description": "Группа дизайн-свойств для расширенного примитива (03 §2.1)."
        },
        "container": {
          "type": "boolean"
        },
        "props": {
          "$ref": "#/$defs/Fields"
        },
        "slots": {
          "type": "object",
          "propertyNames": {
            "$ref": "#/$defs/FieldName"
          },
          "additionalProperties": {
            "$ref": "#/$defs/Slot"
          }
        },
        "events": {
          "type": "object",
          "propertyNames": {
            "$ref": "#/$defs/FieldName"
          },
          "additionalProperties": {
            "type": "object",
            "additionalProperties": false,
            "properties": {
              "payload": {
                "$ref": "#/$defs/Fields"
              },
              "description": {
                "$ref": "#/$defs/Description"
              }
            }
          }
        },
        "provides": {
          "$ref": "#/$defs/Fields"
        },
        "capabilities": {
          "type": "array",
          "uniqueItems": true,
          "items": {
            "$ref": "#/$defs/DottedName"
          }
        },
        "since": {
          "type": "string",
          "maxLength": 64
        },
        "deprecated": {
          "oneOf": [
            {
              "type": "null"
            },
            {
              "$ref": "#/$defs/Deprecated"
            }
          ]
        },
        "sourceRef": {
          "$ref": "#/$defs/SourceRef"
        }
      }
    },
    "Action": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "description": {
          "$ref": "#/$defs/Description"
        },
        "args": {
          "$ref": "#/$defs/Fields"
        },
        "capabilities": {
          "type": "array",
          "uniqueItems": true,
          "items": {
            "$ref": "#/$defs/DottedName"
          }
        },
        "deprecated": {
          "oneOf": [
            {
              "type": "null"
            },
            {
              "$ref": "#/$defs/Deprecated"
            }
          ]
        },
        "sourceRef": {
          "$ref": "#/$defs/SourceRef"
        }
      }
    },
    "DataSource": {
      "type": "object",
      "required": [
        "result"
      ],
      "additionalProperties": false,
      "properties": {
        "description": {
          "$ref": "#/$defs/Description"
        },
        "params": {
          "$ref": "#/$defs/Fields"
        },
        "result": {
          "$ref": "#/$defs/Type"
        },
        "paginated": {
          "type": "boolean"
        },
        "cache": {
          "type": "object",
          "required": [
            "ttlSeconds"
          ],
          "additionalProperties": false,
          "properties": {
            "ttlSeconds": {
              "type": "integer",
              "minimum": 0,
              "maximum": 86400
            }
          }
        },
        "capabilities": {
          "type": "array",
          "uniqueItems": true,
          "items": {
            "$ref": "#/$defs/DottedName"
          }
        },
        "deprecated": {
          "oneOf": [
            {
              "type": "null"
            },
            {
              "$ref": "#/$defs/Deprecated"
            }
          ]
        },
        "sourceRef": {
          "$ref": "#/$defs/SourceRef"
        }
      }
    },
    "Formatter": {
      "description": "Форматтер для привязок (02 §4: format.fn). input — допустимые типы входного значения.",
      "type": "object",
      "required": [
        "input"
      ],
      "additionalProperties": false,
      "properties": {
        "description": {
          "$ref": "#/$defs/Description"
        },
        "input": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": {
            "enum": [
              "string",
              "text",
              "number",
              "boolean",
              "date",
              "datetime",
              "url",
              "enum"
            ]
          }
        },
        "args": {
          "$ref": "#/$defs/Fields"
        },
        "sourceRef": {
          "$ref": "#/$defs/SourceRef"
        }
      }
    },
    "ContentSchema": {
      "type": "object",
      "required": [
        "version",
        "fields"
      ],
      "additionalProperties": false,
      "properties": {
        "version": {
          "type": "integer",
          "minimum": 1
        },
        "title": {
          "type": "string",
          "maxLength": 200
        },
        "description": {
          "$ref": "#/$defs/Description"
        },
        "singleton": {
          "type": "boolean"
        },
        "display": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "titleField": {
              "$ref": "#/$defs/FieldName"
            },
            "previewField": {
              "$ref": "#/$defs/FieldName"
            }
          }
        },
        "fields": {
          "$ref": "#/$defs/Fields",
          "minProperties": 1
        },
        "migrateFrom": {
          "description": "Миграции с прежних версий схемы (05 §4.2): версия → операции.",
          "type": "object",
          "propertyNames": {
            "pattern": "^[1-9][0-9]*$"
          },
          "additionalProperties": {
            "type": "array",
            "minItems": 1,
            "items": {
              "$ref": "#/$defs/MigrationOp"
            }
          }
        }
      }
    },
    "MigrationOp": {
      "description": "Декларативная операция миграции схемы (05 §4.2).",
      "type": "object",
      "required": [
        "op"
      ],
      "discriminator": {
        "propertyName": "op"
      },
      "oneOf": [
        {
          "type": "object",
          "required": [
            "op",
            "from",
            "to"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "rename"
            },
            "from": {
              "$ref": "#/$defs/FieldName"
            },
            "to": {
              "$ref": "#/$defs/FieldName"
            }
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "drop"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            }
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field",
            "value"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "setDefault"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            },
            "value": {}
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "localize"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            }
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field",
            "locale"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "delocalize"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            },
            "locale": {
              "$ref": "#/$defs/Locale"
            }
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "wrapList"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            }
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "unwrapList"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            }
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field",
            "map"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "mapEnum"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            },
            "map": {
              "type": "object",
              "minProperties": 1,
              "additionalProperties": {
                "type": "string"
              }
            }
          }
        },
        {
          "type": "object",
          "required": [
            "op",
            "field",
            "to"
          ],
          "additionalProperties": false,
          "properties": {
            "op": {
              "const": "convert"
            },
            "field": {
              "$ref": "#/$defs/FieldName"
            },
            "to": {
              "enum": [
                "toString",
                "toNumber",
                "textToRichText",
                "richTextToText"
              ]
            }
          }
        }
      ]
    },
    "Capability": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "description": {
          "$ref": "#/$defs/Description"
        },
        "domain": {
          "type": "string",
          "pattern": "^[a-z][a-z0-9-]*$"
        }
      }
    },
    "Locale": {
      "type": "string",
      "pattern": "^[a-z]{2,3}(-[A-Z][a-z]{3})?(-[A-Z]{2})?$"
    }
  }
} as const;
