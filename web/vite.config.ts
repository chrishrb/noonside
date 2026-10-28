/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { VitePWA } from 'vite-plugin-pwa';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig({
  // The deployed commit (APP_VERSION in deploy.yml), also the Sentry release.
  define: { __APP_VERSION__: JSON.stringify(process.env.APP_VERSION ?? process.env.GITHUB_SHA ?? String(Date.now())) },
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      // Registered in src/pwa.ts, which asks before reloading into a new version.
      registerType: 'prompt',
      injectRegister: false,
      // Our own worker (src/sw.ts) for push notifications; the build injects
      // the precache manifest. It is still emitted as sw.js.
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      includeAssets: ['favicon.svg', 'favicon.ico', 'apple-touch-icon-180x180.png'],
      manifest: {
        id: '/',
        name: 'noonside',
        short_name: 'noonside',
        description: 'Monitoring for photovoltaic systems.',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        orientation: 'any',
        theme_color: '#ffffff',
        background_color: '#fafafa',
        icons: [
          { src: 'pwa-64x64.png', sizes: '64x64', type: 'image/png' },
          { src: 'pwa-192x192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512x512.png', sizes: '512x512', type: 'image/png' },
          { src: 'maskable-icon-512x512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      injectManifest: {
        globPatterns: ['**/*.{js,css,html,svg,png,ico,woff2}'],
        // Written by the web container at start (web/nginx); never cached.
        globIgnores: ['config.js'],
      },
    }),
  ],
  // The generated Zod schemas (src/api/gen/zod.gen.ts) are plain `z.…()` calls
  // without pure annotations; this lets unused ones be tree-shaken.
  // Hidden source maps: uploaded to Sentry by the deploy, never served.
  build: { sourcemap: 'hidden', rolldownOptions: { treeshake: { manualPureFunctions: ['z'] } } },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    port: 5173,
    strictPort: true,
    // The Go API and the Auth server (GoTrue from compose.yaml, without its
    // /auth/v1 prefix, like the HTTPRoute in deploy/helm/noonside); in
    // production all of them share the app's origin.
    proxy: {
      '/api': 'http://localhost:8080',
      '/media': 'http://localhost:8080',
      '/auth/v1': {
        target: `http://127.0.0.1:${process.env.NOONSIDE_AUTH_PORT || 9999}`,
        rewrite: (p) => p.replace(/^\/auth\/v1/, ''),
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
  },
});
