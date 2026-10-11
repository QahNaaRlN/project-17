import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { manifest: true, outDir: "dist/client", rollupOptions: { input: "src/client.tsx" } },
  ssr: { noExternal: ["@cms/core", "@cms/ir", "@cms/runtime", "@cms/react"] },
});
