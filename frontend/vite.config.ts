import { defineConfig } from "vite";

export default defineConfig({
  build: {
    outDir: "../internal/server/dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: "index.html",
        admin: "admin.html",
      },
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:3030",
        ws: true,
      },
    },
  },
});
