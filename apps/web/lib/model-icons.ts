export const LOCAL_SIMPLE_ICON_PATHS: Record<string, string> = {
  anthropic: "/brands/anthropic.svg",
  claude: "/brands/claude.svg",
  openai: "/brands/openai.svg",
  deepseek: "/brands/deepseek.svg",
  qwen: "/brands/qwen.svg",
  google: "/brands/google.svg",
  googlegemini: "/brands/googlegemini.svg",
};

export const SIMPLE_ICON_SLUGS = Object.keys(LOCAL_SIMPLE_ICON_PATHS);

export const SIMPLE_ICON_LABELS: Record<string, string> = {
  anthropic: "Anthropic",
  claude: "Claude",
  openai: "OpenAI",
  deepseek: "DeepSeek",
  qwen: "Qwen",
  google: "Google",
  googlegemini: "Google Gemini",
  xai: "xAI",
  moonshot: "Moonshot",
};

export const SIMPLE_ICON_FALLBACK_BADGES: Record<string, string> = {
  anthropic: "A",
  claude: "C",
  openai: "AI",
  deepseek: "DS",
  qwen: "Q",
  google: "G",
  googlegemini: "G",
  xai: "xAI",
  moonshot: "K",
};

export const LOBE_ICON_SLUG_ALIASES: Record<string, string> = {
  anthropic: "anthropic",
  claude: "claude",
  claudecode: "claudecode",
  codex: "codex",
  deepseek: "deepseek",
  doubao: "doubao",
  gemini: "gemini",
  google: "google",
  googlegemini: "gemini",
  grok: "grok",
  kimi: "moonshot",
  mistral: "mistral",
  moonshot: "moonshot",
  openai: "openai",
  qwen: "qwen",
  volcengine: "volcengine",
  xai: "xai",
};

export function normalizeLobeIconSlug(slug: string | undefined): string {
  const normalized = (slug ?? "").trim().toLowerCase().replace(/_/g, "-");
  if (!/^[a-z0-9-]+$/.test(normalized)) return "";
  return LOBE_ICON_SLUG_ALIASES[normalized] ?? normalized;
}

export function lobeIconPath(slug: string | undefined): string {
  const normalized = normalizeLobeIconSlug(slug);
  return normalized ? `/api/model-icons/${normalized}` : "";
}
