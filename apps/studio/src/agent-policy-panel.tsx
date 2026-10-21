import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { StudioError, type Api } from "./types.js";
import type { Editor } from "./editor.js";
import {
  conditionKeys,
  describeWhen,
  effects,
  normalizePolicy,
  policyDiff,
  policyMessage,
  reasonText,
  risks,
  sources,
  zoneModes,
  type ConditionKey,
  type Explanation,
  type PolicyAttempt,
  type PolicyDocument,
  type PolicyKnown,
  type PolicyState,
  type PolicyVersion,
  type Situation,
  type When,
} from "./agent-policy.js";

const conditionNames: Record<ConditionKey, [string, string]> = {
  environment: ["Окружение", "Environment"],
  pathPrefix: ["Префикс пути", "Path prefix"],
  zoneMode: ["Режим зоны", "Zone mode"],
  zoneId: ["Зона (id узла)", "Zone (node id)"],
  operationType: ["Тип операции", "Operation type"],
  schema: ["Схема", "Schema"],
  risk: ["Риск", "Risk"],
  source: ["Источник", "Source"],
};
const effectNames: Record<(typeof effects)[number], [string, string]> = {
  allow: ["Разрешить", "Allow"],
  deny: ["Запретить", "Deny"],
  requireApproval: ["Требовать согласования", "Require approval"],
};

