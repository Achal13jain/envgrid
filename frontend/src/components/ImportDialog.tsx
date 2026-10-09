import { useId, useEffect, useRef, useState, type DragEvent, type FormEvent } from "react";
import { flushSync } from "react-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  ChevronRight,
  CircleCheck,
  CirclePlus,
  Equal,
  FileText,
  FileUp,
  LoaderCircle,
  Pencil,
  Plus,
  Search,
} from "lucide-react";
import { useNavigate } from "react-router";
import { api, ApiError } from "@/lib/api";
import { errorMessage, useAnnounce } from "@/lib/announce";
import { convertible, formatLabel, formatOptions, inferFormat } from "@/lib/format";
import type { ConfigFile, Detection, Environment, Format, ImportResult, User } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Breakable, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, InlineMessage, Input, Select, Textarea } from "./ui";

const MAX_BYTES = 1024 * 1024;
const FIRST_KEYS = 10;

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
const cap = (s: string) => s[0].toUpperCase() + s.slice(1);
const size = (n: number) => (n < 1024 ? count(n, "byte", "bytes") : `${(n / 1024).toFixed(1)} KB`);

/** What an import does to each key; every key lands in exactly one group. */
function outcomes(r: ImportResult) {
  const created = new Set(r.keysCreated);
  return [
    { one: "new key", many: "new keys", keys: r.keysCreated, Icon: Plus, tone: "text-accent" },
    { one: "value added", many: "values added", keys: r.added.filter((k) => !created.has(k)), Icon: CirclePlus, tone: "text-accent" },
    { one: "value changed", many: "values changed", keys: r.updated, Icon: Pencil, tone: "text-changed" },
    { one: "unchanged", many: "unchanged", keys: r.unchanged, Icon: Equal, tone: "text-muted" },
  ];
}

export function summarise(r: ImportResult): string {
  const parts = outcomes(r)
    .filter((o) => o.keys.length)
    .map((o) => count(o.keys.length, o.one, o.many));
  return parts.length ? `${parts.join(", ")}.` : "Nothing changed.";
}

type Props = {
  environments: Environment[];
  user: User;
  onClose: () => void;
  /** Preselected environment. */
  envId?: number | null;
} & ({ file: ConfigFile; repoId?: undefined } | { file?: undefined; repoId: number });

/** A dry run of the import, kept with the input it answered for. */
type Plan = { body: string; envId: number; result?: ImportResult; error?: string; rule?: boolean };

/**
 * Imports a file into one environment, as a three-step wizard. With `file`,
 * into that file; with `repoId`, it creates a new file whose format is
 * detected from the content.
 */
