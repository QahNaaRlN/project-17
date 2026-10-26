// STU-445: deterministic GitHub facts for the §2 scenario. Serves only the
// read endpoints the CMS client uses for one repository and PR #1; the
// scenario moves the PR from open to merged through /control. Loopback only;
// only a CMS binary built with the e2e tag can reach it.
import { createServer } from "node:http";
import { URL } from "node:url";

const port = Number(process.env.E2E_GITHUB_PORT ?? 18197);
const control = process.env.E2E_GITHUB_CONTROL_TOKEN ?? "";
if (!Number.isInteger(port) || port < 1024 || port > 65535 || control.length < 16)
  throw Error("E2E_GITHUB_PORT and a 16+ character E2E_GITHUB_CONTROL_TOKEN are required");
const repo = { id: 42, full_name: "local/store" };
const sha = /^[a-f0-9]{40}$/;
const state = {
  merged: false,
  base: "b".repeat(40),
  head: "a".repeat(40),
  merge: "c".repeat(40),
  mergedAt: null,
};
const json = (res, value, status = 200) => {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(value));
};
const pull = () => ({
  id: 1001,
  number: 1,
  state: state.merged ? "closed" : "open",
  draft: false,
  merged: state.merged,
  mergeable: !state.merged,
  merge_commit_sha: state.merged ? state.merge : null,
  merged_at: state.mergedAt,
  head: { sha: state.head, ref: "quick-view", repo },
  base: { sha: state.base, ref: "dev", repo },
});
createServer(async (req, res) => {
  const url = new URL(req.url ?? "/", "http://127.0.0.1");
  console.log(req.method + " " + url.pathname);
  // Read the whole request first: closing an unread request resets the socket.
  let body = "";
  for await (const chunk of req) body += String(chunk);
  if (req.method === "GET" && url.pathname === "/healthz") return json(res, { ok: true });
  if (req.method === "POST" && url.pathname === "/control") {
    if (req.headers.authorization !== "Bearer " + control) return json(res, {}, 401);
    const next = JSON.parse(body);
    for (const key of ["base", "head", "merge"])
      if (next[key] !== undefined && !sha.test(next[key])) return json(res, {}, 400);
    Object.assign(state, {
      ...next,
      mergedAt: next.merged ? new Date().toISOString() : null,
    });
    return json(res, pull());
  }
  if (req.method !== "GET" || !req.headers.authorization?.startsWith("Bearer "))
    return json(res, { message: "Bad credentials" }, 401);
  const root = "/repos/local/store";
  if (url.pathname === root) return json(res, repo);
  if (url.pathname === root + "/pulls/1") return json(res, pull());
  const statuses = url.pathname.match(/^\/repos\/local\/store\/commits\/([a-f0-9]{40})\/statuses$/);
  if (statuses)
    return json(
      res,
      url.searchParams.get("page") === "1" && statuses[1] === state.head
        ? [
            {
              id: 1,
              context: "ci",
              state: "success",
              created_at: "2026-10-21T00:00:00Z",
              creator: { id: 1 },
            },
          ]
        : [],
    );
  const compare = url.pathname.match(
    /^\/repos\/local\/store\/compare\/([a-f0-9]{40})\.\.\.([a-f0-9]{40})$/,
  );
  // The scenario builds the merged commit itself: every build descends from it.
  if (compare)
    return json(res, {
      status: compare[1] === compare[2] ? "identical" : "ahead",
      ahead_by: compare[1] === compare[2] ? 0 : 1,
      base_commit: { sha: compare[1] },
      merge_base_commit: { sha: compare[1] },
    });
  return json(res, { message: "Not Found" }, 404);
}).listen(port, "127.0.0.1", () => console.log("GitHub fixture ready on " + port));
