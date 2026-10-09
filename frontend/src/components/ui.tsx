/**
 * shadcn/ui-style primitives, trimmed to what envgrid uses. Radix supplies
 * focus management and ARIA; native elements are used where they suffice
 * (select, checkbox).
 */
import { forwardRef, Fragment, useEffect, useId, type ComponentProps, type ReactNode } from "react";
import {
  AlertDialog as AlertPrimitive,
  Dialog as DialogPrimitive,
  DropdownMenu as MenuPrimitive,
  Select as SelectPrimitive,
  Slot,
  Tabs as TabsPrimitive,
} from "radix-ui";
import { cva, type VariantProps } from "class-variance-authority";
import { Check, ChevronDown, CircleAlert, CircleCheck, Info, TriangleAlert, X, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export const buttonVariants = cva(
  "inline-flex cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-md text-sm font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: "bg-accent text-accent-ink hover:bg-accent/90",
        secondary: "border border-line bg-surface text-ink hover:bg-surface-2",
        ghost: "text-ink hover:bg-surface-2",
        danger: "bg-missing text-surface hover:bg-missing/90",
      },
      size: { sm: "h-8 px-2.5", md: "h-9 px-3.5", icon: "size-8" },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

type ButtonProps = ComponentProps<"button"> & VariantProps<typeof buttonVariants> & { asChild?: boolean };

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, asChild, type, ...props },
  ref,
) {
  const Comp = asChild ? Slot.Root : "button";
  return <Comp ref={ref} type={asChild ? undefined : (type ?? "button")} className={cn(buttonVariants({ variant, size }), className)} {...props} />;
});

const field =
  "w-full rounded-md border border-line bg-surface px-3 text-sm text-ink placeholder:text-muted disabled:cursor-not-allowed disabled:opacity-60";

export const Input = forwardRef<HTMLInputElement, ComponentProps<"input">>(function Input({ className, ...props }, ref) {
  return <input ref={ref} className={cn(field, "h-9", className)} {...props} />;
});

export const Textarea = forwardRef<HTMLTextAreaElement, ComponentProps<"textarea">>(function Textarea({ className, ...props }, ref) {
  return <textarea ref={ref} className={cn(field, "min-h-24 py-2 font-mono leading-snug", className)} {...props} />;
});

export interface SelectOption {
  value: string | number;
  label: ReactNode;
  /** Plain text for type-ahead when the label is not a string. */
  textValue?: string;
}

// Radix forbids "" as an item value; "" means "any" in our filters.
const EMPTY = "__empty__";
const encode = (v: string | number) => (v === "" ? EMPTY : String(v));

/**
 * A single-choice dropdown styled like the menus. Radix supplies the
 * listbox semantics, keyboard support and type-ahead; a <label htmlFor>
 * pointing at `id` names it.
 */
