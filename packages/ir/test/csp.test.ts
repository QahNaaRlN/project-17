import { afterEach, expect, it, vi } from "vitest";
import { validateDocument } from "../src/validate.js";
afterEach(() => vi.unstubAllGlobals());
it("SDK-032/IR-005: L1 валидатор работает при CSP без динамической компиляции Function", () => {
  const compile = vi.fn(function () {
    throw new Error("CSP unsafe-eval denied");
  });
  vi.stubGlobal("Function", compile);
  const result = validateDocument({
    irVersion: "1.0",
    kind: "page",
    root: "text",
    nodes: { text: { id: "text", type: "Text", props: { text: "safe" } } },
  });
  expect(result.valid).toBe(true);
  expect(compile).not.toHaveBeenCalled();
});
