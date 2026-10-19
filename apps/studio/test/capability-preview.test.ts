import { afterEach, expect, it, vi } from "vitest";
import {
  previewAge,
  previewArchiveLabel,
  previewLabel,
  previewList,
  previewMessage,
  previewOrigin,
  previewStates,
  type PreviewList,
} from "../src/capability-preview.js";
import { previewItem } from "./preview-fixture.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError } from "../src/types.js";

const editors: Editor[] = [];
afterEach(() => {
  editors.forEach((e) => e.dispose());
  editors.length = 0;
});

it("STU-319: validates the preview projection strictly", () => {
  expect(previewList(previewItem())).toEqual(previewItem());
  const broken: ((v: PreviewList) => void)[] = [
    (v) => (v.items[0]!.environment = "staging"),
    (v) => ((v.items[0]!.state as string) = "fulfilled"),
    (v) => (v.items[0]!.registration!.commit = "abc1234"),
    (v) => (v.items[0]!.registration!.seq = 0),
    (v) => (v.items[0]!.registration!.manifestHash = "x"),
    (v) => (v.items[0]!.registration!.registeredAt = "never"),
    (v) => ((v.items[0]!.registration!.appUrl as unknown) = 7),
    (v) => (v.items[0]!.observation!.headSha = "zz"),
    (v) => (v.items[0]!.observation!.freshUntil = "never"),
    (v) => ((v.items[0]!.args.status as string) = "done"),
    (v) => ((v.items[0]!.args.revision as unknown) = "1"),
    (v) => ((v.items[0]!.keyAvailable as unknown) = "yes"),
    (v) => ((v.items[0]!.pullNumber as unknown) = 1.5),
    (v) => ((v.canOpen as unknown) = 1),
    (v) => ((v.canSetArgs as unknown) = 1),
    (v) => (v.items = Array.from({ length: 11 }, () => v.items[0]!)),
    (v) => ((v.items as unknown) = null),
    (v) => ((v.items[0] as unknown) = null),
  ];
  for (const change of broken) {
    const v = previewItem();
    change(v);
    expect(() => previewList(v)).toThrowError(StudioError);
  }
  expect(() => previewList(null as unknown as PreviewList)).toThrowError(StudioError);
  const empty = previewItem();
  empty.items[0]!.registration = null;
  empty.items[0]!.observation = null;
  expect(previewList(empty).items[0]!.registration).toBeNull();
});

it("STU-328: opens only exact allowed origins", () => {
  const allowed = ["https://pr.example.test"];
  expect(previewOrigin("https://pr.example.test/pr-1", allowed)).toBe("https://pr.example.test");
  expect(previewOrigin("https://other.example.test/", allowed)).toBeNull();
  expect(previewOrigin("https://pr.example.test/?x=1", allowed)).toBeNull();
  expect(previewOrigin("https://u:p@pr.example.test/", allowed)).toBeNull();
  expect(previewOrigin("not a url", allowed)).toBeNull();
  expect(previewOrigin("https://pr.example.test/", undefined)).toBeNull();
  expect(previewOrigin(null, allowed)).toBeNull();
});

it("§11.2: shows the observation age and every state in RU/EN", () => {
  const now = Date.parse("2026-10-19T12:00:00Z");
  expect(previewAge("2026-10-19T11:30:00Z", now, true)).toBe("30 мин");
  expect(previewAge("2026-10-19T10:00:00Z", now, false)).toBe("2 h");
  expect(previewAge("2026-10-16T12:00:00Z", now, true)).toBe("3 дн");
  expect(previewAge("2026-10-19T13:00:00Z", now, false)).toBe("0 min");
  for (const state of previewStates) {
    expect(previewLabel(state, true)).not.toBe("Недоступно");
    expect(previewLabel(state, false)).not.toBe("Unavailable");
  }
  expect(previewLabel("other", true)).toBe("Недоступно");
  expect(previewLabel("other", false)).toBe("Unavailable");
  for (const r of ["deleted", "ttl", "pr_closed", "pr_merged"]) {
    expect(previewArchiveLabel(r, true)).not.toBe("");
    expect(previewArchiveLabel(r, false)).not.toBe("");
  }
  expect(previewArchiveLabel(null, true)).toBe("");
  for (const code of [
    "PREVIEW_READ_ONLY",
    "PREVIEW_OUTDATED",
    "PREVIEW_ARGS_INVALID",
    "PREVIEW_NOT_READY",
    "CAPABILITY_REVISION_CONFLICT",
    "FORBIDDEN",
    "OTHER",
  ])
    expect(previewMessage(code, true)).not.toBe(previewMessage(code, false));
  expect(previewMessage("PREVIEW_ARGS_STALE", true)).toBe(previewMessage("PREVIEW_OUTDATED", true));
});

