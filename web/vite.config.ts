import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:32626",
        changeOrigin: true,
      },
      "/healthz": "http://127.0.0.1:32626",
      "/readyz": "http://127.0.0.1:32626",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 700,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("node_modules/recharts") || id.includes("node_modules/d3-")) return "charts";
          if (id.includes("node_modules/@uiw/react-codemirror") || id.includes("node_modules/@codemirror")) return "editor";
          if (id.includes("node_modules/@tanstack")) return "router";
          if (id.includes("node_modules/react/") || id.includes("node_modules/react-dom/")) return "vendor";
          return undefined;
        },
      },
    },
  },
});

