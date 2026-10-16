/** Repository metadata only; consumers must never execute conventions or open paths. */
export interface CodeElement {
  definition: { file: string; line: number };
  implementation: { file: string; export: string; line?: number };
  dependencies: string[];
  tests: string[];
}
export interface CodeIndex {
  repo: { root: "."; commit: string };
  elements: Record<string, CodeElement>;
  conventions: { registry: string; testCommand: string; manifestCommand: string };
}
const obj = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const exact = (v: unknown, keys: string[]): v is Record<string, unknown> =>
  obj(v) && Object.keys(v).every((k) => keys.includes(k));
const text = (v: unknown, limit: number): v is string =>
  typeof v === "string" && v.length > 0 && Array.from(v).length <= limit && !/[\p{Cc}]/u.test(v);
export function codeIndexPath(value: unknown): value is string {
  return (
    text(value, 512) &&
    !/[\\:]/.test(value) &&
    value
      .split("/")
      .every(
        (part) =>
          !!part &&
          ![
            ".",
            "..",
            "node_modules",
            "dist",
            "generated",
            ".git",
            ".ssh",
            ".aws",
            ".codex",
          ].includes(part.toLowerCase()) &&
          !part.toLowerCase().startsWith(".env"),
      )
  );
}
const paths = (v: unknown) =>
  Array.isArray(v) && v.length <= 1000 && v.every(codeIndexPath) && new Set(v).size === v.length;
const line = (v: unknown) =>
  typeof v === "number" && Number.isSafeInteger(v) && v >= 1 && v <= 10000000;
export function validateCodeIndex(value: unknown): value is CodeIndex {
  if (
    !exact(value, ["repo", "elements", "conventions"]) ||
    !exact(value.repo, ["root", "commit"]) ||
    value.repo.root !== "." ||
    typeof value.repo.commit !== "string" ||
    !/^([0-9a-f]{40}|[0-9a-f]{64})$/.test(value.repo.commit) ||
    !obj(value.elements) ||
    Object.keys(value.elements).length > 1000 ||
    !exact(value.conventions, ["registry", "testCommand", "manifestCommand"]) ||
    !codeIndexPath(value.conventions.registry) ||
    !text(value.conventions.testCommand, 512) ||
    !text(value.conventions.manifestCommand, 512)
  )
    return false;
  for (const [key, v] of Object.entries(value.elements)) {
    if (
      !/^(component|action|dataSource):[^:]+$/.test(key) ||
      !text(key.slice(key.indexOf(":") + 1), 200) ||
      !exact(v, ["definition", "implementation", "dependencies", "tests"]) ||
      !exact(v.definition, ["file", "line"]) ||
      !codeIndexPath(v.definition.file) ||
      !line(v.definition.line) ||
      !exact(v.implementation, ["file", "export", "line"]) ||
      !codeIndexPath(v.implementation.file) ||
      !text(v.implementation.export, 200) ||
      (Object.hasOwn(v.implementation, "line") && !line(v.implementation.line)) ||
      !paths(v.dependencies) ||
      !paths(v.tests)
    )
      return false;
  }
  return new TextEncoder().encode(JSON.stringify(value)).byteLength <= 1048576;
}
