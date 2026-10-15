import { useEffect, useMemo, useState, useRef } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useStore } from "zustand";
import { createApi } from "./api.js";
import { createEditor, type Editor } from "./editor.js";
import { Studio } from "./ui.js";
import type { Account, Api } from "./types.js";
function Person({
  account,
  api,
  editor,
  reset,
}: {
  account: Account;
  api: Api;
  editor: Editor;
  reset: () => void;
}) {
  const state = useStore(editor.store),
    ru = state.locale === "ru";
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(false);
  return (
    <div className="person-bar">
      <span>{account.user?.name}</span>
      <button
        disabled={
          busy || ["saving", "uncertain"].includes(state.status) || !!state.dirtyFields.publication
        }
        onClick={async () => {
          setBusy(true);
          setError(false);
          try {
            await api.logout();
            reset();
          } catch {
            setError(true);
            setBusy(false);
          }
        }}
      >
        {ru ? "Выйти" : "Sign out"}
      </button>
      {error && (
        <p role="alert">
          {ru
            ? "Не удалось подтвердить выход. Повторите попытку."
            : "Could not confirm sign out. Try again."}
        </p>
      )}
    </div>
  );
}
function WorkspaceSession({
  api,
  account,
  reset,
}: {
  api: Api;
  account: Account | null;
  reset: () => void;
}) {
  const editor = useMemo(() => createEditor(api), [api]),
    client = useMemo(() => new QueryClient(), []);
  useEffect(
    () => () => {
      editor.dispose();
      client.clear();
    },
    [editor, client],
  );
  return (
    <QueryClientProvider client={client}>
      <Studio
        api={api}
        editor={editor}
        actorId={account?.user?.id ?? "service"}
        {...(account ? { rights: account.rights } : {})}
        accountControls={account ? <Person {...{ api, account, editor, reset }} /> : null}
      />
    </QueryClientProvider>
  );
}
export function StudioApp({
  makeApi = createApi,
}: {
  makeApi?: (fetcher?: typeof fetch, onSessionLost?: () => void) => Api;
}) {
  const [generation, setGeneration] = useState(0),
    [status, setStatus] = useState("loading"),
    [account, setAccount] = useState<Account | null>(null),
    [locale, setLocale] = useState("ru");
  const reset = () => {
    window.history.replaceState({}, "", "/editor");
    setGeneration((g) => g + 1);
    setStatus("loading");
    setAccount(null);
  };
  const api = useMemo(() => makeApi(undefined, reset), [makeApi, generation]);
  const lastUser = useRef<string | null>(null);
  const ru = locale === "ru";
  useEffect(() => {
    const controller = new AbortController();
    let live = true;
    setStatus("loading");
    void (async () => {
      try {
        const config = await api.config(controller.signal);
        if (config.authMode !== "oidc") {
          if (live) setStatus("service");
          return;
        }
        const user = await api.account(controller.signal);
        if (live) {
          if (
            (!user.authenticated && window.location.hash) ||
            (lastUser.current && lastUser.current !== user.user?.id)
          )
            window.history.replaceState({}, "", window.location.pathname + window.location.search);
          lastUser.current = user.authenticated ? user.user!.id : null;
          setAccount(user);
          setStatus(
            user.authenticated
              ? user.hasAccess &&
                (user.rights.includes("design.read") || user.rights.includes("content.read"))
                ? "ready"
                : "noAccess"
              : "anonymous",
          );
        }
      } catch {
        if (live) setStatus("error");
      }
    })();
    return () => {
      live = false;
      controller.abort();
    };
  }, [api]);
  if (status === "ready" || status === "service")
    return (
      <WorkspaceSession
        key={generation}
        api={api}
        account={status === "ready" ? account : null}
        reset={reset}
      />
    );
  return (
    <main className="identity-gate">
      <h1>CMS Studio</h1>
      <label>
        {ru ? "Язык" : "Language"}
        <select value={locale} onChange={(e) => setLocale(e.target.value)}>
          <option value="ru">Русский</option>
          <option value="en">English</option>
        </select>
      </label>
      {status === "loading" ? (
        <p role="status">{ru ? "Проверка входа…" : "Checking session…"}</p>
      ) : status === "noAccess" ? (
        <>
          <p>{account?.user?.name}</p>
          <p>
            {ru
              ? "Доступ к проекту ещё не назначен. Передайте администратору свой ID для назначения роли."
              : "Project access has not been assigned. Give your ID to an administrator to assign a role."}
          </p>
          <code>{account?.user?.id}</code>
          <button
            onClick={async () => {
              try {
                await api.logout();
                reset();
              } catch {
                setStatus("error");
              }
            }}
          >
            {ru ? "Выйти" : "Sign out"}
          </button>
        </>
      ) : status === "error" ? (
        <p role="alert">
          {ru
            ? "Не удалось проверить вход. Повторите попытку."
            : "Could not check your session. Try again."}
        </p>
      ) : (
        <>
          <p>
            {ru
              ? "Войдите, чтобы открыть свои работы и согласования."
              : "Sign in to open your work and reviews."}
          </p>
          {new URLSearchParams(window.location.search).has("authError") && (
            <p role="alert">
              {ru
                ? "Вход не завершён. Повторите попытку."
                : "Sign in was not completed. Try again."}
            </p>
          )}
          <a href="/api/studio/auth/login">{ru ? "Войти" : "Sign in"}</a>
        </>
      )}
      <button disabled={status === "loading"} onClick={() => setGeneration((g) => g + 1)}>
        {ru ? "Проверить снова" : "Check again"}
      </button>
    </main>
  );
}
