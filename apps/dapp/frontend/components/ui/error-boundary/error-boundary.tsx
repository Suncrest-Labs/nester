"use client";

import React from "react";
import { AlertTriangle, RefreshCw, Home, LogIn } from "lucide-react";

interface ErrorBoundaryState {
  hasError: boolean;
  error?: Error;
  errorInfo?: React.ErrorInfo;
}

interface ErrorBoundaryProps {
  children: React.ReactNode;
  fallback?: React.ComponentType<ErrorFallbackProps>;
  onError?: (error: Error, errorInfo: React.ErrorInfo) => void;
  level?: "page" | "widget";
}

interface ErrorFallbackProps {
  error?: Error;
  resetError: () => void;
  level?: "page" | "widget";
}

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return {
      hasError: true,
      error,
    };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    // Log error to console in development
    if (process.env.NODE_ENV === "development") {
      console.error("ErrorBoundary caught an error:", error, errorInfo);
    }

    this.setState({
      error,
      errorInfo,
    });

    // Call optional error handler
    this.props.onError?.(error, errorInfo);
  }

  resetError = () => {
    this.setState({ hasError: false, error: undefined, errorInfo: undefined });
  };

  render() {
    if (this.state.hasError) {
      const FallbackComponent = this.props.fallback || DefaultErrorFallback;
      
      return (
        <FallbackComponent
          error={this.state.error}
          resetError={this.resetError}
          level={this.props.level}
        />
      );
    }

    return this.props.children;
  }
}

function DefaultErrorFallback({ error, resetError, level = "page" }: ErrorFallbackProps) {
  const isWidget = level === "widget";

  // Check for specific error types
  const isNetworkError = error?.message?.toLowerCase().includes("network") || 
                        error?.message?.toLowerCase().includes("fetch");
  const is401Error = error?.message?.includes("401");
  const is404Error = error?.message?.includes("404");
  const is429Error = error?.message?.includes("429");
  const is500Error = error?.message?.includes("500");

  const getErrorContent = () => {
    if (is401Error) {
      return {
        title: "Session Expired",
        description: "Your session has expired. Please reconnect your wallet to continue.",
        action: "Reconnect Wallet",
        icon: LogIn,
        actionHandler: () => {
          // Trigger wallet reconnection
          window.location.reload();
        }
      };
    }

    if (is404Error) {
      return {
        title: "Resource Not Found",
        description: "The requested resource could not be found.",
        action: "Go Home",
        icon: Home,
        actionHandler: () => {
          window.location.href = "/dashboard";
        }
      };
    }

    if (is429Error) {
      return {
        title: "Too Many Requests",
        description: "You're sending requests too quickly. Please wait a moment.",
        action: "Retry in 30s",
        icon: RefreshCw,
        showCountdown: true,
        actionHandler: resetError
      };
    }

    if (is500Error) {
      return {
        title: "Server Error",
        description: "Something went wrong on our end. Please try again.",
        action: "Retry",
        icon: RefreshCw,
        actionHandler: resetError
      };
    }

    if (isNetworkError) {
      return {
        title: "Connection Error",
        description: "Unable to connect. Please check your internet connection.",
        action: "Retry",
        icon: RefreshCw,
        actionHandler: resetError
      };
    }

    // Default error
    return {
      title: isWidget ? "Something went wrong" : "Unexpected Error",
      description: isWidget 
        ? "This component encountered an error and couldn't load." 
        : "An unexpected error occurred. Please try refreshing the page.",
      action: "Retry",
      icon: RefreshCw,
      actionHandler: resetError
    };
  };

  const errorContent = getErrorContent();
  const Icon = errorContent.icon;

  if (isWidget) {
    return (
      <div className="flex flex-col items-center justify-center p-6 text-center rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] min-h-[12rem]">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-red-50 mb-4">
          <Icon className="h-6 w-6 text-red-600" />
        </div>

        <h3 className="text-sm font-medium text-black dark:text-white mb-2">
          {errorContent.title}
        </h3>

        <p className="text-xs text-black/60 dark:text-white/60 mb-4 max-w-xs">
          {errorContent.description}
        </p>
        
        <RetryButton
          onClick={errorContent.actionHandler}
          showCountdown={errorContent.showCountdown}
          size="sm"
        >
          {errorContent.action}
        </RetryButton>
      </div>
    );
  }

  return (
    <div className="flex min-h-[50vh] flex-col items-center justify-center p-8 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-red-50 mb-6">
        <Icon className="h-8 w-8 text-red-600" />
      </div>
      
      <h2 className="text-xl font-semibold text-black dark:text-white mb-3">
        {errorContent.title}
      </h2>

      <p className="text-sm text-black/60 dark:text-white/60 mb-6 max-w-md">
        {errorContent.description}
      </p>
      
      <div className="space-x-3">
        <RetryButton
          onClick={errorContent.actionHandler}
          showCountdown={errorContent.showCountdown}
        >
          {errorContent.action}
        </RetryButton>
        
        {!is401Error && !is404Error && (
          <button
            onClick={() => window.location.href = "/dashboard"}
            className="px-4 py-2 text-sm text-black/60 dark:text-white/60 hover:text-black dark:hover:text-white transition-colors"
          >
            Go Home
          </button>
        )}
      </div>

      {process.env.NODE_ENV === "development" && error && (
        <details className="mt-8 w-full max-w-2xl">
          <summary className="cursor-pointer text-xs text-black/40 dark:text-white/40 hover:text-black/60 dark:hover:text-white/60">
            Error Details (Development)
          </summary>
          <pre className="mt-2 text-xs text-left bg-gray-50 p-4 rounded overflow-auto">
            {error.stack}
          </pre>
        </details>
      )}
    </div>
  );
}

function RetryButton({ 
  onClick, 
  showCountdown = false, 
  children, 
  size = "default" 
}: { 
  onClick: () => void; 
  showCountdown?: boolean; 
  children: React.ReactNode;
  size?: "sm" | "default";
}) {
  const [countdown, setCountdown] = React.useState(showCountdown ? 30 : 0);

  React.useEffect(() => {
    if (countdown > 0) {
      const timer = setTimeout(() => setCountdown(countdown - 1), 1000);
      return () => clearTimeout(timer);
    }
  }, [countdown]);

  const handleClick = () => {
    if (countdown === 0) {
      onClick();
      if (showCountdown) {
        setCountdown(30);
      }
    }
  };

  const baseClass = size === "sm" 
    ? "px-3 py-1.5 text-xs" 
    : "px-4 py-2 text-sm";

  return (
    <button
      onClick={handleClick}
      disabled={countdown > 0}
      className={`${baseClass} rounded-lg border border-black/[0.08] dark:border-white/[0.08] bg-white dark:bg-[#100F0F] font-medium transition-colors hover:border-black/20 dark:hover:border-white/20 hover:bg-black/[0.02] dark:hover:bg-white/[0.02] disabled:opacity-50 disabled:cursor-not-allowed`}
    >
      {countdown > 0 ? `Retry in ${countdown}s` : children}
    </button>
  );
}

// Convenience component for widget-level error boundaries
export function WidgetErrorBoundary({ children, onError }: { 
  children: React.ReactNode; 
  onError?: (error: Error, errorInfo: React.ErrorInfo) => void;
}) {
  return (
    <ErrorBoundary level="widget" onError={onError}>
      {children}
    </ErrorBoundary>
  );
}

// Hook for programmatic error reporting
export function useErrorBoundary() {
  const [, setState] = React.useState();
  
  return React.useCallback((error: Error) => {
    setState(() => {
      throw error;
    });
  }, []);
}