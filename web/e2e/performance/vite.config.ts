import { fileURLToPath } from 'node:url';
import { defineConfig, mergeConfig } from 'vite';
import appConfig from '../../vite.config.ts';

export default defineConfig(async (env) => mergeConfig(await appConfig(env), {
  build: {
    outDir: fileURLToPath(new URL('../../test-results/matrix-performance/app', import.meta.url)),
    rollupOptions: { input: fileURLToPath(new URL('./index.html', import.meta.url)) },
  },
}));
