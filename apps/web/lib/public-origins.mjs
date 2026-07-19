const HTTP_PROTOCOLS = new Set(["http:", "https:"]);

function normalizeConfiguredOrigin(raw) {
  const value = String(raw ?? "").trim();
  if (!value || value === "*") {
    throw new Error(
      "origins must be explicit http(s) URLs; wildcard and empty values are not allowed",
    );
  }

  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`invalid public origin: ${value}`);
  }

  if (
    !HTTP_PROTOCOLS.has(parsed.protocol) ||
    !parsed.hostname ||
    parsed.username ||
    parsed.password ||
    (parsed.pathname !== "" && parsed.pathname !== "/") ||
    parsed.search ||
    parsed.hash ||
    parsed.hostname.includes("*")
  ) {
    throw new Error(`invalid public origin: ${value}`);
  }
  return parsed.origin;
}

export function parsePublicOrigins(raw) {
  const origins = new Set();
  for (const item of String(raw ?? "").split(",")) {
    if (!item.trim()) continue;
    origins.add(normalizeConfiguredOrigin(item));
  }
  return origins;
}

export function isAllowedWebSocketOrigin(rawOrigin, allowedOrigins) {
  if (!rawOrigin || !(allowedOrigins instanceof Set) || allowedOrigins.size === 0) {
    return false;
  }
  try {
    const parsed = new URL(rawOrigin);
    if (
      !HTTP_PROTOCOLS.has(parsed.protocol) ||
      parsed.username ||
      parsed.password ||
      (parsed.pathname !== "" && parsed.pathname !== "/") ||
      parsed.search ||
      parsed.hash
    ) {
      return false;
    }
    return allowedOrigins.has(parsed.origin);
  } catch {
    return false;
  }
}
