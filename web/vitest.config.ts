import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Under Stryker the reporters are pinned to default, which skips vitest's auto-loaded
// github-actions reporter and its duplicate summary section per mutant run.
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
        // App.tsx is UI glue without standalone logic and is covered by e2e.
        'src/App.tsx',
      ],
    },
  },
})