function Multi({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: string[];
  value: string[];
  onChange: (next: string[]) => void;
}) {
  return (
    <label>
      {label}
      <select
        multiple
        aria-label={label}
        size={Math.min(5, Math.max(2, options.length))}
        value={value}
        onChange={(e) => onChange([...e.target.selectedOptions].map((o) => o.value))}
      >
        {options.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    </label>
  );
}

function List({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string[];
  onChange: (next: string[]) => void;
}) {
  return (
    <label>
      {label}
      <input
        aria-label={label}
        value={value.join(", ")}
        onChange={(e) => onChange(e.target.value.split(",").map((s) => s.trimStart()))}
      />
    </label>
  );
}

function WhenEditor({
  when,
  known,
  ru,
  onChange,
  prefix,
}: {
  when: When | undefined;
  known: PolicyKnown;
  ru: boolean;
  onChange: (next: When) => void;
  prefix: string;
}) {
  const options: Partial<Record<ConditionKey, string[]>> = {
    environment: known.environments,
    zoneMode: zoneModes,
    operationType: known.operationTypes,
    schema: known.schemas,
    risk: risks,
    source: sources,
  };
  return (
    <details>
      <summary>
        {ru ? "Условия" : "Conditions"}: {describeWhen(when) || (ru ? "всегда" : "always")}
      </summary>
      {conditionKeys.map((key) => {
        const label = prefix + " · " + conditionNames[key][ru ? 0 : 1];
        const set = (next: string[]) => onChange({ ...when, [key]: next });
        return options[key] ? (
          <Multi
            key={key}
            label={label}
            options={options[key]!}
            value={when?.[key] ?? []}
            onChange={set}
          />
        ) : (
          <List key={key} label={label} value={when?.[key] ?? []} onChange={set} />
        );
      })}
    </details>
  );
}

export function AgentPolicyPanel({ api, editor }: { api: Api; editor: Editor }) {
  const state = useStore(editor.store),
    ru = state.locale === "ru",
    text = (r: string, e: string) => (ru ? r : e);
  const [current, setCurrent] = useState<PolicyState | null>(null),
    [draft, setDraft] = useState<PolicyDocument | null>(null),
    [versions, setVersions] = useState<PolicyVersion[]>([]),
    [reason, setReason] = useState(""),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [pending, setPending] = useState(false),
    [uncertain, setUncertain] = useState(false),
    [answer, setAnswer] = useState<Explanation | null>(null),
    [situation, setSituation] = useState<Situation>({
      agent: "",
      right: "",
      environment: "",
      path: "",
      zoneMode: "",
      zoneId: "",
      operationType: "",
      schema: "",
      risk: "",
    });
  const attempt = useRef<PolicyAttempt | null>(null),
    alive = useRef(true);
  async function read(signal?: AbortSignal) {
    setLoading(true);
    setError("");
    try {
      const [next, list] = await Promise.all([
        api.agentPolicy!(signal),
        api.agentPolicyVersions!(signal),
      ]);
      if (!alive.current || signal?.aborted) return;
      setCurrent(next);
      setDraft(structuredClone(next.policy));
      setVersions(list);
    } catch (cause) {
      if (alive.current && !signal?.aborted)
        setError(cause instanceof StudioError ? cause.code : "AGENT_POLICY_UNAVAILABLE");
    } finally {
      if (alive.current && !signal?.aborted) setLoading(false);
    }
  }
  useEffect(() => {
    alive.current = true;
    const controller = new AbortController();
    void read(controller.signal);
    return () => {
      alive.current = false;
      controller.abort();
    };
  }, [api]);
  const locked = pending || uncertain;
  const dirty =
    !!current &&
    !!draft &&
    (JSON.stringify(normalizePolicy(draft)) !== JSON.stringify(normalizePolicy(current.policy)) ||
      locked);
  useEffect(() => {
    editor.formDirty("agent-policy", dirty);
    return () => editor.formDirty("agent-policy", false);
  }, [editor, dirty]);

  async function save() {
    if (pending || !api.agentPolicyCommand) return;
    setPending(true);
    setError("");
    try {
      if (!attempt.current) {
        if (!current || !draft) return;
        const account = await api.account();
        if (!account.authenticated || !account.user?.id) throw new StudioError("UNAUTHENTICATED");
        attempt.current = {
          payload: { policy: normalizePolicy(draft), expectedVersion: current.version },
          key: crypto.randomUUID(),
          reason: reason.trim(),
          actor: account.user.id,
        };
      }
      await api.agentPolicyCommand(attempt.current);
      attempt.current = null;
      if (!alive.current) return;
      setUncertain(false);
      setReason("");
      await read();
    } catch (cause) {
      if (!alive.current) return;
      const retry = !!attempt.current && (!(cause instanceof StudioError) || cause.uncertain);
      if (!retry) attempt.current = null;
      setUncertain(retry);
      setError(cause instanceof StudioError ? cause.code : "NETWORK_UNCERTAIN");
    } finally {
      if (alive.current) setPending(false);
    }
  }
  async function explain() {
    if (!api.explainAgentPolicy || !draft) return;
    setError("");
    setAnswer(null);
    try {
      const value = await api.explainAgentPolicy({
        policy: normalizePolicy(draft),
        situation,
      });
      if (alive.current) setAnswer(value);
    } catch (cause) {
      if (alive.current)
        setError(cause instanceof StudioError ? cause.code : "AGENT_POLICY_UNAVAILABLE");
    }
  }
  const update = (fn: (d: PolicyDocument) => void) =>
    setDraft((d) => {
      if (!d) return d;
      const next = structuredClone(d);
      fn(next);
      return next;
    });
  const known = current?.known;
  const subjects = ["*", ...(known?.agents ?? [])];
  return (
    <section className="capability-queue agent-policy" aria-busy={loading || pending}>
      <h1>{text("Политика агентов", "Agent policy")}</h1>
      <p>
        {text(
          "Правила сужают права агентов в пределах потолка: запрет сильнее разрешения, без подходящего разрешения действует потолок.",
          "Rules narrow agent rights within the ceiling: deny wins over allow; without a matching allow the ceiling applies.",
        )}
      </p>
      {error && <p role="alert">{policyMessage(error, ru)}</p>}
      {current && (
        <p>
          {text("Версия", "Version")}: {current.version}
          {current.authorName ? " · " + current.authorName : ""}
        </p>
      )}
      {draft && known && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <fieldset disabled={locked || loading}>
            <legend>{text("Правила прав", "Rights rules")}</legend>
            {draft.rules.map((rule, i) => {
              const name = text("Правило", "Rule") + " " + (rule.id || i + 1);
              return (
                <fieldset key={i}>
                  <legend>{name}</legend>
                  <label>
                    {text("Идентификатор", "Id")}
                    <input
                      required
                      pattern="[a-z][a-z0-9\-]{0,63}"
                      value={rule.id}
                      onChange={(e) => update((d) => (d.rules[i]!.id = e.target.value))}
                    />
                  </label>
                  <label>
                    {text("Агент", "Agent")}
                    <select
                      value={rule.subject.agent}
                      onChange={(e) => update((d) => (d.rules[i]!.subject.agent = e.target.value))}
                    >
                      {[...new Set([...subjects, rule.subject.agent])].map((s) => (
                        <option key={s} value={s}>
                          {s === "*" ? text("Все агенты", "All agents") : s}
                        </option>
                      ))}
                    </select>
                  </label>
                  {effects.map((effect) => (
                    <Multi
                      key={effect}
                      label={name + " · " + effectNames[effect][ru ? 0 : 1]}
                      options={known.rights}
                      value={rule[effect] ?? []}
                      onChange={(next) => update((d) => (d.rules[i]![effect] = next))}
                    />
                  ))}
                  <WhenEditor
                    prefix={name}
                    when={rule.when}
                    known={known}
                    ru={ru}
                    onChange={(w) => update((d) => (d.rules[i]!.when = w))}
                  />
                  <button type="button" onClick={() => update((d) => d.rules.splice(i, 1))}>
                    {text("Удалить правило", "Remove rule")} {rule.id}
                  </button>
                </fieldset>
              );
            })}
            <button
              type="button"
              onClick={() =>
                update((d) =>
                  d.rules.push({
                    id: "rule-" + (d.rules.length + 1),
                    subject: { agent: "*" },
                    deny: [],
                  }),
                )
              }
            >
              {text("Добавить правило", "Add rule")}
            </button>
          </fieldset>
          <fieldset disabled={locked || loading}>
            <legend>{text("Правила риска", "Risk rules")}</legend>
            {draft.risk.map((rule, i) => {
              const name = text("Риск", "Risk") + " " + (rule.id || i + 1);
              return (
                <fieldset key={i}>
                  <legend>{name}</legend>
                  <label>
                    {text("Идентификатор", "Id")}
                    <input
                      required
                      pattern="[a-z][a-z0-9\-]{0,63}"
                      value={rule.id}
                      onChange={(e) => update((d) => (d.risk[i]!.id = e.target.value))}
                    />
                  </label>
                  <label>
                    {text("Не ниже", "At least")}
                    <select
                      value={rule.min}
                      onChange={(e) => update((d) => (d.risk[i]!.min = e.target.value))}
                    >
                      {risks.map((r) => (
                        <option key={r}>{r}</option>
                      ))}
                    </select>
                  </label>
                  <WhenEditor
                    prefix={name}
                    when={rule.when}
                    known={known}
                    ru={ru}
                    onChange={(w) => update((d) => (d.risk[i]!.when = w))}
                  />
                  <button type="button" onClick={() => update((d) => d.risk.splice(i, 1))}>
                    {text("Удалить правило риска", "Remove risk rule")} {rule.id}
                  </button>
                </fieldset>
              );
            })}
            <button
              type="button"
              onClick={() =>
                update((d) => d.risk.push({ id: "risk-" + (d.risk.length + 1), min: "high" }))
              }
            >
              {text("Добавить правило риска", "Add risk rule")}
            </button>
          </fieldset>
          <fieldset disabled={locked || loading}>
            <legend>{text("Правила согласований", "Approval rules")}</legend>
            {draft.approvals.map((rule, i) => {
              const name = text("Согласование", "Approval") + " " + (rule.id || i + 1);
              return (
                <fieldset key={i}>
                  <legend>{name}</legend>
                  <label>
                    {text("Идентификатор", "Id")}
                    <input
                      required
                      pattern="[a-z][a-z0-9\-]{0,63}"
                      value={rule.id}
                      onChange={(e) => update((d) => (d.approvals[i]!.id = e.target.value))}
                    />
                  </label>
                  <label>
                    {text("Число согласований", "Approvals")}
                    <input
                      type="number"
                      min={0}
                      max={5}
                      value={rule.require.count}
                      onChange={(e) =>
                        update((d) => (d.approvals[i]!.require.count = Number(e.target.value)))
                      }
                    />
                  </label>
                  <List
                    label={name + " · " + text("Роли", "Roles")}
                    value={rule.require.roles ?? []}
                    onChange={(next) => update((d) => (d.approvals[i]!.require.roles = next))}
                  />
                  <WhenEditor
                    prefix={name}
                    when={rule.when}
                    known={known}
                    ru={ru}
                    onChange={(w) => update((d) => (d.approvals[i]!.when = w))}
                  />
                  <button type="button" onClick={() => update((d) => d.approvals.splice(i, 1))}>
                    {text("Удалить правило согласований", "Remove approval rule")} {rule.id}
                  </button>
                </fieldset>
              );
            })}
            <button
              type="button"
              onClick={() =>
                update((d) =>
                  d.approvals.push({
                    id: "approval-" + (d.approvals.length + 1),
                    require: { count: 1 },
                  }),
                )
              }
            >
              {text("Добавить правило согласований", "Add approval rule")}
            </button>
          </fieldset>
          <fieldset disabled={locked || loading}>
            <legend>{text("Сохранение", "Save")}</legend>
            <label>
              {text("Причина изменения", "Reason for the change")}
              <input
                required
                maxLength={2000}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </label>
            <button disabled={!dirty || !reason.trim()}>
              {text("Сохранить новую версию", "Save new version")}
            </button>
            <button
              type="button"
              disabled={!dirty}
              onClick={() => setDraft(structuredClone(current!.policy))}
            >
              {text("Отменить правки", "Discard edits")}
            </button>
          </fieldset>
        </form>
      )}
      {uncertain && (
        <button disabled={pending} onClick={() => void save()}>
          {text("Повторить тот же запрос", "Retry exact request")}
        </button>
      )}
      <button disabled={locked || loading} onClick={() => void read()}>
        {text("Перечитать политику", "Reload policy")}
      </button>
      {draft && known && (
        <form
          className="agent-policy-explain"
          onSubmit={(e) => {
            e.preventDefault();
            void explain();
          }}
        >
          <fieldset>
            <legend>{text("Проверить политику", "Check the policy")}</legend>
            <label>
              {text("Агент", "Agent")}
              <select
                value={situation.agent}
                onChange={(e) => setSituation({ ...situation, agent: e.target.value })}
              >
                <option value="">—</option>
                {known.agents.map((a) => (
                  <option key={a}>{a}</option>
                ))}
              </select>
            </label>
            <label>
              {text("Право", "Right")}
              <select
                required
                value={situation.right}
                onChange={(e) => setSituation({ ...situation, right: e.target.value })}
              >
                <option value="">—</option>
                {known.rights.map((r) => (
                  <option key={r}>{r}</option>
                ))}
              </select>
            </label>
            {(
              [
                ["environment", known.environments],
                ["zoneMode", zoneModes],
                ["operationType", known.operationTypes],
                ["schema", known.schemas],
                ["risk", risks],
              ] as const
            ).map(([key, options]) => (
              <label key={key}>
                {conditionNames[key][ru ? 0 : 1]}
                <select
                  value={situation[key]}
                  onChange={(e) => setSituation({ ...situation, [key]: e.target.value })}
                >
                  <option value="">—</option>
                  {options.map((o) => (
                    <option key={o}>{o}</option>
                  ))}
                </select>
              </label>
            ))}
            {(["path", "zoneId"] as const).map((key) => (
              <label key={key}>
                {key === "path" ? text("Путь", "Path") : conditionNames.zoneId[ru ? 0 : 1]}
                <input
                  value={situation[key]}
                  onChange={(e) => setSituation({ ...situation, [key]: e.target.value })}
                />
              </label>
            ))}
            <button>{text("Проверить черновик", "Check the draft")}</button>
          </fieldset>
          {answer && (
            <p role="status">
              {answer.decision.allowed
                ? answer.decision.approval
                  ? text("Разрешено с согласованием", "Allowed with approval")
                  : text("Разрешено", "Allowed")
                : text("Запрещено", "Denied") + ": " + reasonText(answer.decision.reason, ru)}
              {answer.decision.ruleId
                ? " · " + text("правило ", "rule ") + answer.decision.ruleId
                : ""}
              {answer.decision.condition ? " · " + answer.decision.condition : ""}
              {answer.riskRule
                ? " · " + text("риск ", "risk ") + answer.risk + " (" + answer.riskRule + ")"
                : ""}
              {answer.approvals
                ? " · " +
                  text("согласований: ", "approvals: ") +
                  answer.approvals +
                  (answer.approvalRoles.length ? " (" + answer.approvalRoles.join(", ") + ")" : "")
                : ""}
            </p>
          )}
        </form>
      )}
      <section aria-labelledby="agent-policy-history">
        <h2 id="agent-policy-history">{text("История версий", "Version history")}</h2>
        <ol>
          {versions.map((v, i) => {
            const diff = policyDiff(
              versions[i + 1]?.policy ?? { version: 1, rules: [], risk: [], approvals: [] },
              v.policy,
            );
            return (
              <li key={v.version}>
                <strong>
                  {text("Версия", "Version")} {v.version}
                </strong>{" "}
                · {v.authorName || text("миграция", "migration")} ·{" "}
                {v.createdAt ? new Date(v.createdAt).toLocaleString(ru ? "ru-RU" : "en-GB") : ""}
                {v.reason ? " · " + v.reason : ""}
                <ul>
                  {diff.map((d) => (
                    <li key={d.kind + d.section + d.id}>
                      {{ added: "+", removed: "−", changed: "~" }[d.kind]} {d.section}/{d.id}
                    </li>
                  ))}
                </ul>
              </li>
            );
          })}
        </ol>
      </section>
    </section>
  );
}
