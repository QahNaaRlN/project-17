import { StudioError } from "./types.js";

// Conditional agent policy v1 (STU-420…435): the document, its versions and
// the dry-run explanation. The server validates; Studio checks shapes only.
export const conditionKeys = [
  "environment",
  "pathPrefix",
  "zoneMode",
  "zoneId",
  "operationType",
  "schema",
  "risk",
  "source",
] as const;
export type ConditionKey = (typeof conditionKeys)[number];
export type When = Partial<Record<ConditionKey, string[]>>;
export const effects = ["allow", "deny", "requireApproval"] as const;
export type Effect = (typeof effects)[number];
export const zoneModes = ["STRICT", "SYSTEM", "FREE", "CODE"];
export const risks = ["low", "medium", "high"];
export const sources = ["studio", "api", "agent", "migration", "import"];

export interface PolicyRule {
  id: string;
  subject: { agent: string };
  allow?: string[];
  deny?: string[];
  requireApproval?: string[];
  when?: When;
}
export interface RiskRule {
  id: string;
  when?: When;
  min: string;
}
export interface ApprovalRule {
  id: string;
  when?: When;
  require: { count: number; roles?: string[] };
}
export interface PolicyDocument {
  version: 1;
  rules: PolicyRule[];
  risk: RiskRule[];
  approvals: ApprovalRule[];
}
export interface PolicyVersion {
  version: number;
  hash: string;
  policy: PolicyDocument;
  authorId: string | null;
  authorName: string;
  reason: string;
  createdAt: string;
}
export interface PolicyKnown {
  rights: string[];
  environments: string[];
  operationTypes: string[];
  schemas: string[];
  agents: string[];
}
export interface PolicyState extends PolicyVersion {
  known: PolicyKnown;
}
export interface Situation {
  agent: string;
  right: string;
  environment: string;
  path: string;
  zoneMode: string;
  zoneId: string;
  operationType: string;
  schema: string;
  risk: string;
}
export interface Explanation {
  decision: {
    allowed: boolean;
    approval?: boolean;
    ruleId?: string;
    condition?: string;
    reason?: string;
  };
  risk?: string;
  riskRule?: string;
  approvals: number;
  approvalRoles: string[];
  approvalRules: string[];
  version: number;
}
export interface PolicyAttempt {
  payload: { policy: PolicyDocument; expectedVersion: number };
  key: string;
  reason: string;
  actor?: string;
}

const strings = (v: unknown, max = 200) =>
  Array.isArray(v) && v.length <= max && v.every((s) => typeof s === "string" && s.length <= 300);
const when = (v: unknown) =>
  v === undefined ||
  (!!v &&
    typeof v === "object" &&
    Object.entries(v as object).every(
      ([k, values]) => (conditionKeys as readonly string[]).includes(k) && strings(values, 50),
    ));

export function emptyPolicy(): PolicyDocument {
  return { version: 1, rules: [], risk: [], approvals: [] };
}

/** Validates the server document shape; arrays absent in JSON become empty. */
export function policyDocument(value: unknown, uncertain = false): PolicyDocument {
  const d = (value ?? {}) as Record<string, unknown>;
  const rules = (d.rules ?? []) as unknown[],
    risk = (d.risk ?? []) as unknown[],
    approvals = (d.approvals ?? []) as unknown[];
  const ok =
    d.version === 1 &&
    Array.isArray(rules) &&
    Array.isArray(risk) &&
    Array.isArray(approvals) &&
    rules.every((r) => {
      const x = r as PolicyRule;
      return (
        !!x &&
        typeof x.id === "string" &&
        typeof x.subject?.agent === "string" &&
        effects.every((e) => x[e] === undefined || strings(x[e])) &&
        when(x.when)
      );
    }) &&
    risk.every((r) => {
      const x = r as RiskRule;
      return !!x && typeof x.id === "string" && typeof x.min === "string" && when(x.when);
    }) &&
    approvals.every((r) => {
      const x = r as ApprovalRule;
      return (
        !!x &&
        typeof x.id === "string" &&
        Number.isSafeInteger(x.require?.count) &&
        (x.require.roles === undefined || strings(x.require.roles)) &&
        when(x.when)
      );
    });
  if (!ok) throw new StudioError("RESPONSE_INVALID", uncertain);
  return {
    version: 1,
    rules: rules as PolicyRule[],
    risk: risk as RiskRule[],
    approvals: approvals as ApprovalRule[],
  };
}

export function policyVersion(value: unknown, uncertain = false): PolicyVersion {
  const v = (value ?? {}) as PolicyVersion;
  if (
    !Number.isSafeInteger(v.version) ||
    v.version < 0 ||
    typeof v.hash !== "string" ||
    (v.authorId !== null && v.authorId !== undefined && typeof v.authorId !== "string") ||
    (v.authorName !== undefined && typeof v.authorName !== "string") ||
    (v.reason !== undefined && typeof v.reason !== "string") ||
    (v.createdAt !== undefined && typeof v.createdAt !== "string")
  )
    throw new StudioError("RESPONSE_INVALID", uncertain);
  return {
    version: v.version,
    hash: v.hash,
    policy: policyDocument(v.policy, uncertain),
    authorId: v.authorId ?? null,
    authorName: v.authorName ?? "",
    reason: v.reason ?? "",
    createdAt: v.createdAt ?? "",
  };
}

