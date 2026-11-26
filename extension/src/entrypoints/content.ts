import { parseYouTubeUrl } from "../lib/youtube";

export default defineContentScript({
  matches: ["*://*.youtube.com/*"],
  runAt: "document_idle",
  main() {
    const publish = () => {
      const parsed = parseYouTubeUrl(location.href);
      if (!parsed) {
        void browser.runtime.sendMessage({ type: "PAGE_CLEAR" });
        return;
      }
      void browser.runtime.sendMessage({
        type: "PAGE_VIDEO",
        payload: {
          ...parsed,
          title: readTitle(),
          channel: readChannel(),
          thumbnail: `https://i.ytimg.com/vi/${parsed.videoId}/hqdefault.jpg`,
        },
      });
    };
    publish();
    document.addEventListener("yt-navigate-finish", publish);
    window.addEventListener("popstate", publish);
  },
});

function readTitle(): string {
  const selectors = [
    "h1.ytd-watch-metadata yt-formatted-string",
    "h1 yt-formatted-string",
    "yt-shorts-video-title h2",
    "#title h1",
  ];
  for (const selector of selectors) {
    const text = document.querySelector(selector)?.textContent?.trim();
    if (text) return text;
  }
  return document.title.replace(/ - YouTube$/, "");
}

function readChannel(): string {
  const selectors = ["ytd-channel-name a", "#channel-name a", "yt-content-metadata-view-model a"];
  for (const selector of selectors) {
    const text = document.querySelector(selector)?.textContent?.trim();
    if (text) return text;
  }
  return "";
}
