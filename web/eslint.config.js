import js from '@eslint/js'
import globals from 'globals'
// NOTE: eslint-plugin-react-hooks 7.x only supports ESLint ≤9,
// and openapi-typescript 7.x only supports TypeScript ≤5.
// Do NOT upgrade ESLint to v10+ or TypeScript to v6+ until stable
// releases of these plugins add support.
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', 'e2e', 'vitest.config.ts', 'vite.config.ts', 'playwright.config.ts', 'playwright.staging.config.ts']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // Surface silently-swallowed promise rejections.
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/no-misused-promises': 'error',
    },
  },
  {
    // Router config file — contains lazy() declarations alongside the router
    // export. Fast refresh is irrelevant for routing configuration.
    files: ['src/router.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
])
