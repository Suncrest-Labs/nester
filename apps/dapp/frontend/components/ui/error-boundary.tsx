"use client";

import { Component, type ErrorInfo, type ReactNode } from "react";

export type ErrorBoundaryFallbackProps = {
    error: Error;
    reset: () => void;
};

type ErrorBoundaryProps = {
    children: ReactNode;
    fallback:
        | ReactNode
        | ((props: ErrorBoundaryFallbackProps) => ReactNode);
    onReset?: () => void;
};

type ErrorBoundaryState = { error: Error | null };

export class ErrorBoundary extends Component<
    ErrorBoundaryProps,
    ErrorBoundaryState
> {
    state: ErrorBoundaryState = { error: null };

    static getDerivedStateFromError(error: Error): ErrorBoundaryState {
        return { error };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        if (process.env.NODE_ENV === "development") {
            console.error("[ErrorBoundary]", error, info.componentStack);
        }
    }

    reset = () => {
        this.props.onReset?.();
        this.setState({ error: null });
    };

    render() {
        const { error } = this.state;
        if (error) {
            const { fallback } = this.props;
            if (typeof fallback === "function") {
                return fallback({ error, reset: this.reset });
            }
            return fallback;
        }
        return this.props.children;
    }
}