it("STU-329/330: PR preview is read-only, bound to its build and origin", async () => {
  const { api, session } = mockApi();
  const editor = createEditor(api);
  editors.push(editor);
  await editor.open(documentId, changesetId);
  const frame = { postMessage: vi.fn() };
  editor.bindFrame(frame);
  api.preview = vi.fn(async () => ({
    token: "pr-token",
    expiresAt: new Date(Date.now() + 900_000).toISOString(),
    registrationSeq: 2,
  }));
  const target = {
    environment: "preview/pr-1",
    origin: "https://pr.example.test",
    pullNumber: 1,
    registrationSeq: 2,
    commit: "a".repeat(40),
  };
  editor.formDirty("json", true);
  await editor.viewEnvironment(target);
  expect(editor.store.getState()).toMatchObject({ viewTarget: null, error: "FIELD_DRAFT_PENDING" });
  editor.formDirty("json", false);
  await editor.viewEnvironment(target);
  const state = editor.store.getState();
  expect(api.preview).toHaveBeenLastCalledWith(
    expect.anything(),
    expect.anything(),
    "preview/pr-1",
  );
  expect(state.previewSrc.startsWith("https://pr.example.test/api/cms/preview?")).toBe(true);
  expect(state).toMatchObject({ viewTarget: target, mode: "interact" });

  // Only the preview deployment's origin may talk to Studio.
  editor.bindFrame(frame);
  const receive = (origin: string) =>
    editor.receive({
      origin,
      source: frame,
      data: {
        cms: "preview",
        v: 1,
        type: "ready",
        id: "a",
        payload: { documentId, protocolVersion: 1 },
      },
    });
  receive(session.config.appOrigin);
  expect(frame.postMessage).not.toHaveBeenCalled();
  receive("https://pr.example.test");
  expect(frame.postMessage).toHaveBeenCalledWith(
    expect.objectContaining({ type: "init" }),
    "https://pr.example.test",
  );

  await editor.apply([
    { type: "node.setDesign", target: documentId, payload: { nodeId: "n_root", set: {} } },
  ]);
  await editor.applyBindings([]);
  expect(await editor.applyContent([], 1)).toBeNull();
  await editor.changeHistory("undo");
  await editor.workflow("submit");
  expect(api.apply).not.toHaveBeenCalled();
  expect(api.workflow).not.toHaveBeenCalled();
  expect(api.changeHistory).not.toHaveBeenCalled();
  expect(editor.store.getState().error).toBe("PREVIEW_READ_ONLY");

  // A token for another build is refused (STU-329).
  api.preview = vi.fn(async () => ({
    token: "old",
    expiresAt: new Date(Date.now() + 900_000).toISOString(),
    registrationSeq: 3,
  }));
  await editor.viewEnvironment(target);
  expect(editor.store.getState().previewError).toBe("PREVIEW_OUTDATED");

  api.preview = vi.fn(async () => ({
    token: "staging",
    expiresAt: new Date(Date.now() + 900_000).toISOString(),
  }));
  await editor.viewEnvironment(null);
  expect(api.preview).toHaveBeenLastCalledWith(expect.anything(), expect.anything(), undefined);
  expect(editor.store.getState().previewSrc.startsWith(session.config.appOrigin)).toBe(true);
  await editor.open(documentId, changesetId);
  expect(editor.store.getState().viewTarget).toBeNull();
});
