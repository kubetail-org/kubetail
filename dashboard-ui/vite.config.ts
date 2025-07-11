import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react-swc';
import Unfonts from 'unplugin-fonts/vite';
import { defineConfig, loadEnv, mergeConfig } from 'vite';
import svgr from 'vite-plugin-svgr';
import tsconfigPaths from 'vite-tsconfig-paths';
import { defineConfig as defineVitestConfig } from 'vitest/config';

export default ({ mode }: { mode: string }) => {
  const env = loadEnv(mode, process.cwd());

  const backendTarget = `http://localhost:${env.VITE_DASHBOARD_PROXY_PORT}`;

  const viteConfig = defineConfig({
    plugins: [
      tsconfigPaths(),
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
        '^/csrf-token': {
          target: backendTarget,
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
