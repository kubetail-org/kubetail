/// <reference types="vitest" />
import react from '@vitejs/plugin-react';
import path from 'path';
import { visualizer } from 'rollup-plugin-visualizer';
import { defineConfig, splitVendorChunkPlugin } from 'vite';
import svgr from 'vite-plugin-svgr';
import tsconfigPaths from 'vite-tsconfig-paths';

export default defineConfig({
  plugins: [
    tsconfigPaths(),
    svgr(),
    splitVendorChunkPlugin(),
    react(),
    visualizer(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '^/api/auth/.*': {
        target: 'http://localhost:4000',
      },
      '^/csrf-token': {
        target: 'http://localhost:4000',
      },
      '^/graphql': {
        target: 'http://localhost:4000',
        ws: true,
      },
    }
  },
  build: {
    manifest: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts']
  }
});
