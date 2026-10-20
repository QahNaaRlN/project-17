import type { PreviewList } from "../src/capability-preview.js";

export const previewItem = (): PreviewList => ({
  canOpen: true,
  canSetArgs: true,
  items: [
    {
      environment: "preview/pr-1",
      pullNumber: 1,
      state: "ready",
      expiresAt: "2026-11-02T12:00:00Z",
      archivedAt: null,
      archiveReason: null,
      keyAvailable: true,
      registration: {
        seq: 1,
        commit: "a".repeat(40),
        registeredAt: "2026-10-19T12:00:00Z",
        appUrl: "https://pr.example.test/pr-1",
        manifestHash: "sha256:" + "b".repeat(64),
      },
      observation: {
        headSha: "a".repeat(40),
        observedAt: "2026-10-19T12:00:00Z",
        freshUntil: "2026-10-19T12:15:00Z",
      },
      actionArgs: { value: { type: "string", required: true } },
      args: { status: "missing", revision: 0 },
    },
  ],
});
