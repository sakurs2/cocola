export type ResultField = {
  key: string;
  label: string;
  value: unknown;
};

export type SummaryView = {
  lead: string;
  status: string;
  fields: ResultField[];
};

export type ListItemView = {
  key: string;
  title: string;
  fields: ResultField[];
};

export type MetricView = {
  key: string;
  label: string;
  value: unknown;
  unit: string;
  trend: string;
};

export type TableColumnView = {
  key: string;
  dataKey: string;
  label: string;
};

export type TableView = {
  columns: TableColumnView[];
  rows: unknown[];
};

export function resultRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

export function formatResultValue(value: unknown): string {
  if (value == null) return "—";
  if (typeof value === "string") return value.trim() ? value : "—";
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return "Unsupported value";
  }
}

export function humanizeResultKey(key: string): string {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim();
  if (!words) return "Value";
  return words
    .split(/\s+/)
    .map((word, index) => {
      if (/^(id|url|api|cpu|ram|json|llm|mcp)$/i.test(word)) return word.toUpperCase();
      const normalized = word.toLowerCase();
      return index === 0 ? normalized.charAt(0).toUpperCase() + normalized.slice(1) : normalized;
    })
    .join(" ");
}

function scalarText(value: unknown): string {
  if (typeof value === "string") return value.trim();
  return typeof value === "number" || typeof value === "boolean" ? String(value) : "";
}

function resultFields(entries: Array<[string, unknown]>, keyPrefix: string): ResultField[] {
  return entries.map(([key, value], index) => ({
    key: `${keyPrefix}:${key}:${index}`,
    label: humanizeResultKey(key),
    value,
  }));
}

export function buildSummaryView(data: unknown): SummaryView | null {
  const root = resultRecord(data);
  if (!root) return null;

  const lead = typeof root.summary === "string" ? root.summary.trim() : "";
  const status = scalarText(root.status);
  const rawDetails = Array.isArray(root.details) ? root.details : [];
  const normalizedDetails = rawDetails.flatMap((detail, index): ResultField[] => {
    const value = resultRecord(detail);
    const label = typeof value?.label === "string" ? value.label.trim() : "";
    if (!value || !label || !("value" in value)) return [];
    return [{ key: `detail:${index}`, label, value: value.value }];
  });
  const details =
    rawDetails.length > 0 && normalizedDetails.length === rawDetails.length
      ? normalizedDetails
      : [];
  const excluded = new Set(["title"]);
  if (lead) excluded.add("summary");
  if (details.length > 0) excluded.add("details");
  if (status) excluded.add("status");
  const fields = [
    ...details,
    ...resultFields(
      Object.entries(root).filter(([key]) => !excluded.has(key)),
      "summary",
    ),
  ];
  if (!lead && !status && fields.length === 0) return null;
  return { lead, status, fields };
}

export function buildListItems(data: unknown): ListItemView[] {
  const root = resultRecord(data);
  const items = Array.isArray(root?.items) ? root.items : Array.isArray(data) ? data : [];
  return items.map((item, index) => {
    const value = resultRecord(item);
    if (!value) {
      return {
        key: `item:${index}`,
        title: formatResultValue(item),
        fields: [],
      };
    }

    const titleKey = ["title", "label", "name", "heading"].find(
      (key) => scalarText(value[key]) !== "",
    );
    return {
      key: `item:${index}`,
      title: titleKey ? scalarText(value[titleKey]) : `Item ${index + 1}`,
      fields: resultFields(
        Object.entries(value).filter(([key]) => key !== titleKey),
        `item:${index}`,
      ),
    };
  });
}

export function buildMetrics(data: unknown): MetricView[] {
  const root = resultRecord(data);
  if (!root) return [];

  const metricItems = Array.isArray(root.metrics) ? root.metrics : null;
  if (metricItems) {
    return metricItems.map((item, index) => {
      const metric = resultRecord(item);
      return {
        key: `metric:${index}`,
        label: scalarText(metric?.label) || scalarText(metric?.name) || `Metric ${index + 1}`,
        value: metric && "value" in metric ? metric.value : (metric?.amount ?? item),
        unit: scalarText(metric?.unit),
        trend: scalarText(metric?.trend),
      };
    });
  }

  const metricsRoot = resultRecord(root.metrics) ?? root;
  return Object.entries(metricsRoot)
    .filter(([key]) => key !== "title")
    .map(([key, value], index) => ({
      key: `metric:${key}:${index}`,
      label: humanizeResultKey(key),
      value,
      unit: "",
      trend: "",
    }));
}

export function buildTableView(data: unknown): TableView {
  const root = resultRecord(data);
  const rawColumns = Array.isArray(root?.columns) ? root.columns : [];
  const rows = Array.isArray(root?.rows) ? root.rows : [];
  const columns = rawColumns.map((column, index) => {
    if (typeof column === "string") {
      return { key: `${column}:${index}`, dataKey: column, label: column };
    }
    const value = resultRecord(column);
    const dataKey = String(value?.key ?? value?.id ?? index);
    return {
      key: `${dataKey}:${index}`,
      dataKey,
      label: String(value?.label ?? value?.title ?? dataKey),
    };
  });
  return { columns, rows };
}
