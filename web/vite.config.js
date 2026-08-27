import { defineConfig } from 'vite'

// The build output is emitted directly into the Go httpapi package directory so
// the compiled binary embeds it via go:embed and serves it at the root path.
export default defineConfig({
  build: {
    outDir: '../internal/httpapi/dist',
    emptyOutDir: true,
  },
})
