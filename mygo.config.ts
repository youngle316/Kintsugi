import { defineConfig } from "mygo-cli";
import pkg from "./package.json" with { type: "json" };

// The two values of the release. Until they are set, builds are signed ad
// hoc and have no updates; the release workflow refuses to run.
//
// UPDATE_PUBLIC_KEY: the contents of mygo-update.pub (`go tool mygo keygen`).
// The private key stays out of the repository, in the MYGO_UPDATER_PRIVATE_KEY
// secret: apps accept only updates signed with it.
const UPDATE_PUBLIC_KEY = "wHdH3Ul08k8VoS44qTRTBR57q7fBoK0oRc8AQarMVy4=";
// SIGNING_IDENTITY: the Apple Development certificate of the Personal Team,
// as `security find-identity -v -p codesigning` prints it:
// "Apple Development: name (XXXXXXXXXX)". Every version must be signed with
// it, or macOS forgets the Accessibility permission after an update.
const SIGNING_IDENTITY = "Apple Development: youngle316@gmail.com (K542289BP9)";

export default defineConfig({
  name: "Kintsugi",
  slug: "kintsugi",
  identifier: "com.youngle316.kintsugi",
  // The version is package.json's; the release workflow checks the tag.
  version: pkg.version,
  out: "build",
  // The source of resources/icon.png, which the app does not need.
  resourcesExclude: ["icon.svg"],
  ...(UPDATE_PUBLIC_KEY && {
    updates: { publicKey: UPDATE_PUBLIC_KEY, github: "youngle316/Kintsugi" },
  }),
  macos: {
    minimumSystemVersion: "13.0",
    // A menu bar app: no Dock icon, no app menu.
    infoPlist: { LSUIElement: true },
    // KINTSUGI_SIGNING_IDENTITY overrides it, such as "-" to build ad hoc.
    // mygo dev signs the development app with an Apple Development
    // identity of this identity's team, so not the client team's.
    signingIdentity: process.env.KINTSUGI_SIGNING_IDENTITY || SIGNING_IDENTITY || "-",
  },
});
