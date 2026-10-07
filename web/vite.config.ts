import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        // uPlot and xterm each load on one lazy route; named chunks give each its own size limit
        // in .size-limit.json, so route regressions are not hidden under an unchanged dependency.
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
