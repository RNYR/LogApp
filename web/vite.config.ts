import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// Sklad SPA build configuration.
//
// The Go backend embeds this build from `web/dist` (see `web/embed.go`), so the
// output directory MUST stay `dist` and hashed assets stay under `assets/`.
//
// During development the SPA is served by Vite and all backend traffic is
// proxied to the Go app (default `SKLAD_HTTP_ADDR=:8080`), including the
// `/ca.crt` device-trust bootstrap route. Override the target here if your
// backend listens elsewhere.
const BACKEND_ORIGIN = loadEnv('development', '.', 'SKLAD_').SKLAD_DEV_BACKEND || 'http://localhost:8080'

export default defineConfig(({ mode }) => ({
  plugins: [react(), {
    name: 'sklad-explicit-preview',
    transformIndexHtml() {
      return mode === 'preview' ? [{ tag: 'script', attrs: { src: '/preview-mock.js' }, injectTo: 'head' as const }] : []
    },
  }],
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/api': { target: BACKEND_ORIGIN, changeOrigin: true },
      '/ca.crt': { target: BACKEND_ORIGIN, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2020',
  },
}))
