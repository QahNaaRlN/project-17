import { expect, it } from "vitest";
import { builtinCatalogue, BUILTIN_PRIMITIVES, validateManifest } from "../src/index.js";

it("MF-001 / DS-001 fixed builtin catalogue", () => {
  expect(Object.keys(builtinCatalogue).sort()).toEqual([...BUILTIN_PRIMITIVES].sort());
  expect(builtinCatalogue.Button.props.variant.values).toEqual([
    "default",
    "primary",
    "secondary",
    "ghost",
  ]);
  expect(builtinCatalogue.Button.props.size.values).toEqual(["sm", "md", "lg"]);
  expect(builtinCatalogue.Divider.props.orientation.values).toEqual(["horizontal", "vertical"]);
});
it("MF-001 custom variants use a native component, never redefine Button", () => {
  const m = {
    manifestVersion: "1.0",
    app: { id: "test", version: "1.0.0", framework: "react" },
    irVersions: ["1.0"],
    breakpoints: {},
    tokens: {},
    components: { AppButton: { props: { variant: { type: "enum", values: ["brand"] } } } },
  };
  expect(validateManifest(m).valid).toBe(true);
  const invalid = { ...m, components: { Button: m.components.AppButton } };
  expect(validateManifest(invalid).diagnostics).toContainEqual(
    expect.objectContaining({ code: "MANIFEST_NAME_RESERVED", pointer: "/components/Button" }),
  );
});
