import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        // Give the charting engine a stable chunk name so it can be budgeted and
        // regression-gated separately from the first-paint bundle. uPlot only
        // loads on the (already lazy) device-detail route.
        manualChunks(id: string) {
          if (id.includes('node_modules/uplot')) return 'charts'
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
