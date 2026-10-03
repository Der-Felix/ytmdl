import { fileURLToPath, URL } from 'node:url'
import { readFileSync, writeFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { createHash } from 'node:crypto'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The backend sends no CORS headers, so the frontend has to be same-origin
// with the API. In production nginx does that; during development this proxy
// stands in for it. Buffering is disabled so that /api/v1/events arrives as a
// live stream rather than in chunks.
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: 'ytmdl-offline-shell',
      apply: 'build',
      writeBundle(options) {
        const dir = options.dir || 'dist'
        const version = JSON.parse(
          readFileSync(
            fileURLToPath(new URL('./package.json', import.meta.url)),
            'utf8',
          ),
        ).version
        const shell =
          version +
          '-' +
          createHash('sha256')
            .update(readFileSync(join(dir, 'index.html')))
            .digest('hex')
            .slice(0, 16)
        const assets = [
          '/',
          '/offline',
          ...readdirSync(join(dir, 'assets'))
            .filter((name) => !name.endsWith('.map'))
            .map((name) => '/assets/' + name),
          '/favicon.svg',
          '/favicon.png',
          '/favicon-32x32.png',
          '/logo-mark.png',
          '/logo-full.png',
        ]
        writeFileSync(
          join(dir, 'offline-shell.json'),
          JSON.stringify({ version: shell, assets }),
        )
        const worker = join(dir, 'offline-sw.js')
        writeFileSync(
          worker,
          readFileSync(worker, 'utf8').replace(
            '__YTMDL_SHELL_VERSION__',
            shell,
          ),
        )
      },
    },
  ],
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
            if (
              proxyRes.headers['content-type']?.includes('text/event-stream')
            ) {
              delete proxyRes.headers['content-length']
            }
          })
        },
      },
    },
  },
})
