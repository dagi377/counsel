import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { Button } from "./Button";

export type ToastTone = "neutral" | "ok" | "bad" | "gate";

export interface ToastInput {
  text: string;
  tone?: ToastTone;
  action?: { label: string; onClick: () => void };
  // durationMs defaults to 6000; bad toasts stay until dismissed.
  durationMs?: number;
}

export interface ToastItem extends ToastInput {
  id: number;
}

export interface ToastApi {
  show: (t: ToastInput) => number;
  dismiss: (id: number) => void;
}

const Ctx = createContext<ToastApi | null>(null);

const TONE_WORD: Record<ToastTone, string | null> = { neutral: null, ok: "Done", bad: "Failed", gate: "Needs approval" };

// ToastProvider mounts once near the root. Screens call useToast().show({text, tone}) after an
// action completes; a failure that needs reading belongs in a Banner, not a toast.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const seq = useRef(0);
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());

  const dismiss = useCallback((id: number) => {
    const t = timers.current.get(id);
    if (t) clearTimeout(t);
    timers.current.delete(id);
    setItems((xs) => xs.filter((x) => x.id !== id));
  }, []);

  const show = useCallback(
    (t: ToastInput) => {
      const id = ++seq.current;
      setItems((xs) => [...xs, { ...t, id }]);
      const ms = t.durationMs ?? (t.tone === "bad" ? 0 : 6000);
      if (ms > 0) timers.current.set(id, setTimeout(() => dismiss(id), ms));
      return id;
    },
    [dismiss],
  );

  const api = useMemo(() => ({ show, dismiss }), [show, dismiss]);

  return (
    <Ctx.Provider value={api}>
      {children}
      <div className="ui-toasts">
        {items.map((t) => {
          const tone = t.tone ?? "neutral";
          return (
            <div
              key={t.id}
              className={["ui-toast", tone !== "neutral" && `ui-toast--${tone}`].filter(Boolean).join(" ")}
              role={tone === "bad" ? "alert" : "status"}
            >
              <span className="ui-toast__text">
                {TONE_WORD[tone] && <span className="ui-toast__tone">{TONE_WORD[tone]}:</span>}
                {t.text}
              </span>
              {t.action && (
                <Button
                  size="sm"
                  onClick={() => {
                    dismiss(t.id);
                    t.action?.onClick();
                  }}
                >
                  {t.action.label}
                </Button>
              )}
              <Button size="sm" variant="ghost" aria-label="Dismiss" onClick={() => dismiss(t.id)}>
                {"✕"}
              </Button>
            </div>
          );
        })}
      </div>
    </Ctx.Provider>
  );
}

export function useToast(): ToastApi {
  const api = useContext(Ctx);
  if (!api) throw new Error("useToast: no ToastProvider above this component");
  return api;
}
