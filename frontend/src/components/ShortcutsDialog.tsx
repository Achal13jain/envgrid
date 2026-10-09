import { Dialog, DialogContent, DialogDescription, DialogTitle, Kbd } from "./ui";

export const shortcutGroups: { title: string; items: [string[], string][] }[] = [
  {
    title: "Anywhere",
    items: [[["?"], "Show this list"]],
  },
  {
    // "then" marks keys pressed one after the other, not together.
    title: "Navigation",
    items: [
      [["Ctrl", "K"], "Search repos, files and keys"],
      [["g", "then", "h"], "Go to Home"],
      [["g", "then", "r"], "Go to Repos"],
      [["g", "then", "q"], "Go to Requests"],
      [["g", "then", "i"], "Go to Insights"],
      [["g", "then", "a"], "Go to Activity"],
      [["g", "then", "u"], "Go to Users (admins only)"],
      [["g", "then", "s"], "Go to Settings"],
    ],
  },
  {
    title: "File grid",
    items: [
      [["↑", "↓", "←", "→"], "Move between cells"],
      [["Home"], "First column of the row"],
      [["End"], "Last column of the row"],
      [["Ctrl", "Home"], "First cell of the grid"],
      [["Page Down"], "Ten rows down"],
      [["Enter"], "Edit the value (or open key actions on a key name)"],
      [["c"], "Copy the value"],
      [["r"], "Reveal a secret for 10 seconds"],
      [["h"], "Open the value's history"],
      [["Delete"], "Delete the value, or the key when on a key name"],
      [["Alt", "↑"], "Move the key up"],
      [["Alt", "↓"], "Move the key down"],
      [["m"], "Open key actions"],
      [["/"], "Filter keys"],
    ],
  },
  {
    title: "While editing a value",
    items: [
      [["Enter"], "Save"],
      [["Shift", "Enter"], "New line"],
      [["Esc"], "Cancel"],
    ],
  },
];

export function ShortcutList() {
  return (
    <div className="space-y-5">
      {shortcutGroups.map((g) => (
        <section key={g.title}>
          <h3 className="mb-2 font-bold">{g.title}</h3>
          <dl className="divide-y divide-line">
            {g.items.map(([keys, what]) => (
              <div key={what} className="flex items-center justify-between gap-4 py-1.5">
                <dt className="flex shrink-0 items-center gap-1">
                  {keys.map((k) =>
                    k === "then" ? (
                      <span key={k} className="text-xs text-muted">
                        then
                      </span>
                    ) : (
                      <Kbd key={k}>{k}</Kbd>
                    ),
                  )}
                </dt>
                <dd className="text-right text-sm text-muted">{what}</dd>
              </div>
            ))}
          </dl>
        </section>
      ))}
    </div>
  );
}

export function ShortcutsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>Keyboard shortcuts</DialogTitle>
        <DialogDescription>Everything in envgrid works from the keyboard.</DialogDescription>
        <div className="mt-4">
          <ShortcutList />
        </div>
      </DialogContent>
    </Dialog>
  );
}
