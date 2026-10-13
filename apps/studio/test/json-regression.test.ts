import { expect, it } from "vitest";
import { createEditor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
it("STU-091: optimistic local text preserves translations outside the edited locale", async () => {
  const { api, session } = mockApi();
  session.record.body.localContent = {
    t_title: { type: "text", value: { ru: "old", en: "keep" } },
  };
  let complete!: () => void;
  api.apply = () =>
    new Promise<void>((resolve) => {
      complete = resolve;
    });
  const editor = createEditor(api);
  await editor.open(documentId, changesetId);
  const pending = editor.apply([
    {
      type: "localContent.set",
      target: documentId,
      payload: { key: "t_title", type: "text", locale: "ru", value: "new" },
    },
  ]);
  expect(editor.store.getState().document?.localContent?.t_title?.value).toEqual({
    ru: "new",
    en: "keep",
  });
  complete();
  await pending;
  editor.dispose();
});
