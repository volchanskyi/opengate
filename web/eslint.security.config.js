// @ts-nocheck
// Security-only ESLint config used by `make taint-web`.
//
// PR 1 installs `eslint-plugin-security` and `eslint-plugin-no-unsanitized` as
// devDependencies but does NOT register them in the main `eslint.config.js` —
// that registration (and the associated baseline cleanup) lands in PR 5.
// Until then, this config lets developers run the security plugins in
// isolation without affecting `npm run lint` or CI.
//
// Rule sets used here are the upstream `recommended` configurations. Severity
// is `error` so findings surface clearly when the target is invoked.
import js from '@eslint/js'
import globals from 'globals'
import security from 'eslint-plugin-security'
import noUnsanitized from 'eslint-plugin-no-unsanitized'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores([
    'dist',
    'e2e',
    'reports',
    'vitest.config.ts',
    'vite.config.ts',
    'playwright.config.ts',
    'playwright.staging.config.ts',
    'stryker.config.json',
    'src/types/api.d.ts',
  ]),
  {
    files: ['**/*.{ts,tsx,js}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      security.configs.recommended,
      noUnsanitized.configs.recommended,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
  },
])
