"use client";

import { type DataMessagePartProps } from "@assistant-ui/react";
import {
  Activity,
  Check,
  CheckCircle2,
  ChevronDown,
  CircleHelp,
  ClipboardCopy,
  List,
  LoaderCircle,
  Table2,
  XCircle,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FC } from "react";

import {
  type QuestionAnswer,
  type QuestionStatus,
  type RunSummaryStatus,
  type UiQuestionPart,
  type UiRunSummaryPart,
  type UiStructuredResultPart,
  useCocola,
} from "@/app/runtime-provider";
import { formatAgentDuration } from "@/lib/agent-turn-summary.mjs";
import { cn } from "@/lib/utils";

const QUESTION_STATUS_LABELS: Record<QuestionStatus, string> = {
  pending: "Claude needs your input",
  answering: "Continuing",
  answered: "Answered",
  cancelled: "Cancelled",
};

export const QuestionCardPart: FC<DataMessagePartProps<Omit<UiQuestionPart, "type">>> = ({
  data,
}) => {
  const { answerQuestion, cancelQuestion } = useCocola();
  return (
    <QuestionCard
      question={{ ...data, type: "question" }}
      onAnswer={answerQuestion}
      onCancel={cancelQuestion}
    />
  );
};

export function QuestionCard({
  question,
  readonly = false,
  onAnswer,
  onCancel,
}: {
  question: UiQuestionPart;
  readonly?: boolean;
  onAnswer?: (question: UiQuestionPart, answer: QuestionAnswer) => Promise<void>;
  onCancel?: (question: UiQuestionPart) => Promise<void>;
}) {
  const [selectedOptionId, setSelectedOptionId] = useState(question.answer?.optionId ?? "");
  const [customAnswer, setCustomAnswer] = useState(question.answer?.text ?? "");
  const [action, setAction] = useState<"answer" | "cancel" | null>(null);
  const [error, setError] = useState("");
  const titleId = useRef(`question-${question.questionId}`).current;
  const interactive = !readonly && question.status === "pending";
  const busy = action != null || question.status === "answering";

  useEffect(() => {
    setSelectedOptionId(question.answer?.optionId ?? "");
    setCustomAnswer(question.answer?.text ?? "");
  }, [question.answer?.optionId, question.answer?.text]);

  const submit = async () => {
    if (!onAnswer || (!selectedOptionId && !customAnswer.trim())) return;
    setAction("answer");
    setError("");
    try {
      await onAnswer(question, {
        ...(selectedOptionId ? { optionId: selectedOptionId } : {}),
        ...(customAnswer.trim() ? { text: customAnswer.trim() } : {}),
      });
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : "Could not continue the conversation. Try again.",
      );
    } finally {
      setAction(null);
    }
  };

  const cancel = async () => {
    if (!onCancel) return;
    setAction("cancel");
    setError("");
    try {
      await onCancel(question);
    } catch (requestError) {
      setError(
        requestError instanceof Error ? requestError.message : "Could not cancel the question.",
      );
    } finally {
      setAction(null);
    }
  };

  const answeredLabel = useMemo(() => {
    const label = question.options.find((option) => option.id === question.answer?.optionId)?.label;
    return [label, question.answer?.text].filter(Boolean).join(" · ");
  }, [question.answer?.optionId, question.answer?.text, question.options]);

  return (
    <section
      aria-labelledby={titleId}
      className="my-4 overflow-hidden rounded-2xl border border-sky-500/25 bg-card shadow-[0_18px_45px_-36px_rgba(2,132,199,0.7)]"
    >
      <div className="flex items-start gap-3 border-b border-sky-500/15 bg-sky-500/[0.045] px-5 py-4">
        <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-sky-600 text-white shadow-sm">
          {question.status === "answering" ? (
            <LoaderCircle className="size-[18px] animate-spin motion-reduce:animate-none" />
          ) : question.status === "cancelled" ? (
            <XCircle className="size-[18px]" />
          ) : question.status === "answered" ? (
            <CheckCircle2 className="size-[18px]" />
          ) : (
            <CircleHelp className="size-[18px]" />
          )}
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-[10px] font-bold tracking-[0.16em] text-sky-700 uppercase">
            Question
          </div>
          <h3 id={titleId} className="mt-0.5 text-base font-semibold text-foreground">
            {QUESTION_STATUS_LABELS[question.status]}
          </h3>
          {question.status === "pending" ? (
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              Choose an option or enter your own answer.
            </p>
          ) : null}
        </div>
      </div>

      <div className="space-y-4 px-5 py-5">
        <p className="text-[15px] font-medium leading-6 text-foreground">{question.question}</p>
        {interactive && question.options.length > 0 ? (
          <div className="grid gap-2" role="radiogroup" aria-label={question.question}>
            {question.options.map((option) => {
              const selected = selectedOptionId === option.id;
              return (
                <button
                  key={option.id}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  disabled={busy}
                  onClick={() => setSelectedOptionId(option.id)}
                  className={cn(
                    "flex min-h-11 items-center gap-3 rounded-xl border px-3.5 py-2.5 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-500",
                    selected
                      ? "border-sky-500 bg-sky-500/[0.07] text-foreground"
                      : "border-border bg-background hover:bg-muted/50",
                  )}
                >
                  <span
                    className={cn(
                      "grid size-4 shrink-0 place-items-center rounded-full border",
                      selected ? "border-sky-600 bg-sky-600 text-white" : "border-border",
                    )}
                  >
                    {selected ? <Check className="size-2.5" /> : null}
                  </span>
                  <span>{option.label}</span>
                </button>
              );
            })}
          </div>
        ) : null}
        {interactive ? (
          <textarea
            value={customAnswer}
            disabled={busy}
            maxLength={16 * 1024}
            rows={3}
            onChange={(event) => setCustomAnswer(event.target.value)}
            placeholder="Enter your own answer…"
            className="w-full resize-y rounded-xl border border-border bg-background px-3.5 py-3 text-sm leading-5 outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-sky-500 disabled:opacity-60"
          />
        ) : answeredLabel ? (
          <div className="rounded-xl border border-border bg-muted/35 px-3.5 py-3 text-sm text-foreground">
            {answeredLabel}
          </div>
        ) : null}
        {question.status === "cancelled" ? (
          <p className="text-sm text-muted-foreground">This question is no longer current.</p>
        ) : null}
        {error ? (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        ) : null}
      </div>

      {interactive ? (
        <div className="flex flex-col-reverse gap-2 border-t border-border/70 bg-muted/20 px-5 py-4 sm:flex-row sm:justify-end">
          <button
            type="button"
            disabled={busy}
            onClick={() => void cancel()}
            className="inline-flex h-10 items-center justify-center rounded-xl border border-border bg-background px-4 text-sm font-semibold hover:bg-muted disabled:opacity-50"
          >
            {action === "cancel" ? (
              <LoaderCircle className="mr-2 size-4 animate-spin motion-reduce:animate-none" />
            ) : null}
            Cancel question
          </button>
          <button
            type="button"
            disabled={busy || (!selectedOptionId && !customAnswer.trim())}
            onClick={() => void submit()}
            className="inline-flex h-10 items-center justify-center rounded-xl bg-sky-600 px-4 text-sm font-semibold text-white shadow-sm hover:bg-sky-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {action === "answer" ? (
              <LoaderCircle className="mr-2 size-4 animate-spin motion-reduce:animate-none" />
            ) : null}
            Answer and continue
          </button>
        </div>
      ) : null}
    </section>
  );
}

