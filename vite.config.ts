import { defineConfig } from "vite";
export default defineConfig({
  build: {
    outDir: "internal/httpapi/web",
    emptyOutDir: false,
    target: "es2022",
    rollupOptions: {
      input: "ui/site.ts",
      output: {
        entryFileNames: "site.js",
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: "[name].[ext]",
      },
    },
  },
});
