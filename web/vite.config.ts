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
    // Both routes keep the page's Host, so the server hands out relay addresses on this origin
    // and its same-origin check on the relay upgrade passes.
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: false },
      '/ws': { target: 'ws://localhost:8080', ws: true, changeOrigin: false },
    },
  },
})
