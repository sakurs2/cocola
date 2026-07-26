export function normalizeMarkdownForComparison(value) {
  const normalized = String(value ?? "").replace(/\r\n?/g, "\n");
  const withoutTrailingNewlines = normalized.replace(/\n+$/g, "");
  return withoutTrailingNewlines ? `${withoutTrailingNewlines}\n` : "";
}

export function isLosslessMarkdownRoundTrip(source, serialized) {
  return normalizeMarkdownForComparison(source) === normalizeMarkdownForComparison(serialized);
}