const RUN_STATUS_LABELS: Record<RunSummaryStatus, string> = {
  success: "Completed",
  waiting_input: "Waiting for input",
  cancelled: "Stopped",
  error: "Failed",
  interrupted: "Interrupted",
};

export const RunSummaryPart: FC<DataMessagePartProps<Omit<UiRunSummaryPart, "type">>> = ({
  data,
}) => <RunSummary summary={{ ...data, type: "run-summary" }} />;

export function RunSummary({ summary }: { summary: UiRunSummaryPart }) {
  const actions = summary.toolCallCount;
  return (
    <details className="group my-2 text-xs text-muted-foreground">
      <summary className="grid cursor-pointer list-none grid-cols-[1.75rem_minmax(0,1fr)] items-center gap-x-2.5 rounded-lg py-1.5 outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <span className="flex items-center justify-center text-muted-foreground">
          <Activity className="size-4 shrink-0" aria-hidden="true" />
        </span>
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="font-medium text-foreground">{RUN_STATUS_LABELS[summary.status]}</span>
          {summary.modelLabel ? <span>· {summary.modelLabel}</span> : null}
          {summary.durationMs > 0 ? <span>· {formatAgentDuration(summary.durationMs)}</span> : null}
          <span>
            · {actions} {actions === 1 ? "action" : "actions"}
          </span>
          <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
        </span>
      </summary>
      <div className="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-x-2.5">
        <div className="col-start-2 flex flex-wrap gap-x-4 gap-y-1 pb-1.5">
          <span>{summary.llmCallCount} LLM calls</span>
          <span>{summary.toolCallCount} tool calls</span>
          {summary.errorCode ? <span>Error code: {summary.errorCode}</span> : null}
        </div>
      </div>
    </details>
  );
}

