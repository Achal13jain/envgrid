/**
 * Keyboard model of the file grid, kept pure so it can be tested.
 *
 * Column 0 is the key name (row header); columns 1..n are environments.
 * Rows are the visible (filtered) keys.
 */
export interface GridState {
  rows: number;
  cols: number;
  row: number;
  col: number;
  editing: boolean;
}

export type GridAction =
  | { type: "move"; dRow: number; dCol: number }
  | { type: "rowStart" }
  | { type: "rowEnd" }
  | { type: "gridStart" }
  | { type: "gridEnd" }
  | { type: "focus"; row: number; col: number }
  | { type: "edit" }
  | { type: "stopEditing" }
  | { type: "resize"; rows: number; cols: number };

const clamp = (n: number, max: number) => Math.max(0, Math.min(n, Math.max(0, max - 1)));

export function initialGridState(rows: number, cols: number): GridState {
  return { rows, cols, row: 0, col: cols > 1 ? 1 : 0, editing: false };
}

export function gridReducer(s: GridState, a: GridAction): GridState {
  // While a cell is being edited, only leaving edit mode or a resize applies.
  if (s.editing && a.type !== "stopEditing" && a.type !== "resize") return s;
  switch (a.type) {
    case "move":
      return { ...s, row: clamp(s.row + a.dRow, s.rows), col: clamp(s.col + a.dCol, s.cols) };
    case "rowStart":
      return { ...s, col: 0 };
    case "rowEnd":
      return { ...s, col: clamp(s.cols - 1, s.cols) };
    case "gridStart":
      return { ...s, row: 0, col: 0 };
    case "gridEnd":
      return { ...s, row: clamp(s.rows - 1, s.rows), col: clamp(s.cols - 1, s.cols) };
    case "focus":
      return { ...s, row: clamp(a.row, s.rows), col: clamp(a.col, s.cols) };
    case "edit":
      // Only value cells are editable, and only when a row exists.
      return s.rows > 0 && s.col > 0 ? { ...s, editing: true } : s;
    case "stopEditing":
      return { ...s, editing: false };
    case "resize": {
      const next = { ...s, rows: a.rows, cols: a.cols, row: clamp(s.row, a.rows), col: clamp(s.col, a.cols) };
      if (a.rows === 0) next.editing = false;
      return next;
    }
  }
}

/** Commands handled by the grid component rather than the reducer. */
export type GridCommand =
  | "copy"
  | "reveal"
  | "history"
  | "delete"
  | "search"
  | "moveRowUp"
  | "moveRowDown"
  | "keyActions";

export interface KeyInput {
  key: string;
  altKey?: boolean;
  ctrlKey?: boolean;
  metaKey?: boolean;
  shiftKey?: boolean;
}

/** Maps a keydown (outside edit mode) to a reducer action or a command. */
export function keyToIntent(e: KeyInput): { action: GridAction } | { command: GridCommand } | null {
  const mod = e.ctrlKey || e.metaKey;
  if (e.altKey && !mod) {
    if (e.key === "ArrowUp") return { command: "moveRowUp" };
    if (e.key === "ArrowDown") return { command: "moveRowDown" };
    return null;
  }
  switch (e.key) {
    case "ArrowUp":
      return { action: { type: "move", dRow: -1, dCol: 0 } };
    case "ArrowDown":
      return { action: { type: "move", dRow: 1, dCol: 0 } };
    case "ArrowLeft":
      return { action: { type: "move", dRow: 0, dCol: -1 } };
    case "ArrowRight":
      return { action: { type: "move", dRow: 0, dCol: 1 } };
    case "PageUp":
      return { action: { type: "move", dRow: -10, dCol: 0 } };
    case "PageDown":
      return { action: { type: "move", dRow: 10, dCol: 0 } };
    case "Home":
      return { action: mod ? { type: "gridStart" } : { type: "rowStart" } };
    case "End":
      return { action: mod ? { type: "gridEnd" } : { type: "rowEnd" } };
  }
  if (mod) return null; // leave browser shortcuts such as Ctrl+C alone
  switch (e.key) {
    case "Enter":
    case "F2":
      return { action: { type: "edit" } };
    case "c":
      return { command: "copy" };
    case "r":
      return { command: "reveal" };
    case "h":
      return { command: "history" };
    case "/":
      return { command: "search" };
    case "Delete":
    case "Backspace":
      return { command: "delete" };
    case "m":
    case "ContextMenu":
      return { command: "keyActions" };
  }
  return null;
}
