"use client";

import { type DataMessagePartProps } from "@assistant-ui/react";
import {
  Button,
  Card,
  Chip,
  Label,
  Radio,
  RadioGroup,
  Separator,
  Spinner,
  TextArea,
  TextField,
} from "@heroui/react";
import {
  Activity,
  BarChart3,
  Braces,
  Check,
  CheckCircle2,
  ChevronDown,
  CircleHelp,
  ClipboardCopy,
  FileText,
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
import {
  buildListItems,
  buildMetrics,
  buildSummaryView,
  buildTableView,
  formatResultValue,
  resultRecord,
} from "@/lib/structured-result-view";
import { cn } from "@/lib/utils";

const QUESTION_STATUS_LABELS: Record<QuestionStatus, string> = {
  pending: "Waiting for you",
  answering: "Continuing",
  answered: "Answered",
  cancelled: "Cancelled",
};

const QUESTION_STATUS_VIEW: Record<
  QuestionStatus,
  { color: "accent" | "default" | "success" | "warning"; icon: typeof CircleHelp }
> = {
  pending: { color: "warning", icon: CircleHelp },
  answering: { color: "accent", icon: LoaderCircle },
  answered: { color: "success", icon: CheckCircle2 },
  cancelled: { color: "default", icon: XCircle },
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

  const statusView = QUESTION_STATUS_VIEW[question.status];
  const StatusIcon = statusView.icon;

  return (
    <Card aria-labelledby={titleId} className="my-4 w-full max-w-none overflow-hidden p-0">
      <Card.Header className="flex-row items-start justify-between gap-3 px-4 py-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className="bg-warning-soft text-warning grid size-8 shrink-0 place-items-center rounded-xl">
            <CircleHelp className="size-4" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <Card.Title id={titleId} className="text-base tracking-[-0.02em]">
              {question.status === "pending" ? "Your input is needed" : "Question"}
            </Card.Title>
            <Card.Description className="mt-0.5 text-xs">
              {question.status === "pending"
                ? "Choose an option or write a response."
                : QUESTION_STATUS_LABELS[question.status]}
            </Card.Description>
          </div>
        </div>
        <Chip color={statusView.color} size="sm" variant="soft">
          {question.status === "answering" ? (
            <Spinner color="current" size="sm" />
          ) : (
            <StatusIcon className="size-3.5" aria-hidden="true" />
          )}
          {QUESTION_STATUS_LABELS[question.status]}
        </Chip>
      </Card.Header>

      <Separator />
      <Card.Content className="grid gap-3 px-4 py-3">
        <p className="text-sm font-medium leading-6 text-foreground">{question.question}</p>
        {interactive && question.options.length > 0 ? (
          <RadioGroup
            aria-label={question.question}
            className="grid gap-2"
            isDisabled={busy}
            value={selectedOptionId}
            onChange={setSelectedOptionId}
          >
            {question.options.map((option) => (
              <Radio
                key={option.id}
                className={({ isSelected }) =>
                  cn(
                    "border-separator hover:bg-default min-h-10 rounded-xl border px-3 py-2 transition-[background-color,border-color,transform] duration-150",
                    isSelected && "border-warning bg-warning-soft",
                  )
                }
                value={option.id}
              >
                <Radio.Content className="flex w-full items-center gap-3 text-left">
                  <Radio.Control className="shrink-0">
                    <Radio.Indicator />
                  </Radio.Control>
                  <span className="text-sm leading-5 text-foreground">{option.label}</span>
                </Radio.Content>
              </Radio>
            ))}
          </RadioGroup>
        ) : null}
        {interactive ? (
          <TextField
            className="w-full"
            isDisabled={busy}
            value={customAnswer}
            variant="secondary"
            onChange={setCustomAnswer}
          >
            <Label className="sr-only">Your answer</Label>
            <TextArea
              className="min-h-20 resize-y"
              maxLength={16 * 1024}
              placeholder="Write your own answer…"
              rows={3}
            />
          </TextField>
        ) : answeredLabel ? (
          <div className="bg-default rounded-xl px-3 py-2.5 text-sm text-foreground">
            {answeredLabel}
          </div>
        ) : null}
        {question.status === "cancelled" ? (
          <p className="text-xs text-muted">This question is no longer active.</p>
        ) : null}
        {error ? (
          <p role="alert" className="bg-danger-soft text-danger rounded-xl px-3 py-2 text-xs">
            {error}
          </p>
        ) : null}
      </Card.Content>

      {interactive ? (
        <>
          <Separator />
          <Card.Footer className="flex justify-end gap-2 px-4 py-3">
            <Button
              isDisabled={busy}
              isPending={action === "cancel"}
              size="sm"
              variant="outline"
              onPress={() => void cancel()}
            >
              Cancel question
            </Button>
            <Button
              isDisabled={busy || (!selectedOptionId && !customAnswer.trim())}
              isPending={action === "answer"}
              size="sm"
              onPress={() => void submit()}
            >
              Answer and continue
            </Button>
          </Card.Footer>
        </>
      ) : null}
    </Card>
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
    <details className="group my-2 text-xs text-muted">
      <summary className="grid cursor-pointer list-none grid-cols-[1.75rem_minmax(0,1fr)] items-center gap-x-2.5 rounded-lg py-1.5 outline-none focus-visible:ring-2 focus-visible:ring-focus">
        <span className="flex items-center justify-center text-muted">
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

function GenericJSON({ data }: { data: unknown }) {
  return (
    <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-xl border border-border/60 bg-surface-secondary/35 p-3 font-mono text-xs leading-5 text-foreground">
      {JSON.stringify(data, null, 2)}
    </pre>
  );
}

const RESULT_META = {
  summary: { label: "Summary result", icon: FileText },
  table: { label: "Table result", icon: Table2 },
  list: { label: "List result", icon: List },
  metrics: { label: "Metrics result", icon: BarChart3 },
} as const;

export function StructuredResultCard({ result }: { result: UiStructuredResultPart }) {
  const [copied, setCopied] = useState(false);
  const supported =
    result.rendererVersion === 1 &&
    ["summary", "table", "list", "metrics"].includes(result.renderer);
  const root = resultRecord(result.data);
  const title = result.title || (typeof root?.title === "string" ? root.title : "Result");
  const meta = Object.prototype.hasOwnProperty.call(RESULT_META, result.renderer)
    ? RESULT_META[result.renderer as keyof typeof RESULT_META]
    : { label: "Structured result", icon: Braces };
  const ResultIcon = meta.icon;

  const copy = async () => {
    await navigator.clipboard.writeText(JSON.stringify(result.data, null, 2));
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <section className="my-4 overflow-hidden rounded-2xl border border-border/80 bg-surface shadow-sm">
      <header className="flex items-center gap-3 border-b border-border/70 bg-surface-secondary/20 px-4 py-3.5 sm:px-5">
        <span className="grid size-8 shrink-0 place-items-center rounded-lg border border-accent/15 bg-accent/[0.08] text-accent">
          <ResultIcon className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted">
            {meta.label}
          </p>
          <h3 className="mt-0.5 truncate text-sm font-semibold leading-5 text-foreground">
            {title}
          </h3>
        </div>
        <button
          type="button"
          aria-label={copied ? "JSON copied" : "Copy result JSON"}
          onClick={() => void copy()}
          className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium text-muted transition-colors hover:bg-surface-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/30"
        >
          {copied ? <Check className="size-3.5" /> : <ClipboardCopy className="size-3.5" />}
          <span className="hidden sm:inline">{copied ? "Copied" : "Copy JSON"}</span>
        </button>
      </header>
      <div className={cn(supported && result.renderer === "table" ? "p-0" : "p-4 sm:p-5")}>
        {!supported ? (
          <div className="space-y-3">
            <p className="text-sm font-medium text-muted">Unsupported result format</p>
            <GenericJSON data={result.data} />
          </div>
        ) : result.renderer === "table" ? (
          <TableResult data={result.data} title={title} />
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
  const summary = buildSummaryView(data);
  if (!summary) return <GenericJSON data={data} />;
  return (
    <div>
      {(summary.lead || summary.status) && (
        <div className="flex flex-col items-start justify-between gap-3 sm:flex-row">
          {summary.lead ? (
            <p className="max-w-3xl text-sm leading-6 text-foreground">{summary.lead}</p>
          ) : null}
          {summary.status ? (
            <span className="shrink-0 rounded-full border border-accent/15 bg-accent/[0.07] px-2.5 py-1 text-xs font-medium text-accent sm:ml-auto">
              {summary.status}
            </span>
          ) : null}
        </div>
      )}
      {summary.fields.length > 0 ? (
        <dl
          className={cn(
            "divide-y divide-border/70 border-y border-border/70",
            (summary.lead || summary.status) && "mt-5",
          )}
        >
          {summary.fields.map((field) => (
            <div
              key={field.key}
              className="grid gap-1 py-3 sm:grid-cols-[minmax(8rem,0.35fr)_minmax(0,1fr)] sm:gap-5"
            >
              <dt className="text-xs font-medium text-muted">{field.label}</dt>
              <dd className="break-words text-sm leading-5 text-foreground">
                {formatResultValue(field.value)}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
    </div>
  );
}

function TableResult({ data, title }: { data: unknown; title: string }) {
  const { columns, rows } = buildTableView(data);
  if (columns.length === 0) {
    return (
      <div className="p-4 sm:p-5">
        <GenericJSON data={data} />
      </div>
    );
  }
  return (
    <div
      className="overflow-x-auto focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-focus/30"
      role="region"
      aria-label={`${title} table`}
      tabIndex={0}
    >
      <table className="w-max min-w-full border-collapse text-left text-sm">
        <caption className="sr-only">{title}</caption>
        <thead className="bg-surface-secondary/40 text-xs text-muted">
          <tr>
            {columns.map((column, columnIndex) => (
              <th
                key={column.key}
                className={cn(
                  "border-b border-border px-4 py-3 font-semibold sm:px-5",
                  columnIndex === 0 && "sticky left-0 z-20 border-r bg-surface-secondary/70",
                )}
              >
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={columns.length} className="px-5 py-8 text-center text-sm text-muted">
                No rows
              </td>
            </tr>
          ) : (
            rows.map((row, rowIndex) => {
              const record = resultRecord(row);
              const values = Array.isArray(row) ? row : null;
              return (
                <tr
                  key={rowIndex}
                  className="group border-b border-border/60 transition-colors last:border-0 hover:bg-surface-secondary/20"
                >
                  {columns.map((column, columnIndex) => (
                    <td
                      key={column.key}
                      className={cn(
                        "max-w-md px-4 py-3 align-top leading-5 sm:px-5",
                        columnIndex === 0 &&
                          "sticky left-0 z-10 border-r bg-surface font-medium group-hover:bg-surface-secondary/20",
                      )}
                    >
                      {formatResultValue(values ? values[columnIndex] : record?.[column.dataKey])}
                    </td>
                  ))}
                </tr>
              );
            })
          )}
        </tbody>
      </table>
    </div>
  );
}

function ListResult({ data }: { data: unknown }) {
  const items = buildListItems(data);
  if (items.length === 0) return <GenericJSON data={data} />;
  return (
    <ol className="divide-y divide-border/70 border-y border-border/70">
      {items.map((item, index) => (
        <li key={item.key} className="grid grid-cols-[2rem_minmax(0,1fr)] gap-3 py-3.5 sm:gap-4">
          <span className="pt-0.5 font-mono text-xs tabular-nums text-muted">
            {String(index + 1).padStart(2, "0")}
          </span>
          <div className="min-w-0">
            <p className="break-words text-sm font-medium leading-5 text-foreground">
              {item.title}
            </p>
            {item.fields.length > 0 ? (
              <dl className="mt-2 grid gap-x-6 gap-y-1.5 sm:grid-cols-2">
                {item.fields.map((field) => (
                  <div key={field.key} className="min-w-0 text-xs leading-5">
                    <dt className="inline font-medium text-muted">{field.label}: </dt>
                    <dd className="inline break-words text-foreground">
                      {formatResultValue(field.value)}
                    </dd>
                  </div>
                ))}
              </dl>
            ) : null}
          </div>
        </li>
      ))}
    </ol>
  );
}

function MetricsResult({ data }: { data: unknown }) {
  const metrics = buildMetrics(data);
  if (metrics.length === 0) return <GenericJSON data={data} />;
  return (
    <dl className="grid grid-cols-2 gap-x-5 gap-y-5 sm:grid-cols-3">
      {metrics.map((metric) => (
        <div key={metric.key} className="min-w-0 border-t border-border/80 pt-3">
          <dt className="truncate text-xs font-medium text-muted">{metric.label}</dt>
          <dd className="mt-1.5 break-words text-2xl font-semibold tracking-tight text-foreground">
            {formatResultValue(metric.value)}
            {metric.unit ? (
              <span className="ml-1.5 text-xs font-medium tracking-normal text-muted">
                {metric.unit}
              </span>
            ) : null}
          </dd>
          {metric.trend ? (
            <p className="mt-1 text-xs font-medium text-accent">{metric.trend}</p>
          ) : null}
        </div>
      ))}
    </dl>
  );
}