export const StructuredResultCardPart: FC<
  DataMessagePartProps<Omit<UiStructuredResultPart, "type">>
> = ({ data }) => <StructuredResultCard result={{ ...data, type: "structured-result" }} />;

function displayValue(value: unknown): string {
  if (value == null) return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return "Unsupported value";
  }
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function GenericJSON({ data }: { data: unknown }) {
  return (
    <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-muted/60 p-3 text-xs leading-5">
      {JSON.stringify(data, null, 2)}
    </pre>
  );
}

export function StructuredResultCard({ result }: { result: UiStructuredResultPart }) {
  const [copied, setCopied] = useState(false);
  const supported =
    result.rendererVersion === 1 &&
    ["summary", "table", "list", "metrics"].includes(result.renderer);
  const root = objectValue(result.data);
  const title = result.title || (typeof root?.title === "string" ? root.title : "Result");

  const copy = async () => {
    await navigator.clipboard.writeText(JSON.stringify(result.data, null, 2));
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <section className="my-4 overflow-hidden rounded-2xl border border-border bg-card">
      <div className="flex items-center gap-3 border-b border-border/70 bg-muted/25 px-5 py-3.5">
        <span className="grid size-8 place-items-center rounded-lg bg-foreground text-background">
          {result.renderer === "table" ? (
            <Table2 className="size-4" />
          ) : (
            <List className="size-4" />
          )}
        </span>
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">{title}</h3>
        <button
          type="button"
          onClick={() => void copy()}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          {copied ? <Check className="size-3.5" /> : <ClipboardCopy className="size-3.5" />}
          {copied ? "Copied" : "Copy JSON"}
        </button>
      </div>
      <div className="p-5">
        {!supported ? (
          <div className="space-y-3">
            <p className="text-sm font-medium text-muted-foreground">Unsupported result format</p>
            <GenericJSON data={result.data} />
          </div>
        ) : result.renderer === "table" ? (
          <TableResult data={result.data} />
        ) : result.renderer === "list" ? (
          <ListResult data={result.data} />
        ) : result.renderer === "metrics" ? (
          <MetricsResult data={result.data} />
        ) : (
          <SummaryResult data={result.data} />
        )}
      </div>
    </section>
  );
}

