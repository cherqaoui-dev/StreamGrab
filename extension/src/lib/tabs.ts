export function isFromWatchedTab(senderTabId: number | undefined, watchedTabId: number | null): boolean {
  return watchedTabId !== null && senderTabId === watchedTabId;
}
