import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import {
  githubView,
  githubConfig,
  githubLabel,
  githubMessage,
  type GithubView,
  type GithubConfig,
} from "../src/capability-github.js";
import { documentId } from "./fixture.js";
it("STU-310: validates safe projections and exact environment-pinned commands", async () => {
  const config = { configured: true, revision: 1, ready: true, enabled: true, configId: "repo" };
  const view: GithubView = {
    link: null,
    current: null,
    lastKnown: null,
    syncState: "unlinked",
    canManage: true,
  };
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const p = String(input);
    if (p.endsWith("/config"))
      return Response.json({
        project: "store",
        environment: "staging",
        appOrigin: "http://127.0.0.1:18083",
      });
    if (p.includes("/auth/session"))
      return Response.json({
        authenticated: true,
        user: { id: documentId, name: "Engineer" },
        rights: ["code.propose"],
        csrfToken: "c".repeat(43),
        expiresAt: "2030-01-01T00:00:00Z",
        hasAccess: true,
      });
    if (p.includes("/commands/set-")) return Response.json({ result: config });
    if (p.includes("/commands/"))
      return Response.json({ result: { revision: 1, syncId: documentId } });
    if (p.includes("github-observations")) return Response.json({ items: [], nextCursor: null });
    if (p.includes("capability-github-config")) return Response.json(config);
    return Response.json(view);
  });
  const api = createApi(fetcher);
  expect(await api.githubConfig!()).toEqual(config);
  expect(await api.github!(documentId)).toEqual(view);
  expect((await api.githubHistory!(documentId, "next")).items).toEqual([]);
  const attempt = {
    name: "link-capability-github-pr" as const,
    payload: { requestId: documentId, pullNumber: 1 },
    key: "frozen",
    reason: "Link explicitly",
    actor: documentId,
  };
  await api.githubCommand!(attempt);
  await api.githubCommand!(attempt);
  const calls = fetcher.mock.calls.filter(([p]) => String(p).includes("/commands/"));
  expect(calls[0]).toEqual(calls[1]);
  await api.githubCommand!({ ...attempt, name: "set-capability-github-config" });
  await expect(api.github!("bad")).rejects.toThrow();
  await expect(api.githubHistory!("bad")).rejects.toThrow();
  await expect(api.githubCommand!({ ...attempt, actor: "different" })).rejects.toMatchObject({
    code: "SESSION_CHANGED",
    uncertain: true,
  });
});
it("STU-310: malformed data and unsafe URLs cannot become displayed facts", () => {
  for (const bad of [
    null,
    {},
    { configured: true, revision: 0, ready: true },
    { configured: true, revision: 1, ready: true, configId: "x" },
  ])
    expect(() => githubConfig(bad as GithubConfig)).toThrow();
  const view: GithubView = {
    link: null,
    current: null,
    lastKnown: null,
    syncState: "idle",
    canManage: false,
  };
  expect(githubView(view)).toEqual(view);
  for (const bad of [
    null,
    {},
    { ...view, url: "javascript:alert(1)" },
    { ...view, link: { revision: 0 } },
    { ...view, current: { observedAt: "bad" } },
  ])
    expect(() => githubView(bad as GithubView)).toThrow();
  for (const code of [
    "RESPONSE_INVALID",
    "CAPABILITY_GITHUB_CONFIG_CHANGED",
    "CAPABILITY_GITHUB_COOLDOWN",
    "CAPABILITY_GITHUB_FORBIDDEN",
    "CAPABILITY_GITHUB_UNAVAILABLE",
  ])
    expect(githubMessage(code, true)).not.toBe(githubMessage(code, false));
  for (const state of [
    "success",
    "failure",
    "pending",
    "cancelled",
    "unknown",
    "open",
    "closed",
    "merged",
    "queued",
    "sending",
    "retry_wait",
    "idle",
    "stopped",
    "stale",
    "disabled",
    "unlinked",
    "test_merge",
    "merged_commit",
    "unavailable",
    "head",
    "integration",
    "other",
  ])
    expect(githubLabel(state, true)).not.toBe(githubLabel(state, false));
});
