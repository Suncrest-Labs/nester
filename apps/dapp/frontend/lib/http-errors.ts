export type HttpErrorKind =
    | "network"
    | "server"
    | "not_found"
    | "unauthorized"
    | "rate_limited"
    | "unknown";

export function classifyHttpStatus(status: number | undefined): HttpErrorKind {
    if (status === undefined || status === 0) return "network";
    if (status === 401) return "unauthorized";
    if (status === 404) return "not_found";
    if (status === 429) return "rate_limited";
    if (status >= 500) return "server";
    return "unknown";
}

export function getHttpErrorCopy(kind: HttpErrorKind): {
    title: string;
    description: string;
    showRetry: boolean;
    showReconnect: boolean;
    rateLimitSeconds?: number;
} {
    switch (kind) {
        case "network":
            return {
                title: "Connection problem",
                description:
                    "Unable to connect. Check your internet connection.",
                showRetry: true,
                showReconnect: false,
            };
        case "server":
            return {
                title: "Something went wrong",
                description:
                    "Something went wrong on our end. Please try again.",
                showRetry: true,
                showReconnect: false,
            };
        case "not_found":
            return {
                title: "Not found",
                description:
                    "This vault or settlement doesn't exist or has been removed.",
                showRetry: false,
                showReconnect: false,
            };
        case "unauthorized":
            return {
                title: "Session expired",
                description:
                    "Your session has expired. Please reconnect your wallet.",
                showRetry: false,
                showReconnect: true,
            };
        case "rate_limited":
            return {
                title: "Slow down",
                description:
                    "Too many requests. Please wait a moment.",
                showRetry: true,
                showReconnect: false,
                rateLimitSeconds: 30,
            };
        default:
            return {
                title: "Request failed",
                description: "We couldn't complete this request. Please try again.",
                showRetry: true,
                showReconnect: false,
            };
    }
}
