import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The backend sends no CORS headers, so the frontend has to be same-origin
// with the API. In production nginx does that; during development this proxy
// stands in for it. Buffering is disabled so that /api/v1/events arrives as a
// live stream rather than in chunks.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: process.env.YTMDL_API_TARGET ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
        // Server sent events must not be buffered by the dev proxy.
        configure: (proxy) => {
          proxy.on('proxyRes', (proxyRes) => {
            if (proxyRes.headers['content-type']?.includes('text/event-stream')) {
              delete proxyRes.headers['content-length']
            }
          })
        },
      },
    },
  },
})
