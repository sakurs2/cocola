export type EnvironmentPreparationComponent = {
  kind: string;
  status: string;
  label: string;
  summary?: string;
  count?: number;
  metadata?: Record<string, unknown>;
};

export type EnvironmentPreparationSnapshot = {
  schema_version: number;
  part_id: string;
  state: string;
  components: EnvironmentPreparationComponent[];
};

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

export function parseEnvironmentPreparationSnapshot(
  value: unknown,
): EnvironmentPreparationSnapshot | null {
  let raw = value;
  if (typeof raw === "string") {
    try {
      raw = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  if (!raw || typeof raw !== "object") return null;
  const record = raw as Record<string, unknown>;
  const schemaVersion = Number(record.schema_version);
  const partID = stringValue(record.part_id).trim().slice(0, 120);
  const state = stringValue(record.state).trim().slice(0, 80);
  if (!Number.isInteger(schemaVersion) || schemaVersion < 1 || !partID || !state) return null;

  const rawComponents = Array.isArray(record.components) ? record.components : [];
  const components = rawComponents.slice(0, 64).flatMap((item) => {
    if (!item || typeof item !== "object") return [];
    const component = item as Record<string, unknown>;
    const kind = stringValue(component.kind).trim().slice(0, 80) || "component";
    const status = stringValue(component.status).trim().slice(0, 80) || "unknown";
    const label = stringValue(component.label).trim().slice(0, 160) || kind;
    const summary = stringValue(component.summary).trim().slice(0, 240);
    const count = Number(component.count);
    const metadata =
      component.metadata &&
      typeof component.metadata === "object" &&
      !Array.isArray(component.metadata)
        ? (component.metadata as Record<string, unknown>)
        : undefined;
    return [
      {
        kind,
        status,
        label,
        ...(summary ? { summary } : {}),
        ...(Number.isFinite(count) && count >= 0 ? { count } : {}),
        ...(metadata ? { metadata } : {}),
      },
    ];
  });

  return {
    schema_version: schemaVersion,
    part_id: partID,
    state,
    components,
  };
}
