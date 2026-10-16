import { fixture } from "./fixture.js";
export function dataFixture() {
  const session = fixture();
  session.record.body.nodes = {
    n_root: { id: "n_root", type: "Stack", children: ["n_heading"] },
    n_heading: { id: "n_heading", type: "Heading" },
  };
  session.manifest.schemas = {
    Collection: {
      version: 1,
      fields: { handle: { type: "string", required: true, maxLength: 64, localized: true } },
    },
  };
  session.record.body.content = {
    schema: "Collection",
    resolve: { by: "entity", entityId: session.record.id },
  };
  session.manifest.dataSources = {
    "test.items": {
      params: {
        collection: { type: "string", required: true, maxLength: 64 },
        limit: { type: "number", integer: true, min: 1, max: 200, default: 12 },
        enabled: { type: "boolean" },
        choice: { type: "enum", values: ["one", "two"] },
      },
      result: {
        type: "object",
        fields: {
          items: {
            type: "list",
            required: true,
            of: {
              type: "object",
              fields: {
                id: { type: "string", required: true },
                title: { type: "text", required: true },
                count: { type: "number" },
                image: { type: "asset", assetKind: "image" },
              },
            },
          },
        },
      },
    },
    "test.pages": {
      params: {},
      paginated: true,
      result: { type: "list", of: { type: "string", required: true } },
    },
    "test.unsupported": {
      params: { record: { type: "object", required: true, fields: {} } },
      result: { type: "string" },
    },
  };
  session.record.body.dataSources = {
    featured: { source: "test.items", params: { collection: "$content.handle" } },
  };
  return session;
}
