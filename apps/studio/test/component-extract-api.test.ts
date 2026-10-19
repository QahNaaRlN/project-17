import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import { fixture, changesetId } from "./fixture.js";
import { StudioError } from "../src/types.js";

const componentId = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c33";
const plan = {
  nodeId: "n_block",
  blockers: [],
  candidates: [],
  localKeys: [],
  slotRanges: [],
  slotPositions: [],
  sourceListTypes: [],
  effectiveMode: "SYSTEM",
};

it("STU-401: the plan is read with the draft, node and Studio environment", async () => {
  const session = fixture();
  const fetcher = vi.fn<typeof fetch>(async () => Response.json(plan));
  const api = createApi(fetcher);
  expect(await api.extractPlan!(session, "n_block")).toEqual(plan);
  const url = new URL(String(fetcher.mock.calls[0]![0]), "http://localhost");
  expect(url.pathname).toBe(
    `/api/studio/api/v1/documents/${session.record.id}/component-extract-plan`,
  );
  expect(Object.fromEntries(url.searchParams)).toEqual({
    changesetId,
    nodeId: "n_block",
    environment: "staging",
  });
  await expect(api.extractPlan!(session, "a/b")).rejects.toThrow("IDENTIFIER_INVALID");
  // A plan for another node is not accepted.
  await expect(api.extractPlan!(session, "n_other")).rejects.toBeInstanceOf(StudioError);
});

it("STU-412/414: components are read in the draft; failures and invalid documents are skipped", async () => {
  const session = fixture();
  const component = {
    irVersion: "1.0",
    kind: "component",
    root: "n_root",
    nodes: { n_root: { id: "n_root", type: "Stack" } },
  };
  const broken = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c44";
  const page = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c55";
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const path = String(input);
    if (path.includes(broken)) return Response.json({ code: "NOT_FOUND" }, { status: 404 });
    if (path.includes(page))
      return Response.json({
        id: page,
        kind: "page",
        path: "/x",
        body: { ...component, kind: "page" },
      });
    return Response.json({ id: componentId, kind: "component", path: null, body: component });
  });
  const api = createApi(fetcher);
  const result = await api.components!(session, [componentId, broken, page, "not-a-uuid"]);
  expect(Object.keys(result)).toEqual([componentId]);
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(String(fetcher.mock.calls[0]![0])).toContain("changesetId=" + changesetId);
  // Cancellation is not swallowed.
  const controller = new AbortController();
  controller.abort();
  const aborting = createApi(async () => {
    throw new DOMException("aborted", "AbortError");
  });
  await expect(
    aborting.components!(session, [componentId], controller.signal),
  ).rejects.toBeDefined();
});

it("STU-410: operation codes and pointers are kept without values", async () => {
  const session = fixture();
  const api = createApi(async () =>
    Response.json(
      {
        code: "OPERATION_INVALID",
        params: {
          operationCode: "COMPONENT_NAME_TAKEN",
          diagnostics: [
            {
              code: "COMPONENT_NAME_TAKEN",
              pointer: "/payload/name",
              nodeId: "n_block",
              message: "secret",
            },
            { code: "bad code", pointer: "/x" },
          ],
        },
      },
      { status: 422 },
    ),
  );
  const error = await api
    .apply(session, [], "key", new AbortController().signal, "Studio: save as component «X»")
    .catch((e: StudioError) => e);
  expect(error).toBeInstanceOf(StudioError);
  expect((error as StudioError).operation).toEqual({
    code: "COMPONENT_NAME_TAKEN",
    diagnostics: [{ code: "COMPONENT_NAME_TAKEN", pointer: "/payload/name", nodeId: "n_block" }],
  });
  const plain = createApi(async () => Response.json({ code: "FORBIDDEN" }, { status: 403 }));
  const forbidden = await plain
    .apply(session, [], "key", new AbortController().signal)
    .catch((e: StudioError) => e);
  expect((forbidden as StudioError).operation).toBeUndefined();
});
