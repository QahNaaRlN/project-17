// Сгенерировано из schema/builtin-catalogue.json. Не редактировать.
export const builtinCatalogue = {
  "Box": {
    "container": true,
    "props": {
      "backgroundImage": {
        "type": "asset",
        "content": true
      },
      "as": {
        "type": "enum",
        "values": [
          "div",
          "section",
          "article",
          "aside",
          "header",
          "footer",
          "nav",
          "main"
        ]
      }
    },
    "events": {},
    "slots": {}
  },
  "Stack": {
    "container": true,
    "props": {
      "direction": {
        "type": "enum",
        "values": [
          "vertical",
          "horizontal"
        ],
        "responsive": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Flex": {
    "container": true,
    "props": {
      "wrap": {
        "type": "boolean",
        "responsive": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Grid": {
    "container": true,
    "props": {},
    "events": {},
    "slots": {}
  },
  "Container": {
    "container": true,
    "props": {
      "size": {
        "type": "enum",
        "enumSource": "container"
      }
    },
    "events": {},
    "slots": {}
  },
  "Divider": {
    "container": false,
    "props": {
      "orientation": {
        "type": "enum",
        "values": [
          "horizontal",
          "vertical"
        ]
      }
    },
    "events": {},
    "slots": {}
  },
  "Modal": {
    "container": true,
    "props": {
      "size": {
        "type": "enum",
        "values": [
          "sm",
          "md",
          "lg",
          "full"
        ]
      },
      "dismissible": {
        "type": "boolean"
      },
      "title": {
        "type": "text",
        "content": true
      }
    },
    "events": {
      "close": {}
    },
    "slots": {}
  },
  "Heading": {
    "container": false,
    "props": {
      "level": {
        "type": "number",
        "integer": true,
        "min": 1,
        "max": 6
      },
      "text": {
        "type": "text",
        "content": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Text": {
    "container": false,
    "props": {
      "as": {
        "type": "enum",
        "values": [
          "p",
          "span"
        ]
      },
      "text": {
        "type": "text",
        "content": true
      }
    },
    "events": {},
    "slots": {}
  },
  "RichText": {
    "container": false,
    "props": {
      "value": {
        "type": "richText",
        "content": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Label": {
    "container": false,
    "props": {
      "text": {
        "type": "text",
        "content": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Image": {
    "container": false,
    "props": {
      "fit": {
        "type": "enum",
        "values": [
          "cover",
          "contain"
        ]
      },
      "loading": {
        "type": "enum",
        "values": [
          "lazy",
          "eager"
        ]
      },
      "decorative": {
        "type": "boolean"
      },
      "src": {
        "type": "asset",
        "assetKind": "image",
        "content": true
      },
      "alt": {
        "type": "text",
        "content": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Video": {
    "container": false,
    "props": {
      "autoplay": {
        "type": "boolean"
      },
      "muted": {
        "type": "boolean"
      },
      "loop": {
        "type": "boolean"
      },
      "controls": {
        "type": "boolean"
      },
      "src": {
        "type": "asset",
        "assetKind": "video",
        "content": true
      },
      "poster": {
        "type": "asset",
        "assetKind": "image",
        "content": true
      },
      "caption": {
        "type": "text",
        "content": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Icon": {
    "container": false,
    "props": {
      "name": {
        "type": "enum",
        "enumSource": "icons"
      },
      "size": {
        "type": "enum",
        "values": [
          "sm",
          "md",
          "lg"
        ]
      },
      "label": {
        "type": "text",
        "content": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Button": {
    "container": false,
    "props": {
      "variant": {
        "type": "enum",
        "values": [
          "default",
          "primary",
          "secondary",
          "ghost"
        ]
      },
      "size": {
        "type": "enum",
        "values": [
          "sm",
          "md",
          "lg"
        ]
      },
      "disabled": {
        "type": "boolean"
      },
      "label": {
        "type": "text",
        "content": true
      }
    },
    "events": {
      "click": {}
    },
    "slots": {}
  },
  "Link": {
    "container": true,
    "props": {
      "to": {
        "type": "link"
      },
      "newTab": {
        "type": "boolean"
      },
      "label": {
        "type": "text",
        "content": true
      }
    },
    "events": {
      "click": {}
    },
    "slots": {}
  },
  "Repeat": {
    "container": true,
    "props": {
      "as": {
        "type": "string"
      },
      "limit": {
        "type": "number",
        "integer": true,
        "min": 1,
        "max": 200
      },
      "key": {
        "type": "string"
      },
      "items": {
        "type": "list",
        "content": true
      }
    },
    "events": {},
    "slots": {
      "empty": {}
    }
  },
  "Slot": {
    "container": false,
    "props": {
      "name": {
        "type": "string",
        "required": true
      }
    },
    "events": {},
    "slots": {}
  },
  "Composed": {
    "container": false,
    "props": {},
    "events": {},
    "slots": {}
  }
} as const;