function SummaryResult({ data }: { data: unknown }) {
  const root = objectValue(data);
  if (!root) return <GenericJSON data={data} />;
  const entries = Object.entries(root).filter(([key]) => key !== "title");
  return (
    <dl className="grid gap-3 sm:grid-cols-2">
      {entries.map(([key, value]) => (
        <div key={key} className="rounded-xl border border-border bg-background px-3.5 py-3">
          <dt className="text-xs font-medium text-muted-foreground">{key}</dt>
          <dd className="mt-1 break-words text-sm text-foreground">{displayValue(value)}</dd>
        </div>
      ))}
    </dl>
  );
}

function TableResult({ data }: { data: unknown }) {
  const root = objectValue(data);
  const rawColumns = Array.isArray(root?.columns) ? root.columns : [];
  const rows = Array.isArray(root?.rows) ? root.rows : [];
  const columns = rawColumns.map((column, index) => {
    if (typeof column === "string") {
      return { key: `${column}:${index}`, dataKey: column, label: column };
    }
    const value = objectValue(column);
    const dataKey = String(value?.key ?? value?.id ?? index);
    return {
      key: `${dataKey}:${index}`,
      dataKey,
      label: String(value?.label ?? value?.title ?? dataKey),
    };
  });
  if (columns.length === 0) return <GenericJSON data={data} />;
  return (
    <div className="overflow-x-auto rounded-xl border border-border">
      <table className="w-full min-w-max border-collapse text-left text-sm">
        <thead className="bg-muted/50 text-xs text-muted-foreground">
          <tr>
            {columns.map((column) => (
              <th key={column.key} className="border-b border-border px-3 py-2 font-medium">
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, rowIndex) => {
            const record = objectValue(row);
            const values = Array.isArray(row) ? row : null;
            return (
              <tr key={rowIndex} className="border-b border-border/60 last:border-0">
                {columns.map((column, columnIndex) => (
                  <td key={column.key} className="max-w-md px-3 py-2 align-top">
                    {displayValue(values ? values[columnIndex] : record?.[column.dataKey])}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function ListResult({ data }: { data: unknown }) {
  const root = objectValue(data);
  const items = Array.isArray(root?.items) ? root.items : Array.isArray(data) ? data : [];
  if (items.length === 0) return <GenericJSON data={data} />;
  return (
    <ul className="divide-y divide-border rounded-xl border border-border">
      {items.map((item, index) => (
        <li key={index} className="px-3.5 py-3 text-sm">
          {displayValue(item)}
        </li>
      ))}
    </ul>
  );
}

function MetricsResult({ data }: { data: unknown }) {
  const root = objectValue(data);
  const metricItems = Array.isArray(root?.metrics) ? root.metrics : null;
  if (metricItems) {
    return (
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {metricItems.map((item, index) => {
          const metric = objectValue(item);
          const label = displayValue(metric?.label ?? metric?.name ?? `Metric ${index + 1}`);
          const value = displayValue(metric?.value ?? metric?.amount ?? item);
          const unit = typeof metric?.unit === "string" ? metric.unit : "";
          return (
            <div key={index} className="rounded-xl border border-border bg-background px-3.5 py-3">
              <dt className="truncate text-xs font-medium text-muted-foreground">{label}</dt>
              <dd className="mt-1 break-words text-lg font-semibold text-foreground">
                {value}
                {unit ? (
                  <span className="ml-1 text-xs font-medium text-muted-foreground">{unit}</span>
                ) : null}
              </dd>
            </div>
          );
        })}
      </dl>
    );
  }
  const metricsRoot = objectValue(root?.metrics) ?? root;
  if (!metricsRoot) return <GenericJSON data={data} />;
  const entries = Object.entries(metricsRoot).filter(([key]) => key !== "title");
  return (
    <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
      {entries.map(([key, value]) => (
        <div key={key} className="rounded-xl border border-border bg-background px-3.5 py-3">
          <dt className="truncate text-xs font-medium text-muted-foreground">{key}</dt>
          <dd className="mt-1 break-words text-lg font-semibold text-foreground">
            {displayValue(value)}
          </dd>
        </div>
      ))}
    </dl>
  );
}
