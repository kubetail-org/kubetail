import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import path from 'path';
import Unfonts from 'unplugin-fonts/vite';
import { defineConfig, loadEnv, mergeConfig } from 'vite';
import svgr from 'vite-plugin-svgr';
import { defineConfig as defineVitestConfig } from 'vitest/config';

export default ({ mode }: { mode: string }) => {
  const env = loadEnv(mode, process.cwd());

  const backendTarget = `http://localhost:${env.VITE_DASHBOARD_PROXY_PORT}`;

  const viteConfig = defineConfig({
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    plugins: [
      svgr(),
      react(),
      tailwindcss(),
      Unfonts({
        fontsource: {
          families: [
            {
              name: 'Roboto-Flex',
              variable: true,
            },
          ],
        },
      }),
    ],
    optimizeDeps: {
      include: ['react', 'react-dom'],
    },
    server: {
      host: true,
      proxy: {
        '^/api/.*': {
          target: backendTarget,
        },
        '^/cluster-api-proxy/.*': {
          target: backendTarget,
          ws: true,
        },
        '^/graphql': {
          target: backendTarget,
          ws: true,
        },
        '^/healthz': {
          target: backendTarget,
        },
      },
    },
    build: {
      manifest: true,
      sourcemap: true,
      rolldownOptions: {
        output: {
          manualChunks(id: string) {
            if (id.includes('/node_modules/')) return 'vendor';
          },
        },
      },
    },
  });

  const vitestConfig = defineVitestConfig({
    test: {
      environment: 'jsdom',
      globals: true,
      setupFiles: ['./vitest.setup.ts'],
      coverage: {
        provider: 'v8',
        include: ['src/**/*.{ts,tsx}'],
        exclude: ['src/**/*.test.{ts,tsx}', 'src/**/__generated__/**', 'src/mocks/**', 'src/test-utils.tsx'],
        // Paths from the repo root, as Codecov and tools/coverdiff read them.
        reporter: ['text-summary', ['lcov', { projectRoot: '..' }]],
      },
    },
  });

  return mergeConfig(viteConfig, vitestConfig);
};