export function Select({
  id,
  value,
  onValueChange,
  options,
  className,
  itemClassName,
  disabled,
  "aria-label": ariaLabel,
}: {
  id?: string;
  value: string | number;
  onValueChange: (value: string) => void;
  options: SelectOption[];
  className?: string;
  itemClassName?: string;
  disabled?: boolean;
  "aria-label"?: string;
}) {
  return (
    <SelectPrimitive.Root value={encode(value)} onValueChange={(v) => onValueChange(v === EMPTY ? "" : v)} disabled={disabled}>
      <SelectPrimitive.Trigger
        id={id}
        aria-label={ariaLabel}
        className={cn(field, "flex h-9 cursor-pointer items-center justify-between gap-2 text-left hover:bg-surface-2 data-[state=open]:border-accent", className)}
      >
        <span className="min-w-0 truncate">
          <SelectPrimitive.Value />
        </span>
        <SelectPrimitive.Icon asChild>
          <ChevronDown className="size-4 shrink-0 text-muted" aria-hidden="true" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={4}
          className="z-50 max-h-[min(24rem,var(--radix-select-content-available-height))] min-w-(--radix-select-trigger-width) overflow-hidden rounded-md border border-line bg-surface text-ink shadow-lg"
        >
          <SelectPrimitive.Viewport className="p-1">
            {options.map((o) => (
              <SelectPrimitive.Item
                key={encode(o.value)}
                value={encode(o.value)}
                textValue={o.textValue}
                className={cn(
                  "relative flex cursor-pointer items-center rounded py-1.5 pr-3 pl-8 text-sm outline-none select-none data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50 data-[highlighted]:bg-accent-soft data-[state=checked]:font-semibold",
                  itemClassName,
                )}
              >
                <SelectPrimitive.ItemIndicator className="absolute left-2 inline-flex">
                  <Check className="size-4 text-accent" aria-hidden="true" />
                </SelectPrimitive.ItemIndicator>
                <SelectPrimitive.ItemText>{o.label}</SelectPrimitive.ItemText>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}

export function Label({ className, ...props }: ComponentProps<"label">) {
  return <label className={cn("mb-1 block text-sm font-semibold text-ink", className)} {...props} />;
}

export function Field({ label, hint, htmlFor, children }: { label: string; hint?: ReactNode; htmlFor: string; children: ReactNode }) {
  const hintId = `${htmlFor}-hint`;
  // Ties the hint to the control so screen readers read it with the label,
  // whatever wraps the control (a show-password button, a Radix trigger).
  useEffect(() => {
    const el = hint ? document.getElementById(htmlFor) : null;
    if (!el) return;
    const ids = (el.getAttribute("aria-describedby") ?? "").split(" ").filter(Boolean);
    if (!ids.includes(hintId)) el.setAttribute("aria-describedby", [...ids, hintId].join(" "));
    return () => {
      const rest = (el.getAttribute("aria-describedby") ?? "").split(" ").filter((id) => id && id !== hintId);
      if (rest.length) el.setAttribute("aria-describedby", rest.join(" "));
      else el.removeAttribute("aria-describedby");
    };
  }, [hint, htmlFor, hintId]);
  // A flex gap, not space-y: Radix adds a hidden native select after the
  // trigger, and space-y would give the trigger a bottom margin.
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint && (
        <p id={hintId} className="text-xs text-muted">
          {hint}
        </p>
      )}
    </div>
  );
}

/**
 * Text with line-break opportunities after separators, so long names such as
 * EXPRESS_MODULES_HISTORY_PRIVATE wrap at word edges instead of mid-word.
 * Very long text is left alone.
 */
export function Breakable({ text }: { text: string }) {
  if (text.length > 2000) return <>{text}</>;
  const parts = text.split(/(?<=[_./@:?&=,-])/);
  return (
    <>
      {parts.map((p, i) => (
        <Fragment key={i}>
          {i > 0 && <wbr />}
          {p}
        </Fragment>
      ))}
    </>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="inline-flex min-w-5 items-center justify-center rounded border border-line bg-surface-2 px-1 font-mono text-xs text-ink">
      {children}
    </kbd>
  );
}

export function Badge({ className, ...props }: ComponentProps<"span">) {
  return <span className={cn("inline-flex items-center gap-1 rounded border border-line px-1.5 py-px text-xs font-semibold whitespace-nowrap text-muted", className)} {...props} />;
}

// Dialog and sheet

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export function DialogContent({
  className,
  children,
  side,
  ...props
}: ComponentProps<typeof DialogPrimitive.Content> & { side?: "right" }) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/45" />
      <DialogPrimitive.Content
        className={cn(
          "fixed z-50 overflow-y-auto border-line bg-surface text-ink shadow-xl outline-none",
          side === "right"
            ? "inset-y-0 right-0 w-full max-w-md border-l p-5"
            : "top-1/2 left-1/2 max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border p-5",
          className,
        )}
        {...props}
      >
        {children}
        <DialogPrimitive.Close className={cn(buttonVariants({ variant: "ghost", size: "icon" }), "absolute top-3 right-3")} aria-label="Close">
          <X />
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function DialogTitle({ className, ...props }: ComponentProps<typeof DialogPrimitive.Title>) {
  return <DialogPrimitive.Title className={cn("pr-8 text-lg font-bold", className)} {...props} />;
}

export function DialogDescription({ className, ...props }: ComponentProps<typeof DialogPrimitive.Description>) {
  return <DialogPrimitive.Description className={cn("mt-1 text-sm text-muted", className)} {...props} />;
}

export function DialogFooter({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("mt-5 flex flex-wrap justify-end gap-2", className)} {...props} />;
}

// Confirmation

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  tone = "danger",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  onConfirm: () => void;
  /** "danger" for actions that lose something; "primary" for ones that do not, such as signing out. */
  tone?: "danger" | "primary";
}) {
  return (
    <AlertPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AlertPrimitive.Portal>
        <AlertPrimitive.Overlay className="fixed inset-0 z-40 bg-black/45" />
        <AlertPrimitive.Content className="fixed top-1/2 left-1/2 z-50 w-[calc(100vw-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-line bg-surface p-5 text-ink shadow-xl">
          <AlertPrimitive.Title className="text-lg font-bold">{title}</AlertPrimitive.Title>
          <AlertPrimitive.Description className="mt-2 text-sm text-muted">{description}</AlertPrimitive.Description>
          <div className="mt-5 flex justify-end gap-2">
            <AlertPrimitive.Cancel asChild>
              <Button>Cancel</Button>
            </AlertPrimitive.Cancel>
            <AlertPrimitive.Action asChild>
              <Button variant={tone} onClick={onConfirm}>
                {confirmLabel}
              </Button>
            </AlertPrimitive.Action>
          </div>
        </AlertPrimitive.Content>
      </AlertPrimitive.Portal>
    </AlertPrimitive.Root>
  );
}

