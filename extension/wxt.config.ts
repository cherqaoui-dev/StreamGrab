import { defineConfig } from "wxt";
import preact from "@preact/preset-vite";

const extensionKey =
  "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEArD82BMB4e5gANRreLsijayusRAMGIu7ge7hxX9Dd8ar93V2T17ZE5yYAh/2pMYaD8mNxvSGTndU/IYgq/gFC8uUEt/XNuVeltzbgAX76Tt9JhBBABpW8Mk6+GezSkwR0h4M3JZrdseC465IxmaxOU3mszc885cV9O8knfOkGesidoaRMcS/D9JiEPb+H18coA4+SFiFcdxQIEg7yt/q5hTi8fNh+nWDJUjrOuhzYYUBJd697JpbF/FeCJvN+SJ31uMRCSw9BrgSOHSTPxV8OncY9f7Z8JS7kXJ490er7/59YrDmXX1b14Gq9eYY4NA+NbL/ZhZWQ2S1alwYWS8a3+QIDAQAB";

export default defineConfig({
  srcDir: "src",
  manifest: {
    name: "StreamGrab",
    description: "Inspect and download the YouTube video in the active tab.",
    permissions: ["nativeMessaging", "storage", "activeTab"],
    host_permissions: ["*://*.youtube.com/*"],
    action: { default_title: "StreamGrab" },
    key: extensionKey,
    browser_specific_settings: {
      gecko: {
        id: "streamgrab@local",
        strict_min_version: "128.0",
      },
    },
  },
  vite: () => ({
    plugins: [preact()],
  }),
});
