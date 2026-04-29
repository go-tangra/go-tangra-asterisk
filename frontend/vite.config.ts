import { federation } from '@module-federation/vite';
import vue from '@vitejs/plugin-vue';
import { defineConfig } from 'vite';

export default defineConfig(({ command }) => ({
  // Dev: '/' so the dev server can serve at root.
  // Production: nginx proxies /modules/asterisk/* to this remote.
  base: command === 'serve' ? '/' : '/modules/asterisk/',
  plugins: [
    vue(),
    federation({
      name: 'asterisk',
      filename: 'remoteEntry.js',
      remotes: {
        shell: {
          type: 'module',
          name: 'shell',
          entry:
            command === 'serve'
              ? 'http://localhost:5666/remoteEntry.js'
              : '/remoteEntry.js',
        },
      },
      exposes: {
        './module': './src/index.ts',
      },
      shared: {
        vue: { singleton: true },
        'vue-router': { singleton: true },
        pinia: { singleton: true },
        'ant-design-vue': { singleton: true },
      },
      dts: false,
    }),
  ],
  server: {
    port: 3013,
    strictPort: true,
    origin: 'http://localhost:3013',
    cors: true,
  },
  build: {
    target: 'esnext',
    minify: true,
  },
}));
