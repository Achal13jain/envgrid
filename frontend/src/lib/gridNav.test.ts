import { describe, expect, it } from "vitest";
import { gridReducer, initialGridState, keyToIntent, type GridAction, type GridState } from "./gridNav";

const run = (s: GridState, ...actions: GridAction[]) => actions.reduce(gridReducer, s);
const move = (dRow: number, dCol: number): GridAction => ({ type: "move", dRow, dCol });

describe("gridReducer", () => {
  it("starts on the first value cell", () => {
    expect(initialGridState(5, 4)).toMatchObject({ row: 0, col: 1, editing: false });
    expect(initialGridState(5, 1)).toMatchObject({ col: 0 });
  });

  it("moves with arrows and clamps at every edge", () => {
    const s = initialGridState(3, 4);
    expect(run(s, move(1, 0), move(1, 0), move(1, 0))).toMatchObject({ row: 2 });
    expect(run(s, move(-1, 0))).toMatchObject({ row: 0 });
    expect(run(s, move(0, 1), move(0, 1), move(0, 1))).toMatchObject({ col: 3 });
    expect(run(s, move(0, -1), move(0, -1))).toMatchObject({ col: 0 });
    expect(run(s, move(10, 0))).toMatchObject({ row: 2 });
  });

  it("jumps with Home/End and Ctrl+Home/End", () => {
    const s = run(initialGridState(5, 4), move(2, 1));
    expect(run(s, { type: "rowStart" })).toMatchObject({ row: 2, col: 0 });
    expect(run(s, { type: "rowEnd" })).toMatchObject({ row: 2, col: 3 });
    expect(run(s, { type: "gridStart" })).toMatchObject({ row: 0, col: 0 });
    expect(run(s, { type: "gridEnd" })).toMatchObject({ row: 4, col: 3 });
  });

  it("only edits value cells and ignores navigation while editing", () => {
    const s = initialGridState(3, 3);
    const editing = run(s, { type: "edit" });
    expect(editing.editing).toBe(true);
    expect(run(editing, move(1, 0))).toBe(editing);
    expect(run(editing, { type: "focus", row: 2, col: 2 })).toBe(editing);
    expect(run(editing, { type: "stopEditing" })).toMatchObject({ editing: false, row: 0, col: 1 });
    expect(run(s, { type: "rowStart" }, { type: "edit" }).editing).toBe(false);
  });

  it("cannot edit an empty grid", () => {
    expect(run(initialGridState(0, 3), { type: "edit" }).editing).toBe(false);
  });

  it("keeps the cursor inside the grid when it shrinks", () => {
    const s = run(initialGridState(10, 5), { type: "focus", row: 9, col: 4 });
    expect(run(s, { type: "resize", rows: 3, cols: 2 })).toMatchObject({ row: 2, col: 1 });
    expect(run(s, { type: "edit" }, { type: "resize", rows: 0, cols: 2 })).toMatchObject({ row: 0, editing: false });
  });

  it("clamps direct focus", () => {
    expect(run(initialGridState(2, 2), { type: "focus", row: 7, col: -3 })).toMatchObject({ row: 1, col: 0 });
  });
});

describe("keyToIntent", () => {
  it("maps arrows, paging and edit keys", () => {
    expect(keyToIntent({ key: "ArrowDown" })).toEqual({ action: move(1, 0) });
    expect(keyToIntent({ key: "ArrowLeft" })).toEqual({ action: move(0, -1) });
    expect(keyToIntent({ key: "PageDown" })).toEqual({ action: move(10, 0) });
    expect(keyToIntent({ key: "Home", ctrlKey: true })).toEqual({ action: { type: "gridStart" } });
    expect(keyToIntent({ key: "End" })).toEqual({ action: { type: "rowEnd" } });
    expect(keyToIntent({ key: "Enter" })).toEqual({ action: { type: "edit" } });
    expect(keyToIntent({ key: "F2" })).toEqual({ action: { type: "edit" } });
  });

  it("maps single-letter commands", () => {
    expect(keyToIntent({ key: "c" })).toEqual({ command: "copy" });
    expect(keyToIntent({ key: "r" })).toEqual({ command: "reveal" });
    expect(keyToIntent({ key: "h" })).toEqual({ command: "history" });
    expect(keyToIntent({ key: "/" })).toEqual({ command: "search" });
    expect(keyToIntent({ key: "Delete" })).toEqual({ command: "delete" });
  });

  it("reorders rows with Alt+arrows", () => {
    expect(keyToIntent({ key: "ArrowUp", altKey: true })).toEqual({ command: "moveRowUp" });
    expect(keyToIntent({ key: "ArrowDown", altKey: true })).toEqual({ command: "moveRowDown" });
    expect(keyToIntent({ key: "ArrowLeft", altKey: true })).toBeNull();
  });

  it("leaves browser shortcuts and unknown keys alone", () => {
    expect(keyToIntent({ key: "c", ctrlKey: true })).toBeNull();
    expect(keyToIntent({ key: "r", metaKey: true })).toBeNull();
    expect(keyToIntent({ key: "x" })).toBeNull();
  });
});
