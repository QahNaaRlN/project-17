import { expect, it, vi } from "vitest";
import { createApi } from "../src/api.js";
import { fixture, documentId, changesetId } from "./fixture.js";
const account = {
  authenticated: true,
  user: { id: documentId, name: "Alice" },
  csrfToken: "a".repeat(43),
  rights: ["design.read", "content.read"],
  hasAccess: true,
  expiresAt: "2099-01-01T00:00:00Z",
};
it("STU-081/082: commands recheck the actor and carry session-bound CSRF, never bearer credentials", async () => {
  const s = fixture();
  const fetcher = vi.fn<typeof fetch>(async (input) =>
    Response.json(
      String(input).endsWith("/config")
        ? { ...s.config, authMode: "oidc" }
        : String(input).endsWith("/auth/session")
          ? account
          : { ok: true },
    ),
  );
  const api = createApi(fetcher);
  await api.config();
  expect(await api.account()).toEqual(account);
  await api.apply(s, [], "frozen", new AbortController().signal);
  expect(fetcher.mock.calls.at(-1)![1]!.headers).toMatchObject({
    "X-CSRF-Token": account.csrfToken,
    "Idempotency-Key": "frozen",
  });
  expect(JSON.stringify(fetcher.mock.calls)).not.toContain("Bearer");
  await api.logout();
  expect(String(fetcher.mock.calls.at(-1)![0])).toContain("/auth/logout");
});
it("STU-082: a different or missing actor cannot replay an old editor command", async () => {
  let user = { ...account },
    writes = 0;
  const lost = vi.fn();
  const api = createApi(async (input, init) => {
    if (init?.method === "POST") writes++;
    return Response.json(
      String(input).endsWith("/config") ? { ...fixture().config, authMode: "oidc" } : user,
    );
  }, lost);
  await api.config();
  await api.account();
  user = { ...account, user: { id: changesetId, name: "Bob" }, csrfToken: "b".repeat(43) };
  await expect(api.apply(fixture(), [], "k", new AbortController().signal)).rejects.toThrow(
    "SESSION_CHANGED",
  );
  expect(writes).toBe(0);
  expect(lost).toHaveBeenCalled();
  user = { ...account, authenticated: false };
  await expect(api.apply(fixture(), [], "k", new AbortController().signal)).rejects.toThrow(
    "UNAUTHENTICATED",
  );
  expect(writes).toBe(0);
});
it.each([
  null,
  {},
  { authenticated: true, rights: [], hasAccess: true },
  { ...account, csrfToken: "bad" },
  { ...account, expiresAt: "bad" },
  { ...account, rights: [1] },
])("STU-082: malformed identity is rejected", async (body) => {
  await expect(createApi(async () => Response.json(body)).account()).rejects.toThrow(
    "SESSION_INVALID",
  );
});
it("STU-083: work catalogue validates state, identity and cursor", async () => {
  const item = {
    id: changesetId,
    documentId,
    path: "/store",
    title: "Work",
    ownerId: documentId,
    ownerName: "Alice",
    own: false,
    state: "in_review",
  };
  let data: unknown = { items: [item], nextCursor: changesetId };
  const api = createApi(async (input) =>
    Response.json(String(input).endsWith("/config") ? fixture().config : data),
  );
  expect((await api.workItems()).items).toEqual([item]);
  data = { items: [{ ...item, state: "merged" }], nextCursor: null };
  await expect(api.workItems()).rejects.toThrow("RESPONSE_INVALID");
  await expect(api.workItems("bad")).rejects.toThrow("IDENTIFIER_INVALID");
});
it("STU-082: expired authenticated HTTP response signals teardown", async () => {
  const lost = vi.fn();
  const api = createApi(
    async (input) =>
      String(input).endsWith("/config")
        ? Response.json({ ...fixture().config, authMode: "oidc" })
        : Response.json({ code: "UNAUTHENTICATED" }, { status: 401 }),
    lost,
  );
  await api.config();
  await expect(api.account()).rejects.toThrow("UNAUTHENTICATED");
  expect(lost).toHaveBeenCalled();
});
it("STU-082: session preflight failure keeps a frozen command uncertain without posting it", async () => {
  let unavailable = false,
    writes = 0;
  const api = createApi(async (input, init) => {
    if (init?.method === "POST") writes++;
    if (String(input).endsWith("/config"))
      return Response.json({ ...fixture().config, authMode: "oidc" });
    if (unavailable) throw new Error("network failure");
    return Response.json(account);
  });
  await api.config();
  await api.account();
  unavailable = true;
  await expect(
    api.apply(fixture(), [], "frozen", new AbortController().signal),
  ).rejects.toMatchObject({ uncertain: true });
  expect(writes).toBe(0);
});