// Dropdown menu

export const Menu = MenuPrimitive.Root;
export const MenuTrigger = MenuPrimitive.Trigger;

export function MenuContent({ className, ...props }: ComponentProps<typeof MenuPrimitive.Content>) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.Content
        sideOffset={4}
        className={cn("z-50 min-w-48 rounded-md border border-line bg-surface p-1 text-ink shadow-lg", className)}
        {...props}
      />
    </MenuPrimitive.Portal>
  );
}

export function MenuItem({ className, danger, ...props }: ComponentProps<typeof MenuPrimitive.Item> & { danger?: boolean }) {
  return (
    <MenuPrimitive.Item
      className={cn(
        "flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none select-none data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50 data-[highlighted]:bg-surface-2 [&_svg]:size-4 [&_svg]:text-muted",
        danger && "text-missing [&_svg]:text-missing",
        className,
      )}
      {...props}
    />
  );
}

export function MenuLabel({ className, ...props }: ComponentProps<typeof MenuPrimitive.Label>) {
  return <MenuPrimitive.Label className={cn("px-2 py-1.5 text-xs text-muted", className)} {...props} />;
}

export function MenuSeparator() {
  return <MenuPrimitive.Separator className="my-1 h-px bg-line" />;
}

// Tabs

export const Tabs = TabsPrimitive.Root;
export const TabsContent = TabsPrimitive.Content;

export function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return <TabsPrimitive.List className={cn("mb-4 flex gap-1 border-b border-line", className)} {...props} />;
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        "-mb-px cursor-pointer border-b-2 border-transparent px-3 py-2 text-sm font-semibold text-muted hover:text-ink data-[state=active]:border-accent data-[state=active]:text-ink",
        className,
      )}
      {...props}
    />
  );
}

// Panels: the boxed sections every page is built from

