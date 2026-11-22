import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Kintsugi",
  slug: "kintsugi",
  identifier: "com.youngle316.kintsugi",
  version: "0.1.0",
  // `mygo dev` runs devCommand and loads devUrl; `mygo build` runs
  // buildCommand and embeds frontendDist into the app.
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  macos: {
    minimumSystemVersion: "13.0",
    // A menu bar app: no Dock icon, no app menu.
    infoPlist: { LSUIElement: true },
    // Production builds: an "Apple Development: …" or "Developer ID
    // Application: …" identity keeps the Accessibility permission across
    // builds (README). mygo dev picks an Apple Development one itself.
    signingIdentity: process.env.KINTSUGI_SIGNING_IDENTITY ?? "-",
  },
});
