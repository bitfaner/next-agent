import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const gfmEmailLookbehind = String.raw`/(?<=^|\s|\p{P}|\p{S})([-.\w+]+)@([-\w]+(?:\.[-\w]+)+)/gu`;
const gfmEmailCompatible = String.raw`/([-.\w+]+)@([-\w]+(?:\.[-\w]+)+)/gu`;

export default defineConfig({
  plugins: [
    react(),
    {
      name: "macos-webkit-gfm-compatibility",
      transform(code, id) {
        if (!id.includes("mdast-util-gfm-autolink-literal/lib/index.js")) return;
        if (!code.includes(gfmEmailLookbehind)) {
          throw new Error("Unable to apply the GFM email compatibility transform");
        }

        // The dependency's `previous(match)` check enforces the same prefix rule.
        return code.replace(gfmEmailLookbehind, gfmEmailCompatible);
      }
    }
  ],
  base: "./",
  clearScreen: false,
  server: {
    strictPort: true,
    port: 1420
  },
  envPrefix: ["VITE_", "TAURI_"],
  build: {
    target: "safari13",
    outDir: "dist"
  }
});
