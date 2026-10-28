/* eslint-disable @typescript-eslint/no-explicit-any -- untyped JSON of the public API, checked by assertions */
// Helpers of the §2 scenario (STU-440…459): people, public commands, the
// scripted MCP agent, time metrics and per-step evidence.
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import type { Stand } from "../../../../scripts/e2e-stand.mjs";

export interface Person {
  name: string;
  context: BrowserContext;
  page: Page;
  id: string;
  csrf: string;
}

/** Signs a person in through the Studio OIDC form and binds their roles. */
export async function signIn(
  stand: Stand,
  browser: Browser,
  user: "alice" | "bob" | "erin",
  roles: string[],
): Promise<Person> {
  const name = { alice: "Alice", bob: "Bob", erin: "Erin" }[user];
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1100 },
    // STU-458: every person's session is recorded for the evidence artifact. The load bench
    // turns recording off (E2E_VIDEO=off): encoding video takes CPU from the measured stand.
    ...(process.env.E2E_DIR && process.env.E2E_VIDEO !== "off"
      ? { recordVideo: { dir: join(process.env.E2E_DIR, "video") } }
      : {}),
  });
  const page = await context.newPage();
  page.on("pageerror", (error) => console.error(name, "page error:", error.message));
  await page.goto(stand.studioOrigin + "/editor");
  await page.getByRole("link", { name: "Войти", exact: true }).click();
  await page.getByRole("link", { name: "Войти как " + name, exact: true }).click();
  await page.waitForURL(stand.studioOrigin + "/editor**");
  const session = async () =>
    (await (await context.request.get(stand.studioOrigin + "/api/studio/auth/session")).json()) as {
      user: { id: string };
      csrfToken: string;
    };
  const first = await session();
  for (const roleId of roles)
    await admin(stand, "bind-human-role", { actorId: first.user.id, roleId });
  await page.reload({ waitUntil: "domcontentloaded" });
  const account = await session();
  return { name, context, page, id: account.user.id, csrf: account.csrfToken };
}

export class CommandError extends Error {
  constructor(
    public status: number,
    public code: string,
    public params: Record<string, unknown> = {},
  ) {
    super(`${status} ${code} ${JSON.stringify(params).slice(0, 1500)}`);
  }
}

async function result(
  response: { ok: boolean; status: number; json(): Promise<unknown> },
  name: string,
) {
  const data = (await response.json()) as {
    result?: unknown;
    code?: string;
    params?: Record<string, unknown>;
  };
  if (!response.ok) throw new CommandError(response.status, data.code ?? "UNKNOWN", data.params);
  void name;
  return data.result;
}

/** A command of the stand's service actor: setup only (STU-443). */
export async function admin(stand: Stand, name: string, payload: unknown): Promise<any> {
  const response = await fetch(stand.apiOrigin + "/api/v1/commands/" + name, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + stand.credential,
      "X-CMS-Project": stand.project,
      "Content-Type": "application/json",
      "Idempotency-Key": randomUUID(),
    },
    body: JSON.stringify({ payload, reason: "§2 scenario setup" }),
  });
  return result(response, name);
}

export async function adminRead(stand: Stand, path: string): Promise<any> {
  const response = await fetch(stand.apiOrigin + "/api/v1" + path, {
    headers: { Authorization: "Bearer " + stand.credential, "X-CMS-Project": stand.project },
  });
  if (!response.ok) throw new CommandError(response.status, "READ_FAILED");
  return response.json();
}

/** A person's command through the public command API with their Studio session. */
export async function command(
  stand: Stand,
  person: Person,
  name: string,
  payload: unknown,
  reason = "§2 scenario",
): Promise<any> {
  const response = await person.context.request.post(stand.apiOrigin + "/api/v1/commands/" + name, {
    headers: {
      "X-CMS-Project": stand.project,
      Origin: stand.studioOrigin,
      "X-CSRF-Token": person.csrf,
      "Idempotency-Key": randomUUID(),
    },
    data: { payload, reason },
  });
  return result(
    { ok: response.ok(), status: response.status(), json: () => response.json() },
    name,
  );
}

export async function read(stand: Stand, person: Person, path: string): Promise<any> {
  const response = await person.context.request.get(stand.apiOrigin + "/api/v1" + path, {
    headers: { "X-CMS-Project": stand.project },
  });
  if (!response.ok()) throw new CommandError(response.status(), "READ_FAILED");
  return response.json();
}

/** The scripted agent of step 5: a plain MCP client, no language model (STU-444). */
export function mcpAgent(stand: Stand, token: string) {
  let id = 0;
  return async (name: string, args: unknown) => {
    const response = await fetch(stand.apiOrigin + "/mcp", {
      method: "POST",
      headers: {
        Authorization: "Bearer " + token,
        "X-CMS-Project": stand.project,
        Accept: "application/json, text/event-stream",
        "Content-Type": "application/json",
        "MCP-Protocol-Version": "2025-06-18",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: ++id,
        method: "tools/call",
        params: { name, arguments: args },
      }),
    });
    expect(response.status).toBe(200);
    const body = (await response.json()) as {
      result?: { isError: boolean; structuredContent: any };
      error?: { message: string };
    };
    return body;
  };
}

/** Time samples of §6; gates fail only as the spec decides (STU-454). */
export class Metrics {
  samples: Record<string, number[]> = {};
  add(name: string, ms: number) {
    (this.samples[name] ??= []).push(Math.round(ms));
  }
  p95(name: string) {
    const values = [...(this.samples[name] ?? [])].sort((a, b) => a - b);
    return values[Math.min(values.length - 1, Math.ceil(values.length * 0.95) - 1)] ?? NaN;
  }
  write(directory: string) {
    const summary = Object.fromEntries(
      Object.entries(this.samples).map(([k, v]) => [
        k,
        { samples: v, p95: this.p95(k), max: Math.max(...v) },
      ]),
    );
    writeFileSync(join(directory, "e2e-metrics.json"), JSON.stringify(summary, null, 2));
    return summary;
  }
}

/** Measures from start() to the moment the condition becomes true. */
export async function until(condition: () => Promise<boolean>, timeout: number) {
  const started = performance.now();
  await expect.poll(condition, { timeout, intervals: [25] }).toBe(true);
  return performance.now() - started;
}
