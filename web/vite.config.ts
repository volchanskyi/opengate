import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        // Each engine that loads on one lazy route gets a stable chunk name, so
        // it is budgeted and regression-gated on its own rather than inside the
        // total the application's own routes are measured by. Both are large,
        // both are a single dependency, and neither is on the first-paint path:
        // uPlot loads on the device-detail route, xterm on the session route.
        //
        // Splitting is not an exemption — a named chunk carries its own limit in
        // .size-limit.json, which scripts/tests/bundle-budget-coverage.test.sh
        // holds it to. What it buys is attribution: a regression in the routes
        // stops being hidden under a dependency nobody changed, and a dependency
        // that grew stops spending the routes' headroom.
        manualChunks(id: string) {
          if (id.includes('node_modules/uplot')) return 'charts'
          if (id.includes('node_modules/@xterm')) return 'terminal'
          return undefined
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
})
