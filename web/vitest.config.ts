import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Vitest auto-loads the `github-actions` reporter when GITHUB_ACTIONS=true and no
// reporters are configured (see vitest's reporter resolver:
// `if (process.env.GITHUB_ACTIONS === "true") resolved.reporters.push(["github-actions", {}])`).
// Under Stryker each per-mutant vitest re-run would then append a duplicate
// "Vitest Test Report" section to GITHUB_STEP_SUMMARY — hundreds per mutation
// workflow run. Pinning reporters to ['default'] when invoked under Stryker
// skips that auto-load branch (resolved.reporters.length > 0). Normal CI test
// jobs (web-unit, web-integration) leave reporters undefined and keep the
// auto-loaded github-actions reporter so test failures still surface as GitHub
// annotations.
const isUnderStryker = Boolean(process.env.STRYKER_MUTATOR_WORKER)

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./vitest.setup.ts'],
    globals: true,
    exclude: ['**/e2e/**', '**/node_modules/**', '**/dist/**', '**/.stryker-tmp/**'],
    ...(isUnderStryker ? { reporters: ['default' as const] } : {}),
    coverage: {
      provider: 'v8',
      reporter: ['lcov', 'text', 'text-summary', 'json-summary'],
      reportsDirectory: './coverage',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/**/*.d.ts',
        'src/main.tsx',
        'src/types/**',
        'src/test-utils/**',
        // UI bootstrap / glue components without standalone logic — covered by e2e
        'src/App.tsx',
      ],
    },
  },
})
