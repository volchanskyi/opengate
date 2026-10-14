import { existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// The scenarios both hand-off suites replay, found upward so a copied tree still reaches them.
function sharedScenarios(): string {
  for (let dir = dirname(fileURLToPath(import.meta.url)); ; dir = dirname(dir)) {
    const candidate = join(dir, 'testdata', 'direct-link')
    if (existsSync(candidate)) return candidate
    if (dirname(dir) === dir) throw new Error('testdata/direct-link is not above web/')
  }
}
const scenarios = sharedScenarios()

// Under Stryker the reporters are pinned to default, which skips vitest's auto-loaded
// github-actions reporter and its duplicate summary section per mutant run.
const isUnderStryker = Boolean(process.env.STRYKER_MUTATOR_WORKER)

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@direct-link-scenarios': scenarios } },
  server: { fs: { allow: ['.', scenarios] } },
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
        // App.tsx is UI glue without standalone logic and is covered by e2e.
        'src/App.tsx',
      ],
    },
  },
})
