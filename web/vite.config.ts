import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// Stable local asset paths retain offline operation and the Go server's exact
// allowlist. No CDN, server-side Node runtime, inline code or eval is required.
export default defineConfig({
  plugins: [vue()],
  // Local development only: keep the backend's Host/Origin and cookie checks.
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:18088',
        changeOrigin: true,
        headers: { Origin: 'http://127.0.0.1:18088' },
      },
    },
  },
  build: {
    target: 'es2022',
    cssCodeSplit: false,
    modulePreload: false,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/panel.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/panel.[ext]',
      },
    },
  },
});
