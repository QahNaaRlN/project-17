import { Component, Suspense, type ReactNode } from "react";

export class DeferredPanel extends Component<{
  children: ReactNode;
  locale: "ru" | "en";
  canReload: boolean;
}> {
  override state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  override render() {
    const ru = this.props.locale === "ru";
    if (this.state.failed)
      return (
        <section className="panel-load-error" role="alert">
          <p>
            {ru ? "Не удалось загрузить панель редактора." : "Could not load the editor panel."}
          </p>
          <p>
            {ru
              ? "Сохраните или сбросьте ввод перед перезагрузкой Studio."
              : "Save or discard your input before reloading Studio."}
          </p>
          <button disabled={!this.props.canReload} onClick={() => window.location.reload()}>
            {ru ? "Перезагрузить Studio" : "Reload Studio"}
          </button>
        </section>
      );
    return (
      <Suspense
        fallback={
          <p className="panel-loading" role="status">
            {ru ? "Загрузка панели…" : "Loading panel…"}
          </p>
        }
      >
        {this.props.children}
      </Suspense>
    );
  }
}
