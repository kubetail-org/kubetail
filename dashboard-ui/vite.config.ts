import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import Unfonts from 'unplugin-fonts/vite';
import { defineConfig, loadEnv, mergeConfig } from 'vite';
import svgr from 'vite-plugin-svgr';
import { defineConfig as defineVitestConfig } from 'vitest/config';
import path from 'path';

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
      },
    },
    build: {
      manifest: true,
      sourcemap: true,
      rollupOptions: {
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
    },
  });

  return mergeConfig(viteConfig, vitestConfig);
};
