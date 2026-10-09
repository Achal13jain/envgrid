export type ThemeChoice = "system" | "light" | "dark";

const KEY = "envgrid-theme";

export function getTheme(): ThemeChoice {
  try {
    const t = localStorage.getItem(KEY);
    return t === "light" || t === "dark" ? t : "system";
  } catch {
    return "system";
  }
}

export function applyTheme(choice: ThemeChoice) {
  try {
    if (choice === "system") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, choice);
  } catch {
    /* storage blocked: the choice lasts for this page only */
  }
  const dark = choice === "dark" || (choice === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
}

/** Display size in percent of the browser's text size: 90 is the default (set in index.css); 100 is larger, 80 fits more. */
export type SizeChoice = "100" | "90" | "80";

const SIZE_KEY = "envgrid-size";

export function getSize(): SizeChoice {
  try {
    const s = localStorage.getItem(SIZE_KEY);
    return s === "100" || s === "80" ? s : "90";
  } catch {
    return "90";
  }
}

export function applySize(choice: SizeChoice) {
  try {
    if (choice === "90") localStorage.removeItem(SIZE_KEY);
    else localStorage.setItem(SIZE_KEY, choice);
  } catch {
    /* storage blocked: the choice lasts for this page only */
  }
  document.documentElement.style.fontSize = choice === "90" ? "" : `${choice}%`;
}
