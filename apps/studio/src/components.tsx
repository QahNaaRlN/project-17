import { useEffect, useId, useRef, type ButtonHTMLAttributes, type ReactNode } from "react";

export function Button({
  variant = "secondary",
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "ghost" }) {
  return <button type="button" {...props} className={`studio-button ${variant} ${className}`} />;
}

export function Notice({ children, error = false }: { children: ReactNode; error?: boolean }) {
  return (
    <p className={`studio-notice ${error ? "error" : ""}`} role={error ? "alert" : "status"}>
      {children}
    </p>
  );
}

export function Dialog({
  title,
  children,
  onDismiss,
  locked = false,
}: {
  title: string;
  children: ReactNode;
  onDismiss: () => void;
  locked?: boolean;
}) {
  const id = useId(),
    ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement;
    ref.current?.querySelector<HTMLElement>("input,button")?.focus();
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, []);
  return (
    <div className="studio-dialog-overlay">
      <div
        ref={ref}
        className="studio-dialog"
        role="dialog"
        tabIndex={-1}
        aria-modal="true"
        aria-labelledby={id}
        onKeyDown={(event) => {
          if (event.key === "Escape" && !locked) {
            event.preventDefault();
            onDismiss();
          }
          if (event.key !== "Tab") return;
          const targets = [
            ...(ref.current?.querySelectorAll<HTMLElement>(
              "input:not(:disabled),button:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href]",
            ) ?? []),
          ];
          const first = targets[0],
            last = targets.at(-1);
          if (!first) {
            event.preventDefault();
            ref.current?.focus();
            return;
          }
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last?.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first?.focus();
          }
        }}
      >
        <h2 id={id}>{title}</h2>
        {children}
      </div>
    </div>
  );
}

export function Signal() {
  return (
    <svg className="studio-signal" viewBox="0 0 64 64" aria-hidden="true">
      <path d="M10 20 24 10h8v44H21V27l-11 7Z" fill="currentColor" />
      <path d="M37 10h23v10L42 54H30l18-33H37Z" fill="var(--accent)" />
    </svg>
  );
}
