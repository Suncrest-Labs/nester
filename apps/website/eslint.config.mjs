import { defineConfig } from "eslint/config";
import nextCoreWebVitals from "eslint-config-next/core-web-vitals";
import nextTypescript from "eslint-config-next/typescript";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export default defineConfig([{
    extends: [...nextCoreWebVitals, ...nextTypescript],
    rules: {
        "react-hooks/exhaustive-deps": "off",
        "react-hooks/rules-of-hooks": "off",
        "react-hooks/set-state-in-effect": "off",
        "@next/next/no-img-element": "off"
    }
}]);