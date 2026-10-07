// @ts-nocheck
// Security-only ESLint config used by `make taint-web`, separate from `npm run lint`.
// It applies the upstream `recommended` rule sets of the security plugins at error severity.
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
