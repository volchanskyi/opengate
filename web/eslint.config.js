import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import security from 'eslint-plugin-security'
import noUnsanitized from 'eslint-plugin-no-unsanitized'
import boundaries from 'eslint-plugin-boundaries'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', 'e2e', 'coverage', 'reports', '.stryker-tmp', 'vitest.config.ts', 'vite.config.ts', 'playwright.config.ts', 'playwright.staging.config.ts']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
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
    rules: {
      // Surface silently-swallowed promise rejections.
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/no-misused-promises': 'error',
      // The security plugin's recommended rules run at error severity.
      'security/detect-object-injection': 'error',
      'security/detect-non-literal-fs-filename': 'error',
      'security/detect-non-literal-regexp': 'error',
      'security/detect-unsafe-regex': 'error',
      'security/detect-buffer-noassert': 'error',
      'security/detect-child-process': 'error',
      'security/detect-disable-mustache-escape': 'error',
      'security/detect-eval-with-expression': 'error',
      'security/detect-no-csrf-before-method-override': 'error',
      'security/detect-non-literal-require': 'error',
      'security/detect-possible-timing-attacks': 'error',
      'security/detect-pseudoRandomBytes': 'error',
      'security/detect-bidi-characters': 'error',
      'security/detect-new-buffer': 'error',
    },
  },
  {
    // The router file declares lazy() routes beside the router export, and fast refresh does not apply.
    files: ['src/router.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  // Boundary groups: app entry points, features, the lib utility layer and bootstrap-coupled state.
  // Only useAuthStore lives in src/state, the global store that the boundary rules permit.
  {
    files: ['src/**/*.{ts,tsx}'],
    plugins: { boundaries },
    settings: {
      'boundaries/include': ['src/**/*'],
      // Imports omit their extension; a target left unresolved escapes every policy.
      'import/resolver': { node: { extensions: ['.ts', '.tsx', '.js', '.jsx'] } },
      'boundaries/elements': [
        // The entry points are single files, which only mode 'file' classifies.
        { type: 'app', pattern: 'src/{main,App,router,vite-env.d}.{ts,tsx}', mode: 'file' },
        { type: 'app-state', pattern: 'src/state/**' },
        { type: 'feature', pattern: 'src/features/*/**' },
        { type: 'lib', pattern: 'src/lib/**' },
      ],
    },
    rules: {
      'boundaries/dependencies': ['error', {
        default: 'disallow',
        policies: [
          // Entry points reach everywhere.
          {
            from: { element: { type: 'app' } },
            allow: { to: { element: { types: { anyOf: ['app', 'app-state', 'feature', 'lib'] } } } },
          },
          // Features may use shared utilities + the global bootstrap stores.
          {
            from: { element: { type: 'feature' } },
            allow: { to: { element: { types: { anyOf: ['feature', 'lib', 'app-state'] } } } },
          },
          // The lib layer is a leaf — utilities only depend on other utilities.
          { from: { element: { type: 'lib' } }, allow: { to: { element: { type: 'lib' } } } },
          // Global bootstrap stores can pull lib helpers but not features.
          {
            from: { element: { type: 'app-state' } },
            allow: { to: { element: { types: { anyOf: ['app-state', 'lib'] } } } },
          },
        ],
      }],
    },
  },
])
