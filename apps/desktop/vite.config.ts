import babel from '@rolldown/plugin-babel';
import tailwindcss from '@tailwindcss/vite';
import react, { reactCompilerPreset } from '@vitejs/plugin-react';
import Unfonts from 'unplugin-fonts/vite';
import { defineConfig, mergeConfig } from 'vite';
import svgr from 'vite-plugin-svgr';
import { coverageConfigDefaults, defineConfig as defineVitestConfig } from 'vitest/config';

// https://vite.dev/config/
const host = process.env.TAURI_DEV_HOST;

const viteConfig = (coverage: boolean) =>
  defineConfig({
    resolve: {
      tsconfigPaths: true,
    },
    plugins: [
      react(),
      !coverage &&
        babel({
          presets: [reactCompilerPreset()],
        }),
      svgr(),
      tailwindcss(),
      Unfonts({
        fontsource: {
          families: ['Roboto Flex Variable', 'Roboto Mono Variable'],
        },
      }),
    ],

    // Vite options tailored for Tauri development and only applied in `tauri dev` or `tauri build`
    //
    // 1. prevent Vite from obscuring rust errors
    clearScreen: false,
    // 2. tauri expects a fixed port, fail if that port is not available
    server: {
      port: 1420,
      strictPort: true,
      host: host || '0.0.0.0',
      hmr: host
        ? {
            protocol: 'ws',
            host,
            port: 1421,
          }
        : undefined,
      watch: {
        // 3. tell Vite to ignore watching `src-tauri`
        ignored: ['**/src-tauri/**'],
      },
    },
  });

const vitestConfig = (coverage: boolean) =>
  defineVitestConfig({
    test: {
      environment: 'jsdom',
      globals: true,
      setupFiles: ['./vitest.setup.ts'],
      // Node's own localStorage, on by default since Node 25, shadows jsdom's
      // and is undefined without --localstorage-file.
      execArgv: ['--no-experimental-webstorage'],
      // CI uploads lcov.info to Codecov, which also ignores what this excludes.
      coverage: {
        enabled: coverage,
        provider: 'v8',
        reporter: ['text', 'lcovonly'],
        include: ['src/**/*.{ts,tsx}', 'lint/**/*.js'],
        // Generated output, the router's wiring, the entry point, and the test
        // harness itself: a test would assert nothing the suite doesn't already.
        exclude: [
          ...coverageConfigDefaults.exclude,
          'src/gql/**',
          'src/routeTree.ts',
          'src/main.tsx',
          'src/test-utils.tsx',
        ],
      },
    },
  });

// `vitest run --mode coverage` measures coverage on the source as written. The
// React compiler strips comments inside components and hooks, `v8 ignore`
// markers included, and adds a cached branch per memoized value that only an
// unchanged re-render reaches. Every other run keeps the compiler.
export default defineVitestConfig(({ mode }) => {
  const coverage = mode === 'coverage';
  return mergeConfig(viteConfig(coverage), vitestConfig(coverage));
});
