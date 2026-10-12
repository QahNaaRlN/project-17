import { expect, it, vi } from "vitest";
import { createEditor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError } from "../src/types.js";
it("STU-074: lost successful publish response retains exact command and blocks unrelated writes", async () => {
  const { api, session } = mockApi();
  session.changeset.state = "approved";
  const editor = createEditor(api);
  await editor.open(documentId, changesetId);
  let calls = 0;
  vi.mocked(api.workflow).mockImplementation(async () => {
    session.changeset.state = "merged";
    if (!calls++) throw new StudioError("NETWORK_UNCERTAIN", true);
    return {
      id: documentId,
      changesetId,
      environment: "staging",
      createdAt: "2026-10-12T00:00:00Z",
    };
  });
  await editor.workflow("publish");
  expect(editor.store.getState()).toMatchObject({
    status: "uncertain",
    publication: null,
    workflowRetry: true,
  });
  await editor.refresh();
  expect(editor.store.getState().status).toBe("uncertain");
  const opens = vi.mocked(api.open).mock.calls.length;
  await editor.open(documentId, changesetId);
  expect(api.open).toHaveBeenCalledTimes(opens);
  await editor.apply([]);
  await editor.workflow("reopen");
  await editor.changeHistory("undo");
  expect(api.apply).not.toHaveBeenCalled();
  expect(api.workflow).toHaveBeenCalledTimes(1);
  await editor.retryWorkflow();
  expect(vi.mocked(api.workflow).mock.calls[1]!.slice(0, 4)).toEqual(
    vi.mocked(api.workflow).mock.calls[0]!.slice(0, 4),
  );
  expect(editor.store.getState()).toMatchObject({
    status: "ready",
    workflowRetry: false,
    publication: { environment: "staging" },
  });
  editor.dispose();
});
it("STU-074: known rejection rereads state and retains safe diagnostics; failed reread requires refresh", async () => {
  const { api } = mockApi(),
    editor = createEditor(api);
  await editor.open(documentId, changesetId);
  const d = {
    code: "IR_INVALID",
    objectId: documentId,
    environment: "staging",
    severity: "error" as const,
    pointer: "/nodes/n_heading",
  };
  vi.mocked(api.workflow).mockRejectedValueOnce(new StudioError("VALIDATION_FAILED", false, [d]));
  vi.mocked(api.open).mockRejectedValueOnce(Error("read failed"));
  await editor.workflow("submit");
  expect(editor.store.getState()).toMatchObject({
    status: "uncertain",
    workflowRetry: false,
    workflowDiagnostics: [d],
  });
  await editor.retryWorkflow();
  expect(api.workflow).toHaveBeenCalledTimes(1);
  await editor.refresh();
  expect(editor.store.getState().status).toBe("ready");
  editor.dispose();
});
it("STU-072/074: server-disabled action, missing comment, idle or busy cannot send; late response is ignored", async () => {
  const { api, session } = mockApi(),
    editor = createEditor(api);
  await editor.workflow("submit");
  expect(api.workflow).not.toHaveBeenCalled();
  session.review.actions.submit = "FORBIDDEN";
  await editor.open(documentId, changesetId);
  await editor.workflow("submit");
  await editor.workflow("request-changes", " ");
  await editor.workflow("request-changes", "x".repeat(2001));
  expect(api.workflow).not.toHaveBeenCalled();
  editor.formDirty("property-text", true);
  await editor.workflow("approve");
  expect(api.workflow).not.toHaveBeenCalled();
  editor.formDirty("property-text", false);
  let resolve!: (value: null) => void;
  vi.mocked(api.workflow).mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const pending = editor.workflow("approve");
  await editor.workflow("approve");
  await editor.retryWorkflow();
  expect(api.workflow).toHaveBeenCalledTimes(1);
  editor.dispose();
  resolve(null);
  await pending;
  expect(api.open).toHaveBeenCalledTimes(1);
});
it("STU-074: generic transport failure remains uncertain even if subsequent state can be read", async () => {
  const { api } = mockApi(),
    editor = createEditor(api);
  await editor.open(documentId, changesetId);
  vi.mocked(api.workflow).mockRejectedValueOnce(Error("secret"));
  await editor.workflow("submit");
  expect(editor.store.getState()).toMatchObject({
    status: "uncertain",
    error: "WORKFLOW_UNCERTAIN",
    workflowRetry: true,
  });
  await editor.retryWorkflow();
  expect(editor.store.getState().status).toBe("ready");
  editor.dispose();
});
