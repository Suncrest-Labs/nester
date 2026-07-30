"use client";

import React, { createContext, useContext, useReducer } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { X, CheckCircle, AlertTriangle, Info, AlertCircle } from "lucide-react";
import { cn } from "@/lib/utils";

type ToastType = "success" | "error" | "warning" | "info";

interface Toast {
  id: string;
  type: ToastType;
  title?: string;
  message: string;
  action?: {
    label: string;
    onClick: () => void;
  };
  duration?: number;
  persistent?: boolean;
}

interface ToastState {
  toasts: Toast[];
}

type ToastAction =
  | { type: "ADD_TOAST"; toast: Toast }
  | { type: "REMOVE_TOAST"; id: string }
  | { type: "CLEAR_ALL" };

const toastReducer = (state: ToastState, action: ToastAction): ToastState => {
  switch (action.type) {
    case "ADD_TOAST":
      // Limit to 3 visible toasts, remove oldest if needed
      const newToasts = [action.toast, ...state.toasts];
      return {
        toasts: newToasts.slice(0, 3)
      };
    case "REMOVE_TOAST":
      return {
        toasts: state.toasts.filter(toast => toast.id !== action.id)
      };
    case "CLEAR_ALL":
      return {
        toasts: []
      };
    default:
      return state;
  }
};

interface ToastContextValue {
  toasts: Toast[];
  addToast: (toast: Omit<Toast, "id">) => string;
  removeToast: (id: string) => void;
  clearAll: () => void;
  success: (message: string, options?: Partial<Pick<Toast, "title" | "action" | "duration">>) => string;
  error: (message: string, options?: Partial<Pick<Toast, "title" | "action" | "persistent">>) => string;
  warning: (message: string, options?: Partial<Pick<Toast, "title" | "action" | "duration">>) => string;
  info: (message: string, options?: Partial<Pick<Toast, "title" | "action" | "duration">>) => string;
}

const ToastContext = createContext<ToastContextValue | null>(null);

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [state, dispatch] = useReducer(toastReducer, { toasts: [] });

  const generateId = () => Math.random().toString(36).substr(2, 9);

  const addToast = (toast: Omit<Toast, "id">): string => {
    const id = generateId();
    const fullToast: Toast = { id, ...toast };
    
    dispatch({ type: "ADD_TOAST", toast: fullToast });
    
    // Auto-dismiss non-persistent toasts
    if (!fullToast.persistent) {
      const duration = fullToast.duration ?? getDefaultDuration(fullToast.type);
      if (duration > 0) {
        setTimeout(() => {
          dispatch({ type: "REMOVE_TOAST", id });
        }, duration);
      }
    }
    
    return id;
  };

  const removeToast = (id: string) => {
    dispatch({ type: "REMOVE_TOAST", id });
  };

  const clearAll = () => {
    dispatch({ type: "CLEAR_ALL" });
  };

  const success = (message: string, options: Partial<Pick<Toast, "title" | "action" | "duration">> = {}): string => {
    return addToast({ type: "success", message, ...options });
  };

  const error = (message: string, options: Partial<Pick<Toast, "title" | "action" | "persistent">> = {}): string => {
    return addToast({ type: "error", message, persistent: true, ...options });
  };

  const warning = (message: string, options: Partial<Pick<Toast, "title" | "action" | "duration">> = {}): string => {
    return addToast({ type: "warning", message, ...options });
  };

  const info = (message: string, options: Partial<Pick<Toast, "title" | "action" | "duration">> = {}): string => {
    return addToast({ type: "info", message, ...options });
  };

  return (
    <ToastContext.Provider value={{
      toasts: state.toasts,
      addToast,
      removeToast,
      clearAll,
      success,
      error,
      warning,
      info
    }}>
      {children}
      <ToastContainer toasts={state.toasts} onRemove={removeToast} />
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error("useToast must be used within a ToastProvider");
  }
  return context;
}

function getDefaultDuration(type: ToastType): number {
  switch (type) {
    case "success":
      return 5000;
    case "info":
      return 5000;
    case "warning":
      return 5000;
    case "error":
      return 0; // Persistent by default
    default:
      return 5000;
  }
}

function ToastContainer({ 
  toasts, 
  onRemove 
}: { 
  toasts: Toast[]; 
  onRemove: (id: string) => void;
}) {
  return (
    <div className="fixed bottom-4 right-4 z-50 space-y-3 max-w-sm w-full pointer-events-none">
      <AnimatePresence>
        {toasts.map((toast) => (
          <ToastItem
            key={toast.id}
            toast={toast}
            onRemove={() => onRemove(toast.id)}
          />
        ))}
      </AnimatePresence>
    </div>
  );
}

function ToastItem({ 
  toast, 
  onRemove 
}: { 
  toast: Toast; 
  onRemove: () => void;
}) {
  const getIcon = () => {
    switch (toast.type) {
      case "success":
        return <CheckCircle className="h-5 w-5 text-green-600" />;
      case "error":
        return <AlertCircle className="h-5 w-5 text-red-600" />;
      case "warning":
        return <AlertTriangle className="h-5 w-5 text-amber-600" />;
      case "info":
        return <Info className="h-5 w-5 text-blue-600" />;
    }
  };

  const getBorderColor = () => {
    switch (toast.type) {
      case "success":
        return "border-green-200";
      case "error":
        return "border-red-200";
      case "warning":
        return "border-amber-200";
      case "info":
        return "border-blue-200";
    }
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 20, scale: 0.95 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, y: -20, scale: 0.95 }}
      transition={{ duration: 0.2 }}
      className={cn(
        "relative rounded-xl border bg-white dark:bg-[#100F0F] shadow-lg pointer-events-auto p-4",
        getBorderColor()
      )}
    >
      <div className="flex items-start gap-3">
        <div className="flex-shrink-0 pt-0.5">
          {getIcon()}
        </div>
        
        <div className="flex-1 min-w-0">
          {toast.title && (
            <h4 className="text-sm font-medium text-black dark:text-white mb-1">
              {toast.title}
            </h4>
          )}
          <p className="text-sm text-black/80 dark:text-white/80">
            {toast.message}
          </p>

          {toast.action && (
            <button
              onClick={toast.action.onClick}
              className="mt-2 text-xs font-medium text-black/60 dark:text-white/60 hover:text-black dark:hover:text-white underline"
            >
              {toast.action.label}
            </button>
          )}
        </div>

        <button
          onClick={onRemove}
          className="flex-shrink-0 p-1 rounded-md text-black/40 dark:text-white/40 hover:text-black/60 dark:hover:text-white/60 hover:bg-black/[0.04] dark:hover:bg-white/[0.04] transition-colors"
          aria-label="Dismiss"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </motion.div>
  );
}