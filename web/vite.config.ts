import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The Go server serves the build from "/" and holds anything under /assets/
// indefinitely, so the asset directory name and the absolute base have to stay
// as they are here.
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    emptyOutDir: true,
  },
  server: {
    // `npm run dev` serves the interface itself and forwards every API call to
    // a locally running gateway, so the dev server never needs its own copy of
    // the credentials.
    proxy: {
      '/v1': 'http://localhost:8230',
    },
  },
})