export function policyState(value: unknown): PolicyState {
  const k = ((value ?? {}) as PolicyState).known;
  if (
    !k ||
    !strings(k.rights) ||
    !strings(k.environments) ||
    !strings(k.operationTypes) ||
    !strings(k.schemas) ||
    !strings(k.agents)
  )
    throw new StudioError("RESPONSE_INVALID");
  return { ...policyVersion(value), known: k };
}

export function explanation(value: unknown): Explanation {
  const v = (value ?? {}) as Explanation;
  if (
    !v.decision ||
    typeof v.decision.allowed !== "boolean" ||
    !Number.isSafeInteger(v.approvals) ||
    !strings(v.approvalRoles) ||
    !strings(v.approvalRules) ||
    !Number.isSafeInteger(v.version)
  )
    throw new StudioError("RESPONSE_INVALID");
  return v;
}

/** Drops empty condition lists and empty effects before saving. */
export function normalizePolicy(d: PolicyDocument): PolicyDocument {
  const clean = (w?: When): When | undefined => {
    const out: When = {};
    for (const key of conditionKeys) {
      const values = (w?.[key] ?? []).map((s) => s.trim()).filter(Boolean);
      if (values.length) out[key] = values;
    }
    return Object.keys(out).length ? out : undefined;
  };
  return {
    version: 1,
    rules: d.rules.map((r) => {
      const rule: PolicyRule = { id: r.id.trim(), subject: { agent: r.subject.agent.trim() } };
      for (const e of effects) if (r[e]?.length) rule[e] = [...r[e]!];
      const w = clean(r.when);
      if (w) rule.when = w;
      return rule;
    }),
    risk: d.risk.map((r) => {
      const w = clean(r.when);
      return { id: r.id.trim(), min: r.min, ...(w ? { when: w } : {}) };
    }),
    approvals: d.approvals.map((r) => {
      const w = clean(r.when);
      const roles = (r.require.roles ?? []).map((s) => s.trim()).filter(Boolean);
      return {
        id: r.id.trim(),
        require: { count: r.require.count, ...(roles.length ? { roles } : {}) },
        ...(w ? { when: w } : {}),
      };
    }),
  };
}

/** Line diff of two versions by rule id: added, removed and changed rules. */
export function policyDiff(
  before: PolicyDocument,
  after: PolicyDocument,
): { kind: "added" | "removed" | "changed"; section: string; id: string }[] {
  const out: { kind: "added" | "removed" | "changed"; section: string; id: string }[] = [];
  for (const section of ["rules", "risk", "approvals"] as const) {
    const a = new Map(before[section].map((r) => [r.id, JSON.stringify(r)]));
    const b = new Map(after[section].map((r) => [r.id, JSON.stringify(r)]));
    for (const [id, value] of b)
      if (!a.has(id)) out.push({ kind: "added", section, id });
      else if (a.get(id) !== value) out.push({ kind: "changed", section, id });
    for (const id of a.keys()) if (!b.has(id)) out.push({ kind: "removed", section, id });
  }
  return out;
}

export function describeWhen(w: When | undefined): string {
  return conditionKeys
    .filter((k) => w?.[k]?.length)
    .map((k) => k + "=" + w![k]!.join("|"))
    .join("; ");
}

export function policyMessage(code: string, ru: boolean): string {
  const messages: Record<string, [string, string]> = {
    POLICY_INVALID: [
      "Политика некорректна: проверьте идентификаторы, права и значения условий.",
      "The policy is invalid: check ids, rights and condition values.",
    ],
    POLICY_VERSION_CONFLICT: [
      "Политику изменили. Перечитайте текущую версию и повторите правку.",
      "The policy changed. Reload the current version and repeat your edit.",
    ],
    VALIDATION_FAILED: ["Укажите причину изменения.", "Enter a reason for the change."],
    FORBIDDEN: ["Нужно право project.admin.", "project.admin is required."],
    NETWORK_UNCERTAIN: [
      "Результат неизвестен. Повторите тот же запрос.",
      "The outcome is unknown. Retry the exact request.",
    ],
  };
  const m = messages[code];
  return m ? m[ru ? 0 : 1] : code;
}

export function reasonText(reason: string | undefined, ru: boolean): string {
  const names: Record<string, [string, string]> = {
    denied_by_rule: ["запрещено правилом", "denied by a rule"],
    no_rule: ["нет разрешающего правила", "no rule allows it"],
    outside_ceiling: ["вне потолка прав агента", "outside the agent ceiling"],
    delegate_lacks_right: ["у делегата нет права", "the delegate lacks the right"],
  };
  return reason ? (names[reason]?.[ru ? 0 : 1] ?? reason) : "";
}

/** The step text of a refused agent operation (STU-434). */
export function denialText(
  denial: { ruleId?: string; condition?: string; reason?: string; right?: string } | undefined,
  ru: boolean,
): string {
  if (!denial) return "";
  const cause = denial.ruleId
    ? (ru ? "правило " : "rule ") + denial.ruleId
    : reasonText(denial.reason, ru);
  return (
    (ru ? "Запрещено политикой" : "Denied by policy") +
    (cause ? ": " + cause : "") +
    (denial.condition ? ", " + denial.condition : "")
  );
}
