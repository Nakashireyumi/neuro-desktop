import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dashboard is normally served by the Go bridge itself
// (http://127.0.0.1:8300/ui/), so the production build must keep relative asset
// URLs. During `npm run dev` the bridge is usually on another port, so proxy the
// API calls instead of hardcoding a host in the app.
const BRIDGE = process.env.NEURO_ADMIN_URL ?? "http://127.0.0.1:8300";

export default defineConfig({
  base: "",
  plugins: [react()],
  server: {
    host: true,
    proxy: {
      "/api": { target: BRIDGE, changeOrigin: true },
      "/health": { target: BRIDGE, changeOrigin: true },
    },
  },
  build: {
    rollupOptions: {
      output: {
        format: "iife",
        inlineDynamicImports: true,
      },
    },
    minify: false,
    sourcemap: true,
    integrity: false,
  },
});
