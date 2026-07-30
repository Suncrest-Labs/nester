"use client";

import React, { useEffect } from "react";
import ReactDOM from "react-dom";

export const A11yAudit = () => {
    useEffect(() => {
        if (process.env.NODE_ENV === "development" && typeof window !== "undefined") {
            import("@axe-core/react").then((axe) => {
                axe.default(React, ReactDOM, 1000);
            });
        }
    }, []);

    return null;
};
