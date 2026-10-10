import js from "@eslint/js";
import pluginVue from "eslint-plugin-vue";
import { defineConfigWithVueTs, vueTsConfigs } from "@vue/eslint-config-typescript";
import prettier from "eslint-config-prettier";
import globals from "globals";

export default defineConfigWithVueTs(
  {
    ignores: ["node_modules/", "playwright-report/", "test-results/", "src/shared/api/*.gen.ts"],
  },
  js.configs.recommended,
  pluginVue.configs["flat/recommended"],
  vueTsConfigs.strictTypeChecked,
  {
    languageOptions: { globals: { ...globals.browser } },
    rules: {
      "vue/multi-word-component-names": "off",
      "vue/require-default-prop": "off",
      "vue/block-lang": ["error", { script: { lang: "ts" } }],
      "vue/component-api-style": ["error", ["script-setup"]],
      "vue/no-v-html": "error",
      "vue/no-static-inline-styles": "error",
      "@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true }],
      "@typescript-eslint/no-confusing-void-expression": ["error", { ignoreArrowShorthand: true }],
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", ignoreRestSiblings: true },
      ],
      // Features talk to each other only through shared/ (docs/dev/frontend.md).
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/features/*/*", "!@/features/*/index"],
              message: "Import a feature through its index.ts.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/features/**", "src/shared/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/pages/*", "@/app/*"],
              message: "Lower layers must not import pages or app.",
            },
            {
              group: ["@/features/*/*", "!@/features/*/index"],
              message: "Import a feature through its index.ts.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/shared/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/pages/*", "@/app/*", "@/features/*"],
              message: "shared/ must not depend on features, pages or app.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["*.config.{js,ts}", "scripts/**", "e2e/**"],
    languageOptions: { globals: { ...globals.node } },
  },
  {
    files: ["scripts/**", "eslint.config.js"],
    extends: [vueTsConfigs.disableTypeChecked],
  },
  prettier,
);
