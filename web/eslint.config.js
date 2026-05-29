import js from '@eslint/js'
import globals from 'globals'
// NOTE: openapi-typescript 7.x caps its peer dep at TypeScript ^5.x.
// Do NOT upgrade TypeScript to v6+ until openapi-typescript ships a
// version that supports it (last checked: 7.13.0 still 5.x-only).
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
      // Promote security plugin's recommended rules from warn → error.
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
    // Router config file — contains lazy() declarations alongside the router
    // export. Fast refresh is irrelevant for routing configuration.
    files: ['src/router.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  // ADR-022 / ADR-020 §5.3 — per-feature boundaries.
  //
  // Element groups:
  //   app        — top-level src/{main,App,router,...}.tsx — entry points,
  //                may import from any group.
  //   feature    — src/features/<name>/** — should import siblings only via
  //                their barrel index.ts (deep-import rule comes later).
  //   lib        — src/lib/<name>/** — utility layer; never imports feature.
  //   app-state  — src/state/** — bootstrap-coupled global stores; only
  //                useAuthStore lives here per ADR-022, but the directory is
  //                permitted as the documented exception until the migration
  //                completes.
  //
  // Flipped to ERROR on 2026-05-28 per ADR-020 §5.4 — zero current
  // boundaries violations, marker recorded at
  // .claude/.markers/arch-lint-flipped/eslint-boundaries.
  {
    files: ['src/**/*.{ts,tsx}'],
    plugins: { boundaries },
    settings: {
      'boundaries/include': ['src/**/*'],
      'boundaries/elements': [
        { type: 'app', pattern: 'src/{main,App,router,vite-env.d}.{ts,tsx}', mode: 'file' },
        { type: 'app-state', pattern: 'src/state/**' },
        { type: 'feature', pattern: 'src/features/*/**' },
        { type: 'lib', pattern: 'src/lib/**' },
      ],
    },
    rules: {
      // v6 object-selector syntax. `boundaries/dependencies` replaces the
      // legacy `boundaries/element-types`.
      'boundaries/dependencies': ['error', {
        default: 'disallow',
        rules: [
          // Entry points reach everywhere.
          { from: { type: 'app' }, allow: { to: { type: ['app', 'app-state', 'feature', 'lib'] } } },
          // Features may use shared utilities + the global bootstrap stores.
          { from: { type: 'feature' }, allow: { to: { type: ['feature', 'lib', 'app-state'] } } },
          // The lib layer is a leaf — utilities only depend on other utilities.
          { from: { type: 'lib' }, allow: { to: { type: 'lib' } } },
          // Global bootstrap stores can pull lib helpers but not features.
          { from: { type: 'app-state' }, allow: { to: { type: ['app-state', 'lib'] } } },
        ],
      }],
      // ADR-022's barrel-only enforcement ("features must import siblings
      // only via the sibling's index.ts") rides on `boundaries/dependencies`
      // when each feature gains an index.ts. The legacy `boundaries/no-private`
      // rule is deprecated in v6+ — its semantics merged into `dependencies`
      // with appropriate selectors. We add per-feature barrel enforcement
      // opportunistically as each feature migrates.
    },
  },
])
