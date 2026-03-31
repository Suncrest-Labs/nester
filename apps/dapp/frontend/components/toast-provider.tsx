"use client";

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useReducer,
    useRef,
    type ReactNode,
} from "react";

export type ToastVariant = "success" | "error" | "warning" | "info";

export type ToastAction = {
    label: string;
    href?: string;
    onClick?: () => void;
};

type ToastRecord = {
    id: string;
    variant: ToastVariant;
    title: string;
    description?: string;
    durationMs: number | "persist";
    action?: ToastAction;
};

type ToastState = {
    active: ToastRecord[];
    queue: ToastRecord[];
};

const MAX_VISIBLE = 3;

function buildId() {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return `toast-${crypto.randomUUID()}`;
    }
    return `toast-${Date.now()}-${Math.floor(Math.random() * 10000)}`;
}

function toastReducer(state: ToastState, action: { type: "push"; toast: ToastRecord } | { type: "dismiss"; id: string }): ToastState {
    if (action.type === "push") {
        const next = action.toast;
        if (state.active.length < MAX_VISIBLE) {
            return { ...state, active: [...state.active, next] };
        }
        return { ...state, queue: [...state.queue, next] };
    }

    const active = state.active.filter((t) => t.id !== action.id);
    const queue = [...state.queue];
    while (active.length < MAX_VISIBLE && queue.length > 0) {
        const next = queue.shift();
        if (next) active.push(next);
    }
    return { active, queue };
}

type ToastOptions = {
    description?: string;
    durationMs?: number;
    action?: ToastAction;
};

type ToastContextValue = {
    toasts: ToastRecord[];
    dismiss: (id: string) => void;
    success: (title: string, options?: ToastOptions) => void;
    error: (title: string, options?: ToastOptions) => void;
    warning: (title: string, options?: ToastOptions) => void;
    info: (title: string, options?: ToastOptions) => void;
};

const ToastContext = createContext<ToastContextValue | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
    const [state, dispatch] = useReducer(toastReducer, { active: [], queue: [] });
    const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});

    const dismiss = useCallback((id: string) => {
        const t = timers.current[id];
        if (t) {
            clearTimeout(t);
            delete timers.current[id];
        }
        dispatch({ type: "dismiss", id });
    }, []);

    const push = useCallback(
        (variant: ToastVariant, title: string, options?: ToastOptions) => {
            const id = buildId();
            const durationMs =
                options?.durationMs ??
                (variant === "error" ? ("persist" as const) : 5000);

            const record: ToastRecord = {
                id,
                variant,
                title,
                description: options?.description,
                durationMs,
                action: options?.action,
            };

            dispatch({ type: "push", toast: record });
            return id;
        },
        []
    );

    useEffect(() => {
        const activeIds = new Set(state.active.map((t) => t.id));
        for (const id of Object.keys(timers.current)) {
            if (!activeIds.has(id)) {
                clearTimeout(timers.current[id]);
                delete timers.current[id];
            }
        }

        for (const t of state.active) {
            if (t.durationMs === "persist" || timers.current[t.id]) continue;
            timers.current[t.id] = setTimeout(() => {
                delete timers.current[t.id];
                dispatch({ type: "dismiss", id: t.id });
            }, t.durationMs);
        }
    }, [state.active]);

    useEffect(() => {
        return () => {
            Object.values(timers.current).forEach(clearTimeout);
            timers.current = {};
        };
    }, []);

    const value = useMemo<ToastContextValue>(
        () => ({
            toasts: state.active,
            dismiss,
            success: (title, options) => push("success", title, options),
            error: (title, options) =>
                push("error", title, {
                    ...options,
                    durationMs: options?.durationMs ?? "persist",
                }),
            warning: (title, options) => push("warning", title, options),
            info: (title, options) => push("info", title, options),
        }),
        [dismiss, push, state.active]
    );

    return (
        <ToastContext.Provider value={value}>{children}</ToastContext.Provider>
    );
}

export function useToast() {
    const ctx = useContext(ToastContext);
    if (!ctx) {
        throw new Error("useToast must be used within ToastProvider");
    }
    return ctx;
}
