import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** True when a keyboard event comes from a text field, where letters must type. */
export function isTyping(e: KeyboardEvent | React.KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null;
  if (!el) return false;
  return el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName);
}

/** Copies text, falling back to execCommand where the Clipboard API is unavailable (plain HTTP). */
export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return;
  } catch {
    /* fall back below */
  }
  const previous = document.activeElement as HTMLElement | null;
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  const ok = document.execCommand("copy");
  ta.remove();
  previous?.focus();
  if (!ok) throw new Error("The browser blocked clipboard access.");
}

/** Saves text as a file in the browser. */
export function downloadText(name: string, body: string, type: string) {
  const url = URL.createObjectURL(new Blob([body], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