export function ImportDialog(props: Props) {
  const { environments, user, onClose, file } = props;
  const isNew = !file;
  const fileId = file?.id;
  const announce = useAnnounce();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const inputId = useId();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const nextRef = useRef<HTMLButtonElement>(null);

  const writable = environments.filter((e) => user.role === "admin" || !e.isProtected);
  const [step, setStep] = useState(1);
  const [editing, setEditing] = useState(true);
  const [envId, setEnvId] = useState<number>(props.envId ?? writable[0]?.id ?? 0);
  const [body, setBody] = useState("");
  const [sourceName, setSourceName] = useState("");
  const [name, setName] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [override, setOverride] = useState<Format | "">("");
  const [detection, setDetection] = useState<{ body: string; d: Detection | null } | null>(null);
  const [plan, setPlan] = useState<Plan | null>(null);
  const [readError, setReadError] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<ConfigFile | null>(null);
  const [result, setResult] = useState<ImportResult | null>(null);

  // Ask the server what the content is, a moment after it stops changing.
  useEffect(() => {
    if (!body.trim()) return;
    let cancelled = false;
    const t = setTimeout(async () => {
      try {
        const d = await api.detect(body, sourceName);
        if (!cancelled) setDetection({ body, d });
      } catch {
        if (!cancelled) setDetection({ body, d: null });
      }
    }, 300);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [body, sourceName]);

  // Into an existing file, a dry run says what would change. It stores nothing.
  useEffect(() => {
    if (fileId === undefined || !body.trim() || !envId) return;
    let cancelled = false;
    const t = setTimeout(async () => {
      try {
        const r = await api.importFile(fileId, envId, body, true);
        if (!cancelled) setPlan({ body, envId, result: r });
      } catch (err) {
        if (!cancelled) setPlan({ body, envId, error: errorMessage(err), rule: err instanceof ApiError && err.code === "rule_failed" });
      }
    }, 300);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [fileId, envId, body]);

  const hasContent = body.trim() !== "";
  // Detection and dry run only count for the content they answered for.
  const detected = detection?.body === body ? detection.d : null;
  const detecting = hasContent && detection?.body !== body;
  // CSV is read, never stored: a new file from a CSV upload becomes a .env file.
  const guess = detected?.format === "csv" ? "dotenv" : detected?.format;
  const format: Format = isNew ? override || guess || inferFormat(sourceName) || "raw" : file.format;
  const mismatch = !isNew && detected && detected.format !== file.format && detected.format !== "raw" ? detected.format : null;
  const looksLikeText = !isNew && detected?.format === "raw" && file.format !== "raw";
  const currentPlan = plan && plan.body === body && plan.envId === envId ? plan : null;
  const envName = environments.find((e) => e.id === envId)?.name ?? "";
  const finalLabel = isNew ? "Create file and import" : "Import";

  const steps: [string, string][] = [
    ["Add the file", "Upload a file or paste its contents."],
    ["Check what envgrid found", isNew ? "See the format and the keys, and change the format if it is wrong." : "See the format and the keys in the file."],
    ["Choose where it goes", isNew ? "Name the new file and pick the environment." : "Pick the environment and check what will change."],
  ];

  /** Shows a step and moves focus to its heading. */
  const go = (n: number) => {
    flushSync(() => setStep(n));
    headingRef.current?.focus();
  };

  const readFile = async (f: File) => {
    setReadError(null);
    if (f.size > MAX_BYTES) return setReadError(`${f.name} is larger than 1 MiB, the import limit. Choose a smaller file.`);
    const text = await f.text();
    if (text.includes(String.fromCharCode(0))) return setReadError(`${f.name} looks like a binary file. envgrid imports text files only.`);
    flushSync(() => {
      setBody(text);
      setSourceName(f.name);
      setEditing(false);
      if (!nameTouched) setName(f.name);
    });
    nextRef.current?.focus();
    announce(`Loaded ${f.name}.`);
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDragging(false);
    const f = e.dataTransfer.files[0];
    if (f) void readFile(f);
  };

  /** Back to step 1. An uploaded file is cleared; pasted text stays to be edited. */
  const changeFile = () => {
    if (sourceName) {
      setBody("");
      setSourceName("");
    }
    setEditing(true);
    go(1);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (step !== 3) return;
    setError(null);
    setBusy(true);
    try {
      const target = file ?? created ?? (await api.createFile(props.repoId!, { name: name.trim(), format }));
      if (isNew) setCreated(target);
      const r = await api.importFile(target.id, envId, body);
      setResult(r);
      announce(`Imported into ${target.name}: ${summarise(r)}`);
      await qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "me" });
      if (isNew) navigate(`/repos/${target.repoId}/files/${target.id}`);
    } catch (err) {
      setError(created ? `The file was created, but the import failed: ${errorMessage(err)}` : errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[min(46rem,calc(100dvh-2rem))] max-w-5xl flex-col overflow-hidden p-0">
        <div className="shrink-0 border-b border-line px-5 py-4">
          <DialogTitle>{isNew ? "Import a file" : `Import into ${file.name}`}</DialogTitle>
          <DialogDescription>
            {isNew
              ? "Upload or paste any text file. envgrid works out the format and the keys; anything without keys is kept whole as plain text."
              : "New keys are added, changed values get a new version, and keys missing from the file are left alone."}
          </DialogDescription>
        </div>

        {result ? (
          <ImportSummary result={result} isNew={isNew} target={file?.name ?? created?.name ?? ""} env={envName} onDone={onClose} />
        ) : (
          <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-0 flex-1 flex-col md:flex-row">
              <nav aria-label="Import steps" className="hidden shrink-0 overflow-y-auto border-r border-line bg-surface-alt p-3 md:block md:w-52">
                <ol className="space-y-1">
                  {steps.map(([title, hint], i) => {
                    const n = i + 1;
                    const done = n < step;
                    return (
                      <li key={n} aria-current={n === step ? "step" : undefined} className={cn("flex gap-3 rounded-lg p-2.5", n === step && "bg-accent-soft")}>
                        <span
                          aria-hidden="true"
                          className={cn(
                            "flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-bold",
                            n === step ? "border-accent bg-accent text-accent-ink" : done ? "border-accent bg-surface text-accent" : "border-line bg-surface text-muted",
                          )}
                        >
                          {done ? <Check className="size-4" /> : n}
                        </span>
                        <span className="min-w-0">
                          <span className="block text-sm font-semibold">
                            {title}
                            {done && <span className="sr-only">, done</span>}
                          </span>
                          <span className="mt-0.5 block text-xs text-muted">{hint}</span>
                        </span>
                      </li>
                    );
                  })}
                </ol>
              </nav>

              {/* The heading says "Step n of 3" to screen readers, so this stays visual. */}
              <div aria-hidden="true" className="shrink-0 border-b border-line px-4 py-2.5 md:hidden">
                <p className="text-xs font-semibold text-muted">Step {step} of 3</p>
                <div className="mt-1.5 flex gap-1">
                  {[1, 2, 3].map((n) => (
                    <span key={n} className={cn("h-1 flex-1 rounded-full", n <= step ? "bg-accent" : "bg-line")} />
                  ))}
                </div>
              </div>

              <div className="min-h-0 flex-1 overflow-y-auto lg:flex">
                <div className="min-w-0 flex-1 space-y-4 p-4 sm:p-5">
                  {step > 1 && <SourceRow name={sourceName} body={body} onChange={changeFile} />}
                  <div>
                    <h3 ref={headingRef} tabIndex={-1} className="flex items-center gap-3 text-base font-bold outline-none">
                      <span aria-hidden="true" className="flex size-7 shrink-0 items-center justify-center rounded-full bg-accent text-sm text-accent-ink">
                        {step}
                      </span>
                      <span>
                        <span className="sr-only">Step {step} of 3: </span>
                        {steps[step - 1][0]}
                      </span>
                    </h3>
                    <p className="mt-1 text-sm text-muted sm:pl-10">{steps[step - 1][1]}</p>
                  </div>

                  {step === 1 &&
                    (!editing && hasContent ? (
                      <SourceRow name={sourceName} body={body} onChange={changeFile} />
                    ) : (
                      <>
                        <div
                          onDragOver={(e) => {
                            e.preventDefault();
                            setDragging(true);
                          }}
                          onDragLeave={() => setDragging(false)}
                          onDrop={onDrop}
                          className={cn(
                            "flex flex-col items-center gap-2 rounded-lg border-2 border-dashed px-4 py-6 text-center",
                            dragging ? "border-accent bg-accent-soft" : "border-line bg-surface-alt",
                          )}
                        >
                          <FileUp className="size-6 text-muted" aria-hidden="true" />
                          <p className="text-sm">Drop a file here, or</p>
                          <input
                            id={inputId}
                            type="file"
                            className="peer sr-only"
                            onChange={(e) => {
                              const f = e.target.files?.[0];
                              if (f) void readFile(f);
                              e.target.value = "";
                            }}
                          />
                          <label
                            htmlFor={inputId}
                            className="inline-flex h-9 cursor-pointer items-center gap-1.5 rounded-md border border-line bg-surface px-3.5 text-sm font-semibold hover:bg-surface-2 peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-(--focus)"
                          >
                            <FileText className="size-4" aria-hidden="true" /> Choose a file
                          </label>
                          <p className="text-xs text-muted">Text files up to 1 MiB.</p>
                        </div>
                        {readError && (
                          <InlineMessage tone="error" role="alert" title="envgrid cannot use this file">
                            {readError}
                          </InlineMessage>
                        )}
                        <Field label="Or paste the contents" htmlFor="import-body">
                          <Textarea
                            id="import-body"
                            rows={8}
                            spellCheck={false}
                            autoComplete="off"
                            value={body}
                            onChange={(e) => setBody(e.target.value)}
                            placeholder={"DATABASE_URL=postgres://...\nor JSON, YAML, INI, .properties, PHP constants, or any text"}
                          />
                        </Field>
                      </>
                    ))}

                  {step === 2 && (
                    <>
                      <div className="grid gap-4 rounded-lg border border-line bg-surface-alt p-4 sm:grid-cols-2">
                        <div>
                          <p className="text-sm font-semibold">Detected format</p>
                          <p className="mt-2 flex items-center gap-2 text-sm">
                            {detected ? (
                              <>
                                <FileText className="size-4 shrink-0 text-muted" aria-hidden="true" />
                                <span className="font-semibold">
                                  {detected.label}
                                  {detected.format !== "raw" && <>, {count(detected.keys, "key", "keys")}</>}
                                </span>
                              </>
                            ) : detecting ? (
                              <span role="status" className="flex items-center gap-2 text-muted">
                                <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" /> Reading the file…
                              </span>
                            ) : (
                              <span className="text-muted">Not known</span>
                            )}
                          </p>
                        </div>
                        {isNew ? (
                          <Field label="Read it as" htmlFor="import-format" hint="Change this only if the guess is wrong.">
                            <Select
                              id="import-format"
                              value={override}
                              onValueChange={(v) => setOverride(v as Format | "")}
                              options={[
                                { value: "", label: `Detected: ${detected ? formatLabel[detected.format] : "…"}` },
                                ...formatOptions.map(([f, label]) => ({ value: f, label })),
                              ]}
                            />
                          </Field>
                        ) : (
                          <div>
                            <p className="text-sm font-semibold">
                              Format of <span className="font-mono">{file.name}</span>
                            </p>
                            <p className="mt-2 text-sm">{formatLabel[file.format]}</p>
                          </div>
                        )}
                      </div>

                      {!detected && !detecting && (
                        <InlineMessage title="envgrid could not check the format">
                          {isNew
                            ? "Choose the format in Read it as, then go on."
                            : "You can still go on. If the content cannot be read, the next step says so before anything is stored."}
                        </InlineMessage>
                      )}
                      {detected?.error && (
                        <InlineMessage tone="warning" title="The file has a problem">
                          It looks like {formatLabel[detected.format]}, but it could not be read: {detected.error}.
                        </InlineMessage>
                      )}
                      {detected && !detected.error && detected.format === "raw" && !looksLikeText && (
                        <InlineMessage title="Plain text: no keys found">
                          The whole text is stored as one value per environment, with history and compare like any other value.
                        </InlineMessage>
                      )}
                      {looksLikeText && (
                        <InlineMessage tone="warning" title="No keys were found">
                          This looks like plain text. To keep a text file whole, import it from the repo as a new file instead.
                        </InlineMessage>
                      )}
                      {mismatch && convertible(mismatch, file!.format) && (
                        <InlineMessage title="The file will be converted">
                          This is {formatLabel[mismatch]}. It will be converted into {file!.name} ({formatLabel[file!.format]}).
                        </InlineMessage>
                      )}
                      {mismatch && !convertible(mismatch, file!.format) && (
                        <InlineMessage tone="warning" title="The format does not match">
                          This looks like {formatLabel[mismatch]}, but {file!.name} is a {formatLabel[file!.format]} file, so the import will probably fail. Use
                          Import a file on the repo instead: it creates a {formatLabel[mismatch]} file.
                        </InlineMessage>
                      )}
                      {detected && !detected.error && detected.format !== "raw" && !mismatch && (!override || override === guess) && (
                        <InlineMessage tone="success" title="The format looks right">
                          envgrid read it as {detected.label} and found {count(detected.keys, "key", "keys")}.
                        </InlineMessage>
                      )}
                      {detected && detected.sample.length > 0 && <KeysFound detected={detected} />}
                    </>
                  )}

                  {step === 3 && (
                    <>
                      <div className="grid gap-4 sm:grid-cols-2">
                        {isNew && (
                          <Field label="File name in envgrid" htmlFor="import-name" hint={`Stored as ${formatLabel[format]}.`}>
                            <Input
                              id="import-name"
                              required
                              disabled={!!created}
                              className="font-mono"
                              value={name}
                              placeholder="constants.php"
                              onChange={(e) => {
                                setName(e.target.value);
                                setNameTouched(true);
                              }}
                            />
                          </Field>
                        )}
                        <Field
                          label="Environment"
                          htmlFor="import-env"
                          hint={writable.length < environments.length ? "Protected environments are not listed: only admins can change them." : undefined}
                        >
                          <Select
                            id="import-env"
                            value={envId}
                            onValueChange={(v) => setEnvId(Number(v))}
                            itemClassName="font-mono"
                            options={writable.map((e) => ({ value: e.id, label: e.name }))}
                          />
                        </Field>
                      </div>
                      {writable.length === 0 && (
                        <InlineMessage tone="warning" title="There is no environment you can import into">
                          Every environment in this repo is protected. Ask an admin to import the file.
                        </InlineMessage>
                      )}
                      {error && (
                        <InlineMessage tone="error" role="alert" title="The import did not finish">
                          {error}
                        </InlineMessage>
                      )}
                    </>
                  )}
                </div>

                <div className={cn("space-y-4 border-t border-line p-4 sm:p-5 lg:w-72 lg:shrink-0 lg:border-t-0 lg:border-l", step !== 3 && "hidden lg:block")}>
                  <section aria-labelledby="import-plan" aria-live={step === 3 ? "polite" : undefined} className="rounded-lg border border-line bg-surface p-4">
                    <h3 id="import-plan" className="text-sm font-bold">
                      What will happen
                    </h3>
                    {envName && hasContent && (
                      <p className="mt-0.5 text-xs text-muted">
                        In <span className="font-mono">{envName}</span>
                      </p>
                    )}
                    <div className="mt-3 text-sm">
                      {!hasContent ? (
                        <p className="text-muted">Add a file to see what the import will do.</p>
                      ) : isNew ? (
                        <NewFilePlan name={name.trim()} format={format} keys={override ? undefined : detected?.keys} />
                      ) : !envId ? (
                        <p className="text-muted">Pick an environment to see what will change.</p>
                      ) : !currentPlan ? (
                        <p className="flex items-center gap-2 text-muted">
                          <LoaderCircle className="size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" /> Working out the changes…
                        </p>
                      ) : currentPlan.result ? (
                        <PlanList result={currentPlan.result} />
                      ) : (
                        <InlineMessage tone="error" title={currentPlan.rule ? "A value breaks a key's rule" : "envgrid cannot import this content"}>
                          {currentPlan.error}
                          {currentPlan.rule && " Fix the value in the file, then add the file again."}
                        </InlineMessage>
                      )}
                    </div>
                  </section>
                  {!created && (
                    <InlineMessage title="Nothing is stored yet">
                      envgrid reads the content on its server to find the keys and work out the changes. Nothing is saved until you press {finalLabel}.
                    </InlineMessage>
                  )}
                </div>
              </div>
            </div>

            <DialogFooter className="mt-0 shrink-0 justify-between border-t border-line px-5 py-3">
              {step === 1 ? (
                <Button key="cancel" onClick={onClose}>
                  Cancel
                </Button>
              ) : (
                <Button key="back" onClick={() => go(step - 1)}>
                  <ArrowLeft /> Back
                </Button>
              )}
              {/* Distinct keys, so Next is never reused as the submit button mid-click. */}
              {step < 3 ? (
                <Button
                  key="next"
                  ref={nextRef}
                  variant="primary"
                  disabled={!hasContent}
                  onClick={() => {
                    setEditing(false);
                    go(step + 1);
                  }}
                >
                  Next <ArrowRight />
                </Button>
              ) : (
                <Button key="submit" type="submit" variant="primary" disabled={busy || !hasContent || !envId || !!currentPlan?.error}>
                  {busy ? "Importing…" : finalLabel}
                </Button>
              )}
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** The loaded content in one line: name, size and where it came from. */
function SourceRow({ name, body, onChange }: { name: string; body: string; onChange: () => void }) {
  const lines = (body.match(/\n/g)?.length ?? 0) + (body.endsWith("\n") ? 0 : 1);
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border border-line bg-surface-alt p-3">
      <span aria-hidden="true" className="flex size-10 shrink-0 items-center justify-center rounded-md bg-accent-soft text-accent">
        <FileText className="size-5" />
      </span>
      <div className="min-w-0 flex-1">
        <p className={cn("truncate font-semibold", name && "font-mono")}>{name || "Pasted text"}</p>
        <p className="text-xs text-muted">
          {count(lines, "line", "lines")} · {size(new Blob([body]).size)} · {name ? "From your computer" : "Pasted"}
        </p>
      </div>
      <Button size="sm" onClick={onChange}>
        <Pencil /> Change file
      </Button>
    </div>
  );
}

/** The key names from detection, filterable, first ten until asked for all. */
function KeysFound({ detected }: { detected: Detection }) {
  const id = useId();
  const [filter, setFilter] = useState("");
  const [all, setAll] = useState(false);
  const q = filter.trim().toLowerCase();
  const matches = detected.sample.map((k, i) => ({ k, n: i + 1 })).filter(({ k }) => k.toLowerCase().includes(q));
  const shown = all ? matches : matches.slice(0, FIRST_KEYS);
  return (
    <section aria-labelledby={id} className="rounded-lg border border-line">
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-3 py-2">
        <h4 id={id} className="text-sm font-bold">
          Keys found{" "}
          <span className="font-normal text-muted">({shown.length < detected.keys ? `${shown.length} of ${detected.keys} shown` : detected.keys})</span>
        </h4>
        <div className="relative w-full sm:ml-auto sm:w-52">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted" aria-hidden="true" />
          <Input type="search" aria-label="Filter keys" placeholder="Filter keys" className="pl-8" value={filter} onChange={(e) => setFilter(e.target.value)} />
        </div>
      </div>
      {shown.length > 0 ? (
        <table className="w-full text-left text-sm">
          <thead className="bg-surface-2">
            <tr>
              <th scope="col" className="w-14 px-3 py-1.5">
                No.
              </th>
              <th scope="col" className="px-3 py-1.5">
                Key
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {shown.map(({ k, n }) => (
              <tr key={n}>
                <td className="px-3 py-1.5 text-muted tabular-nums">{n}</td>
                <td className="px-3 py-1.5 font-mono">
                  <Breakable text={k} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="px-3 py-3 text-sm text-muted">No keys match "{filter.trim()}".</p>
      )}
      {(matches.length > FIRST_KEYS || detected.keys > detected.sample.length) && (
        <div className="flex flex-wrap items-center gap-2 border-t border-line px-3 py-2">
          {matches.length > FIRST_KEYS && (
            <Button size="sm" variant="ghost" onClick={() => setAll(!all)}>
              {all ? `Show the first ${FIRST_KEYS} keys` : `Show all ${matches.length} ${q ? "matching keys" : "keys"}`}
            </Button>
          )}
          {detected.keys > detected.sample.length && (
            <p className="text-xs text-muted">
              Only the first {detected.sample.length} names are listed here. All {detected.keys} keys are imported.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

/** A new file has no keys yet, so everything in it is new. */
function NewFilePlan({ name, format, keys }: { name: string; format: Format; keys?: number }) {
  return (
    <div className="space-y-2">
      <p>
        envgrid creates {name ? <span className="font-mono">{name}</span> : "the new file"} in this repo, stored as {formatLabel[format]}.
      </p>
      {format === "raw" ? (
        <p>The whole text becomes one value.</p>
      ) : (
        <>
          {keys !== undefined && (
            <p className="flex items-center gap-2">
              <Plus className="size-4 text-accent" aria-hidden="true" />
              <span className="font-semibold">{count(keys, "new key", "new keys")}</span>
            </p>
          )}
          <p className="text-muted">Every key will be new, because the file does not exist yet.</p>
        </>
      )}
    </div>
  );
}

/** Dry-run counts, with a list of each key and its outcome on request. */
function PlanList({ result }: { result: ImportResult }) {
  const listId = useId();
  const [open, setOpen] = useState(false);
  const groups = outcomes(result);
  return (
    <div className="space-y-3">
      <ul className="space-y-1.5">
        {groups.map((g) => (
          <li key={g.many} className="flex items-center gap-2">
            <g.Icon className={cn("size-4 shrink-0", g.tone)} aria-hidden="true" />
            <span>
              <span className="font-semibold tabular-nums">{g.keys.length}</span> {g.keys.length === 1 ? g.one : g.many}
            </span>
          </li>
        ))}
      </ul>
      <Button size="sm" variant="ghost" className="-ml-2" aria-expanded={open} aria-controls={listId} onClick={() => setOpen(!open)}>
        <ChevronRight className={cn("transition-transform motion-reduce:transition-none", open && "rotate-90")} aria-hidden="true" /> List each key
      </Button>
      {open && (
        <ul id={listId} aria-label="Each key and what will happen to it" className="divide-y divide-line rounded-md border border-line">
          {groups.flatMap((g) =>
            g.keys.map((k) => (
              <li key={k} className="flex items-start gap-2 px-2.5 py-1.5">
                <span className="min-w-0 flex-1 font-mono text-xs [overflow-wrap:anywhere]">
                  <Breakable text={k} />
                </span>
                <span className="flex shrink-0 items-center gap-1 text-xs text-muted">
                  <g.Icon className={cn("size-3.5", g.tone)} aria-hidden="true" />
                  {cap(g.one)}
                </span>
              </li>
            )),
          )}
        </ul>
      )}
    </div>
  );
}

function ImportSummary({ result, isNew, target, env, onDone }: { result: ImportResult; isNew: boolean; target: string; env: string; onDone: () => void }) {
  return (
    <>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5" role="status">
        <p className="flex items-center gap-2 font-semibold">
          <CircleCheck className="size-5 shrink-0 text-accent" aria-hidden="true" />
          <span>
            Imported into <span className="font-mono">{target}</span> in <span className="font-mono">{env}</span>.
          </span>
        </p>
        {result.readAs && <p className="text-sm text-muted">The content was read as {formatLabel[result.readAs]} and converted.</p>}
        <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {outcomes(result).map((g) => (
            <div key={g.many} className="rounded-lg border border-line bg-surface-alt p-3">
              <dt className="flex items-center gap-1.5 text-xs text-muted">
                <g.Icon className={cn("size-3.5", g.tone)} aria-hidden="true" />
                {cap(g.many)}
              </dt>
              <dd className="text-2xl font-bold tabular-nums">{g.keys.length}</dd>
            </div>
          ))}
        </dl>
        <p className="text-sm text-muted">
          Every value set here is saved as a new version, so it shows in the key's history.
          {!isNew && " Keys that were not in the file were left alone."}
        </p>
      </div>
      <DialogFooter className="mt-0 shrink-0 border-t border-line px-5 py-3">
        <Button variant="primary" autoFocus onClick={onDone}>
          Done
        </Button>
      </DialogFooter>
    </>
  );
}
