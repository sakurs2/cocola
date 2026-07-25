export type QuestionStatus = "pending" | "answering" | "answered" | "cancelled";

export type QuestionOption = {
  id: string;
  label: string;
};

export type QuestionAnswer = {
  optionId?: string;
  text?: string;
};

export type UiQuestionPart = {
  type: "question";
  questionId: string;
  version: number;
  status: QuestionStatus;
  question: string;
  options: QuestionOption[];
  answer?: QuestionAnswer | null;
};

export type RunSummaryStatus = "success" | "waiting_input" | "cancelled" | "error" | "interrupted";

export type UiRunSummaryPart = {
  type: "run-summary";
  runId: string;
  status: RunSummaryStatus;
  modelLabel: string;
  durationMs: number;
  toolCallCount: number;
  llmCallCount: number;
  errorCode: string;
};

export type StructuredResultRenderer = "summary" | "table" | "list" | "metrics";

export type UiStructuredResultPart = {
  type: "structured-result";
  runId: string;
  renderer: string;
  rendererVersion: number;
  title: string;
  contractHash: string;
  data: unknown;
};

export type UiRichMessagePart = UiQuestionPart | UiRunSummaryPart | UiStructuredResultPart;

const QUESTION_STATUSES = new Set<QuestionStatus>([
  "pending",
  "answering",
  "answered",
  "cancelled",
]);
const RUN_SUMMARY_STATUSES = new Set<RunSummaryStatus>([
  "success",
  "waiting_input",
  "cancelled",
  "error",
  "interrupted",
]);

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

export function normalizeQuestionAnswer(raw: unknown): QuestionAnswer | null {
  if (!raw || typeof raw !== "object") return null;
  const value = raw as Record<string, unknown>;
  const optionId = stringValue(value.optionId || value.option_id).trim();
  const text = stringValue(value.text).trim();
  if (!optionId && !text) return null;
  return {
    ...(optionId ? { optionId } : {}),
    ...(text ? { text } : {}),
  };
}

export function normalizeQuestionPart(raw: unknown): UiQuestionPart | null {
  if (!raw || typeof raw !== "object") return null;
  const part = raw as Record<string, unknown>;
  const questionId = stringValue(part.questionId).trim();
  const question = stringValue(part.question).trim();
  const status = stringValue(part.status) as QuestionStatus;
  const version = Number(part.version);
  if (
    !questionId ||
    !question ||
    question.length > 16 * 1024 ||
    !QUESTION_STATUSES.has(status) ||
    !Number.isInteger(version) ||
    version <= 0
  ) {
    return null;
  }
  const seen = new Set<string>();
  const options = (Array.isArray(part.options) ? part.options : []).flatMap(
    (rawOption): QuestionOption[] => {
      if (!rawOption || typeof rawOption !== "object") return [];
      const option = rawOption as Record<string, unknown>;
      const id = stringValue(option.id).trim();
      const label = stringValue(option.label).trim();
      if (!id || !label || label.length > 1024 || seen.has(id) || seen.size >= 8) return [];
      seen.add(id);
      return [{ id, label }];
    },
  );
  return {
    type: "question",
    questionId,
    version,
    status,
    question,
    options,
    answer: normalizeQuestionAnswer(part.answer),
  };
}

function boundedCount(raw: unknown): number {
  const count = Number(raw);
  return Number.isFinite(count) && count > 0 ? Math.floor(count) : 0;
}

export function normalizeRunSummaryPart(raw: unknown): UiRunSummaryPart | null {
  if (!raw || typeof raw !== "object") return null;
  const part = raw as Record<string, unknown>;
  const runId = stringValue(part.runId).trim();
  const status = stringValue(part.status) as RunSummaryStatus;
  if (!runId || !RUN_SUMMARY_STATUSES.has(status)) return null;
  return {
    type: "run-summary",
    runId,
    status,
    modelLabel: stringValue(part.modelLabel).trim().slice(0, 160),
    durationMs: boundedCount(part.durationMs),
    toolCallCount: boundedCount(part.toolCallCount),
    llmCallCount: boundedCount(part.llmCallCount),
    errorCode: stringValue(part.errorCode).trim().slice(0, 80),
  };
}

export function normalizeStructuredResultPart(raw: unknown): UiStructuredResultPart | null {
  if (!raw || typeof raw !== "object") return null;
  const part = raw as Record<string, unknown>;
  const runId = stringValue(part.runId).trim();
  const renderer = stringValue(part.renderer).trim();
  const rendererVersion = Number(part.rendererVersion);
  if (!runId || !renderer || !Number.isInteger(rendererVersion) || rendererVersion <= 0) {
    return null;
  }
  return {
    type: "structured-result",
    runId,
    renderer,
    rendererVersion,
    title: stringValue(part.title).trim().slice(0, 1024),
    contractHash: stringValue(part.contractHash).trim().slice(0, 160),
    data: part.data,
  };
}

// undefined means the input is not a rich part; null means it declares a rich
// part type but violates that type's wire contract.
export function normalizeRichMessagePart(raw: unknown): UiRichMessagePart | null | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const type = stringValue((raw as Record<string, unknown>).type);
  if (type === "question") return normalizeQuestionPart(raw);
  if (type === "run-summary") return normalizeRunSummaryPart(raw);
  if (type === "structured-result") return normalizeStructuredResultPart(raw);
  return undefined;
}
