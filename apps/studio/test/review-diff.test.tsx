// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PublishingPanel } from "../src/publishing.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, fixture, documentId, changesetId } from "./fixture.js";
import { createApi } from "../src/api.js";
import { validateDiffIndex, validateDiffPage } from "../src/review-diff.js";
import {
  StudioError,
  type Api,
  type DiffIndex,
  type DiffPage,
  type DiffValue,
} from "../src/types.js";
const hash = "sha256:" + "a".repeat(64);
const otherHash = "sha256:" + "b".repeat(64);
function index(): DiffIndex {
  return {
    changesetId,
    seq: 1,
    environment: "staging",
    targets: ["staging"],
    complete: true,
    snapshot: hash,
    objects: [documentId, changesetId].map((id, n) => ({
      id,
      kind: n ? "entity" : "document",
      ...(n ? {} : { docKind: "page" }),
      title: n ? "Article" : "Store",
      beforePath: n ? null : "/store",
      afterPath: n ? null : "/store",
      workingVersionId: id,
      baseVersionId: id,
      headVersionId: id,
      publishedVersionId: null,
      baseStale: false,
      summaries: {
        base: { effect: "modified", changeCount: 2 },
        published: { effect: "added", changeCount: 2 },
      },
    })),
  };
}
function page(objectId = documentId, baseline: "base" | "published" = "base"): DiffPage {
  return {
    changesetId,
    objectId,
    seq: 1,
    environment: "staging",
    snapshot: hash,
    baseline,
    totalChanges: 2,
    nextCursor: "next",
    items: [
      {
        key: "0",
        category: "property",
        kind: "replace",
        pointer: "/nodes/n_heading/props/text",
        nodeId: "n_heading",
        before: { present: true, value: "Before" },
        after: { present: true, value: "After" },
      },
    ],
  };
}
let root: Root, editor: Editor, client: QueryClient;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 30));
  });
}
async function setup(state = "approved") {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api, session } = mockApi();
  session.changeset.state = state;
  api.diffIndex = vi.fn(async () => index());
  api.diffPage = vi.fn<NonNullable<Api["diffPage"]>>(async (_s, _i, id, baseline, cursor) =>
    cursor
      ? {
          ...page(id, baseline),
          nextCursor: null,
          items: [
            {
              key: "1",
              category: "metadata",
              kind: "replace",
              pointer: "/@version/path",
              before: { present: false },
              after: { present: true, rawJson: "9007199254740993" },
            },
          ],
        }
      : page(id, baseline),
  );
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  const onContent = vi.fn();
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <PublishingPanel api={api} editor={editor} onContent={onContent} />
      </QueryClientProvider>,
    ),
  );
  await settle();
  await settle();
  return { api, session, onContent };
}
const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === name)!;
async function click(name: string) {
  await act(async () => button(name).click());
  await settle();
}
it("STU-149/153/157: whole-work index, both baselines, pagination, content navigation and protected explicit publication", async () => {
  const { api, onContent } = await setup();
  expect(document.querySelector(".review-diff")?.textContent).toContain("Before");
  expect(document.querySelector(".review-diff")?.textContent).toContain("After");
  expect(button("Опубликовать").disabled).toBe(false);
  await click("Ещё изменения");
  expect(document.body.textContent).toContain("9007199254740993");
  expect(document.body.textContent).toContain("2 / 2");
  await act(async () =>
    document.querySelector<HTMLButtonElement>(".diff-object-list button:last-child")!.click(),
  );
  await settle();
  await settle();
  await click("Открыть объект");
  expect(onContent).toHaveBeenCalledWith(changesetId, "entity");
  await click("Изменения в окружении · staging");
  await settle();
  expect(api.diffPage).toHaveBeenLastCalledWith(
    expect.anything(),
    expect.anything(),
    changesetId,
    "published",
    undefined,
    expect.any(AbortSignal),
  );
  await click("Опубликовать");
  expect(api.diffIndex).toHaveBeenCalledTimes(2);
  expect(api.workflow).not.toHaveBeenCalled();
  await click("Подтвердить публикацию");
  expect(api.workflow).toHaveBeenCalledWith(
    expect.objectContaining({ diffSnapshot: hash }),
    "publish",
    "",
    expect.any(String),
    expect.any(AbortSignal),
  );
});
it("STU-158: fresh publication re-read rejects a changed pointer hash", async () => {
  const { api } = await setup();
  vi.mocked(api.diffIndex!).mockResolvedValue({ ...index(), snapshot: otherHash });
  await click("Опубликовать");
  expect(document.querySelector(".publication-confirmation")).toBeNull();
  expect(api.workflow).not.toHaveBeenCalled();
  expect(button("Опубликовать").disabled).toBe(true);
  expect(document.body.textContent).toContain("Перечитайте сравнение");
});
it("STU-158: approval with the same sequence waits for its own comparison before publication", async () => {
  const { api } = await setup("in_review");
  let resolve!: (value: DiffIndex) => void;
  vi.mocked(api.diffIndex!).mockImplementationOnce(
    async () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  await act(async () => {
    const session = editor.store.getState().session!;
    editor.store.setState({
      session: {
        ...session,
        changeset: { ...session.changeset, state: "approved" },
      },
    });
  });
  await settle();
  expect(api.diffIndex).toHaveBeenCalledTimes(2);
  expect(button("Опубликовать").disabled).toBe(true);
  expect(document.querySelector(".publication-confirmation")).toBeNull();
  vi.mocked(api.diffPage!).mockResolvedValue({ ...page(), snapshot: otherHash });
  vi.mocked(api.diffIndex!).mockResolvedValue({ ...index(), snapshot: otherHash });
  await act(async () => resolve({ ...index(), snapshot: otherHash }));
  await settle();
  await settle();
  expect(button("Опубликовать").disabled).toBe(false);
  await click("Опубликовать");
  expect(button("Подтвердить публикацию").disabled).toBe(false);
  expect(api.workflow).not.toHaveBeenCalled();
  await click("Подтвердить публикацию");
  expect(api.workflow).toHaveBeenCalledWith(
    expect.objectContaining({ diffSnapshot: otherHash }),
    "publish",
    "",
    expect.any(String),
    expect.any(AbortSignal),
  );
});
it("STU-158: a late publication read cannot confirm a different work; dirty input and errors block decisions", async () => {
  const { api } = await setup();
  let resolve!: (v: DiffIndex) => void;
  vi.mocked(api.diffIndex!).mockImplementationOnce(
    async () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  await act(async () => button("Опубликовать").click());
  await act(async () =>
    editor.store.setState({
      session: {
        ...editor.store.getState().session!,
        changeset: { ...editor.store.getState().session!.changeset, id: documentId },
      },
    }),
  );
  await act(async () => resolve(index()));
  await settle();
  expect(document.querySelector(".publication-confirmation")).toBeNull();
  expect(api.workflow).not.toHaveBeenCalled();
});
it("STU-158: detail failures and unsaved values disable approval, refresh restores the readable comparison", async () => {
  const { api } = await setup("in_review");
  await act(async () => editor.store.setState({ dirtyFields: { x: true } }));
  expect(button("Согласовать").disabled).toBe(true);
  await act(async () => editor.store.setState({ dirtyFields: {} }));
  await settle();
  vi.mocked(api.diffPage!).mockRejectedValue(new StudioError("DIFF_LIMIT_EXCEEDED"));
  await act(async () => client.invalidateQueries({ queryKey: ["studio-diff-page"] }));
  await settle();
  expect(document.body.textContent).toContain("превышен лимит");
  expect(button("Согласовать").disabled).toBe(true);
  expect(api.workflow).not.toHaveBeenCalled();
});
it("STU-154/155: validators preserve null, absence and precise raw numbers and reject incomplete/ambiguous responses", () => {
  expect(validateDiffIndex(index(), fixture())).toEqual(index());
  for (const mutate of [
    (v: DiffIndex) => {
      v.complete = false as true;
    },
    (v: DiffIndex) => {
      v.targets = [];
    },
    (v: DiffIndex) => {
      v.objects.push(v.objects[0]!);
    },
    (v: DiffIndex) => {
      v.objects[0]!.baseVersionId = "bad";
    },
  ]) {
    const v = index();
    mutate(v);
    expect(() => validateDiffIndex(v, fixture())).toThrow("RESPONSE_INVALID");
  }
  expect(() => validateDiffIndex({ ...index(), seq: 2 }, fixture())).toThrow("DIFF_SNAPSHOT_STALE");
  const values: DiffValue[] = [
    { present: false },
    { present: true, value: null },
    { present: true, value: false },
    { present: true, rawJson: "9007199254740993" },
    { present: true, redacted: true, reason: "service-field" },
  ];
  for (const value of values) {
    const v = page();
    v.items[0]!.after = value;
    expect(validateDiffPage(v, index(), documentId, "base")).toEqual(v);
  }
  for (const value of [
    { present: false, value: 1 },
    { present: true, rawJson: 2 },
    { present: true },
    { present: true, redacted: true, reason: "other" },
    { present: true, value: null, rawJson: "null" },
  ]) {
    const v = page();
    v.items[0]!.after = value as DiffValue;
    expect(() => validateDiffPage(v, index(), documentId, "base")).toThrow("RESPONSE_INVALID");
  }
  for (const mutate of [
    (v: DiffPage) => {
      v.items = [];
    },
    (v: DiffPage) => {
      v.items[0]!.key = "999";
    },
    (v: DiffPage) => {
      v.baseline = "published";
    },
    (v: DiffPage) => {
      v.items[0]!.pointer = "bad";
    },
    (v: DiffPage) => {
      v.nextCursor = "x".repeat(2049);
    },
    (v: DiffPage) => {
      v.totalChanges = 3;
    },
  ]) {
    const v = page();
    mutate(v);
    expect(() => validateDiffPage(v, index(), documentId, "base")).toThrow("RESPONSE_INVALID");
  }
});
it("STU-156/158: API scopes index/detail and sends snapshot only with protected actions, rejecting cursor loops", async () => {
  const fetcher = vi.fn<typeof fetch>(async (input) =>
    Response.json(
      String(input).includes("/diff/")
        ? page()
        : String(input).includes("/diff?")
          ? index()
          : { result: { changeset: { ...fixture().changeset, state: "approved" } } },
    ),
  );
  const api = createApi(fetcher),
    s = fixture();
  const i = await api.diffIndex!(s);
  await api.diffPage!(s, i, documentId, "base");
  expect(String(fetcher.mock.calls[1]![0])).toContain("snapshot=sha256%3A");
  await expect(api.diffPage!(s, i, documentId, "base", "next")).rejects.toThrow("RESPONSE_INVALID");
  await api.workflow(
    { ...s, diffSnapshot: hash },
    "approve",
    "",
    "key",
    new AbortController().signal,
  );
  expect(JSON.parse(String(fetcher.mock.calls.at(-1)![1]!.body)).payload.expectedDiffSnapshot).toBe(
    hash,
  );
});
it("STU-157: tombstone metadata remains readable without navigation even when both versions are deleted", async () => {
  const { api } = await setup();
  const tombstone = index();
  tombstone.objects[0]!.workingDeleted = true;
  tombstone.objects[0]!.summaries.base.effect = "metadata-only";
  vi.mocked(api.diffIndex!).mockResolvedValue(tombstone);
  await act(async () => client.invalidateQueries({ queryKey: ["studio-diff"] }));
  await settle();
  await settle();
  expect(document.querySelector(".review-diff")?.textContent).toContain("After");
  expect(button("Открыть объект")).toBeUndefined();
  expect(button("Выбрать блок")).toBeUndefined();
});
it("STU-155/159: revoked read access hides cached index and before/after values", async () => {
  const { api } = await setup("in_review");
  vi.mocked(api.diffIndex!).mockRejectedValue(new StudioError("FORBIDDEN"));
  await act(async () => client.invalidateQueries({ queryKey: ["studio-diff"] }));
  await settle();
  expect(document.querySelector(".review-diff")?.textContent).not.toContain("Before");
  expect(document.querySelector(".review-diff")?.textContent).not.toContain("Store");
  expect(button("Согласовать").disabled).toBe(true);
  vi.mocked(api.diffIndex!).mockResolvedValue(index());
  await act(async () => client.invalidateQueries({ queryKey: ["studio-diff"] }));
  await settle();
  await settle();
  vi.mocked(api.diffPage!).mockRejectedValue(new StudioError("FORBIDDEN"));
  await act(async () => client.invalidateQueries({ queryKey: ["studio-diff-page"] }));
  await settle();
  expect(document.querySelector(".review-diff")?.textContent).not.toContain("Before");
  expect(button("Согласовать").disabled).toBe(true);
});