export function Panel({
  title,
  description,
  actions,
  children,
  className,
  bodyClassName,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  const id = useId();
  return (
    <section aria-labelledby={title ? id : undefined} className={cn("rounded-xl border border-line bg-surface shadow-sm", className)}>
      {title && (
        <header className={cn("px-4 py-3", children !== undefined && "border-b border-line")}>
          <div className="flex min-h-8 items-center gap-2">
            <h2 id={id} className="min-w-0 text-sm font-bold text-ink">
              {title}
            </h2>
            {actions && <div className="ml-auto flex shrink-0 items-center gap-1">{actions}</div>}
          </div>
          {description && <p className="mt-1 text-xs text-muted">{description}</p>}
        </header>
      )}
      {children !== undefined && <div className={cn("p-4", bodyClassName)}>{children}</div>}
    </section>
  );
}

/** A numbered step inside a dialog, for flows that really are a sequence. */
export function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="rounded-lg border border-line bg-surface-alt p-4">
      <h3 id={id} className="mb-3 flex items-center gap-2 text-sm font-bold">
        <span className="flex size-6 items-center justify-center rounded-full bg-accent text-xs text-accent-ink" aria-hidden="true">
          {n}
        </span>
        <span>
          <span className="sr-only">Step {n}: </span>
          {title}
        </span>
      </h3>
      {children}
    </section>
  );
}

// States

export function EmptyState({
  title,
  icon: Icon,
  children,
  actions,
}: {
  title: string;
  icon?: LucideIcon;
  children?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="mx-auto max-w-md px-4 py-16 text-center">
      {Icon && (
        <span className="mx-auto mb-4 flex size-12 items-center justify-center rounded-full bg-accent-soft text-accent" aria-hidden="true">
          <Icon className="size-6" />
        </span>
      )}
      <h2 className="text-lg font-bold">{title}</h2>
      {children && <div className="mt-2 text-muted">{children}</div>}
      {actions && <div className="mt-5 flex flex-wrap justify-center gap-2">{actions}</div>}
    </div>
  );
}

/** Grey placeholder lines while data loads; screen readers hear the label. */
export function Loading({ label = "Loading" }: { label?: string }) {
  return (
    <div className="space-y-3 p-6" role="status">
      <span className="sr-only">{label}…</span>
      {["w-11/12", "w-3/4", "w-1/2"].map((w) => (
        <div key={w} className={cn("h-3.5 rounded bg-surface-2 motion-safe:animate-pulse", w)} aria-hidden="true" />
      ))}
    </div>
  );
}

export function ErrorNotice({ error }: { error: unknown }) {
  return (
    <p className="m-6 rounded-md border border-missing bg-missing-soft p-3 text-missing" role="alert">
      {error instanceof Error ? error.message : "Something went wrong."}
    </p>
  );
}

const messageTone = {
  info: { box: "border-line bg-surface-2", icon: "text-accent", Icon: Info },
  success: { box: "border-accent bg-accent-soft", icon: "text-accent", Icon: CircleCheck },
  warning: { box: "border-protected bg-protected-soft", icon: "text-protected", Icon: TriangleAlert },
  error: { box: "border-missing bg-missing-soft", icon: "text-missing", Icon: CircleAlert },
};

/**
 * A message inside a page or dialog: a result, a warning or a hint. The icon
 * and the title carry the tone too, so colour is never the only signal.
 * Pass role="alert" for errors that appear after an action, or "status" for results.
 */
export function InlineMessage({
  tone = "info",
  title,
  children,
  action,
  className,
  role,
}: {
  tone?: keyof typeof messageTone;
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
  className?: string;
  role?: "alert" | "status";
}) {
  const t = messageTone[tone];
  return (
    <div role={role} className={cn("flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm text-ink", t.box, className)}>
      <t.Icon className={cn("mt-0.5 size-4 shrink-0", t.icon)} aria-hidden="true" />
      <div className="min-w-0 flex-1">
        {title && <p className="font-semibold">{title}</p>}
        {children && <div className={cn(title && "mt-0.5", "text-ink/90")}>{children}</div>}
      </div>
      {action && <div className="shrink-0 self-center">{action}</div>}
    </div>
  );
}
