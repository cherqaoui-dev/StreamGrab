import { nextContainer, step } from "../../lib/keyboard";
import type { Settings } from "../../protocol/types";

export type Tab = "video" | "audio" | "queue" | "history";

export const tabs: Tab[] = ["video", "audio", "queue", "history"];

export function moveTab(event: KeyboardEvent, current: Tab, setTab: (tab: Tab) => void) {
  if (event.key === "ArrowDown") {
    event.preventDefault();
    const panel = document.querySelector("[role='tabpanel']");
    const row = panel?.querySelector<HTMLElement>("[data-get], [data-audio-index]");
    (row ?? panel?.querySelector<HTMLElement>("button"))?.focus();
    return;
  }
  if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
  event.preventDefault();
  const next = tabs[step(tabs.indexOf(current), event.key === "ArrowRight" ? 1 : -1, tabs.length)];
  setTab(next);
  (event.currentTarget as HTMLElement).parentElement?.querySelector<HTMLElement>(`[data-tab="${next}"]`)?.focus();
}

export function moveContainer(event: KeyboardEvent, current: string, update: (patch: Partial<Settings>) => void) {
  if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
  event.preventDefault();
  const next = nextContainer(current, event.key === "ArrowRight" ? 1 : -1);
  update({ container: next });
  (event.currentTarget as HTMLElement).parentElement?.querySelector<HTMLElement>(`[data-container="${next}"]`)?.focus();
}

export function moveRow(
  event: KeyboardEvent,
  index: number,
  count: number,
  attribute: string,
  setSelected: (index: number) => void,
) {
  if (event.key === "ArrowUp" && index === 0) {
    event.preventDefault();
    document.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus();
    return;
  }
  if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
  event.preventDefault();
  const next = step(index, event.key === "ArrowDown" ? 1 : -1, count);
  setSelected(next);
  document.querySelector<HTMLElement>(`[${attribute}="${next}"]`)?.focus();
}
