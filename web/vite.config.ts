import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: 'autoUpdate',
      // Phase 1 only needs installability and an app shell. Offline score entry
      // and background sync land in Phase 3 with the score tracker.
      workbox: {
        globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
        // API responses are never precached: a stale course list is worse than
        // a loading state, and score sync will need its own strategy.
        navigateFallbackDenylist: [/^\/api\//],
      },
      manifest: {
        name: 'Roundly — Golf Scorekeeping',
        short_name: 'Roundly',
        description: 'Track your rounds, courses, and golf stats.',
        theme_color: '#15803d',
        background_color: '#ffffff',
        display: 'standalone',
        start_url: '/',
        icons: [
          { src: '/icon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any' },
          { src: '/icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: '/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
    }),
  ],
  server: {
    port: 5173,
    // The Go API runs separately in development. Proxying keeps the browser on
    // one origin, so cookies and redirects behave as they do in production.
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Recharts is by some distance the largest dependency and the one least
        // likely to change. Splitting it out means shipping an app update
        // re-downloads the app, not the chart library with it - which matters
        // here because the service worker precaches every asset, so a changed
        // filename is a changed download for everyone.
        manualChunks: {
          charts: ['recharts'],
        },
      },
    },
  },
})
