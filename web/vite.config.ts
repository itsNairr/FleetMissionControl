import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev the browser talks only to Vite (:5173); Vite forwards /api (REST + WebSockets) to the Go gateway.
// Same-origin from the browser's point of view, so no CORS juggling during development.
const apiTarget = process.env.FLEET_API_TARGET ?? 'http://localhost:8081'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: apiTarget, ws: true },
    },
  },
  build: {
    chunkSizeWarningLimit: 1200, // three.js is big; it is split into its own lazy chunk
  },
})
