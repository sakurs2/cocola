"use client";

import {
  MarkdownTextPrimitive,
  type CodeHeaderProps,
  type SyntaxHighlighterProps,
} from "@assistant-ui/react-markdown";
import { CheckIcon, CopyIcon } from "lucide-react";
import { type FC, type ReactNode, useState } from "react";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

// Renders assistant text as GitHub-flavored Markdown, styled with the shadcn
// design tokens (app/globals.css). Dependency-light: code blocks get chrome
// and copy affordances without adding a syntax highlighter dependency.
export function MarkdownText() {
  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      defer
      components={{
        CodeHeader,
        SyntaxHighlighter,
      }}
      className={cn(
        "aui-stream-in text-sm leading-7 text-foreground",
        "[&_p]:my-2.5 [&_p:first-child]:mt-0 [&_p:last-child]:mb-0",
        "[&_ul]:my-2.5 [&_ol]:my-2.5 [&_li]:my-1 [&_li>p]:my-1",
        "[&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5",
        "[&_h1]:mb-2.5 [&_h1]:mt-5 [&_h1]:text-xl [&_h1]:font-semibold [&_h1]:tracking-normal",
        "[&_h2]:mb-2 [&_h2]:mt-5 [&_h2]:border-b [&_h2]:border-border/70 [&_h2]:pb-1.5 [&_h2]:text-lg [&_h2]:font-semibold [&_h2]:tracking-normal",
        "[&_h3]:mb-1.5 [&_h3]:mt-4 [&_h3]:text-base [&_h3]:font-semibold [&_h3]:tracking-normal",
        "[&_a]:font-medium [&_a]:text-primary [&_a]:underline [&_a]:decoration-border [&_a]:underline-offset-4 [&_a:hover]:decoration-primary",
        "[&_code]:rounded-md [&_code]:border [&_code]:border-border/70 [&_code]:bg-muted/80 [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[0.85em]",
        "[&_pre_code]:border-0 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:text-inherit",
        "[&_blockquote]:my-3 [&_blockquote]:border-l-2 [&_blockquote]:border-primary/60 [&_blockquote]:bg-muted/30 [&_blockquote]:py-1 [&_blockquote]:pl-4 [&_blockquote]:text-muted-foreground",
        "[&_table]:my-3 [&_table]:w-full [&_table]:border-collapse [&_table]:text-sm",
        "[&_th]:border [&_th]:border-border [&_th]:bg-muted/50 [&_th]:px-2.5 [&_th]:py-1.5 [&_th]:text-left [&_th]:font-medium",
        "[&_td]:border [&_td]:border-border [&_td]:px-2.5 [&_td]:py-1.5",
        "[&_hr]:my-5 [&_hr]:border-border",
      )}
    />
  );
}

const CodeHeader: FC<CodeHeaderProps> = ({ language, code }) => {
  const [copied, setCopied] = useState(false);
  const label = language && language !== "unknown" ? language : "text";

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className="flex items-center justify-between gap-3 border-b border-border bg-muted/40 px-3 py-2">
      <span className="min-w-0 truncate font-mono text-[11px] uppercase text-muted-foreground">
        {label}
      </span>
      <button
        type="button"
        onClick={onCopy}
        className="aui-code-action inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-background hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        aria-label={copied ? "Copied code" : "Copy code"}
      >
        {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
      </button>
    </div>
  );
};

const SyntaxHighlighter: FC<SyntaxHighlighterProps> = ({
  components: { Pre, Code },
  language,
  code,
}) => {
  const normalized = normalizeLanguage(language);
  const lines = code.replace(/\n$/, "").split("\n");

  return (
    <Pre className="overflow-x-auto bg-[#0f1011] p-4 text-[13px] leading-6 text-[#eceff4]">
      <Code className="block whitespace-pre font-mono">
        {lines.map((line, index) => (
          <span key={index} className={cn("block min-h-6", diffLineClass(normalized, line))}>
            {highlightLine(line, normalized)}
          </span>
        ))}
      </Code>
    </Pre>
  );
};

const normalizeLanguage = (language?: string) => {
  switch ((language ?? "").toLowerCase()) {
    case "bash":
    case "sh":
    case "shell":
    case "zsh":
      return "shell";
    case "js":
    case "jsx":
    case "javascript":
      return "javascript";
    case "ts":
    case "tsx":
    case "typescript":
      return "typescript";
    case "py":
    case "python":
      return "python";
    case "go":
    case "golang":
      return "go";
    case "json":
    case "jsonc":
      return "json";
    case "diff":
    case "patch":
      return "diff";
    default:
      return "generic";
  }
};

const diffLineClass = (language: string, line: string) => {
  if (language !== "diff") return "";
  if (line.startsWith("+")) return "bg-emerald-500/10 text-emerald-200";
  if (line.startsWith("-")) return "bg-red-500/10 text-red-200";
  if (line.startsWith("@@")) return "bg-sky-500/10 text-sky-200";
  return "";
};

const highlightLine = (line: string, language: string): ReactNode[] => {
  if (language === "json") return highlightWith(line, jsonPattern, jsonClass);
  if (language === "shell") return highlightWith(line, shellPattern, shellClass);
  if (language === "diff") return [line];
  return highlightWith(line, sourcePattern, (token) => sourceClass(token, language));
};

const highlightWith = (
  line: string,
  pattern: RegExp,
  classify: (token: string, line: string, index: number) => string,
): ReactNode[] => {
  const nodes: ReactNode[] = [];
  let last = 0;
  for (const match of line.matchAll(pattern)) {
    const token = match[0];
    const index = match.index ?? 0;
    if (index > last) nodes.push(line.slice(last, index));
    nodes.push(
      <span key={`${index}-${token}`} className={classify(token, line, index)}>
        {token}
      </span>,
    );
    last = index + token.length;
  }
  if (last < line.length) nodes.push(line.slice(last));
  return nodes.length > 0 ? nodes : [line || " "];
};

const jsonPattern =
  /"(?:\\.|[^"\\])*"(?=\s*:)|"(?:\\.|[^"\\])*"|\b(?:true|false|null)\b|-?\b\d+(?:\.\d+)?(?:e[+-]?\d+)?\b|[{}[\]:,]/gi;

const jsonClass = (token: string, line: string, index: number) => {
  if (token.startsWith('"')) {
    const after = line.slice(index + token.length).trimStart();
    return after.startsWith(":") ? "text-emerald-200" : "text-amber-200";
  }
  if (/^(true|false|null)$/i.test(token)) return "text-violet-200";
  if (/^-?\d/.test(token)) return "text-sky-200";
  return "text-muted-foreground";
};

const shellPattern =
  /#.*|\b(?:cd|cp|curl|echo|export|git|go|grep|make|mkdir|npm|pnpm|rm|sed|uv)\b|--?[a-zA-Z0-9][\w-]*|\$[A-Za-z_][\w]*|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'/g;

const shellClass = (token: string) => {
  if (token.startsWith("#")) return "text-slate-500";
  if (token.startsWith("-")) return "text-sky-200";
  if (token.startsWith("$")) return "text-violet-200";
  if (token.startsWith('"') || token.startsWith("'")) return "text-amber-200";
  return "text-emerald-200";
};

const sourcePattern =
  /\/\/.*|#.*|\/\*.*?\*\/|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\b[A-Za-z_]\w*(?=\s*\()|\b[A-Za-z_]\w*\b|-?\b\d+(?:\.\d+)?\b/g;

const genericKeywords = new Set([
  "class",
  "const",
  "def",
  "else",
  "for",
  "func",
  "function",
  "if",
  "return",
]);

const keywords: Record<string, Set<string>> = {
  javascript: new Set([
    "async",
    "await",
    "break",
    "case",
    "catch",
    "class",
    "const",
    "default",
    "else",
    "export",
    "extends",
    "finally",
    "for",
    "from",
    "function",
    "if",
    "import",
    "let",
    "new",
    "return",
    "switch",
    "throw",
    "try",
    "type",
    "undefined",
  ]),
  typescript: new Set([
    "async",
    "await",
    "break",
    "case",
    "catch",
    "class",
    "const",
    "default",
    "else",
    "export",
    "extends",
    "finally",
    "for",
    "from",
    "function",
    "if",
    "import",
    "interface",
    "let",
    "new",
    "return",
    "satisfies",
    "switch",
    "throw",
    "try",
    "type",
    "undefined",
  ]),
  python: new Set([
    "and",
    "as",
    "async",
    "await",
    "class",
    "def",
    "elif",
    "else",
    "except",
    "False",
    "finally",
    "for",
    "from",
    "if",
    "import",
    "in",
    "is",
    "None",
    "not",
    "or",
    "return",
    "True",
    "try",
    "with",
    "yield",
  ]),
  go: new Set([
    "break",
    "case",
    "chan",
    "const",
    "continue",
    "defer",
    "else",
    "fallthrough",
    "for",
    "func",
    "go",
    "if",
    "import",
    "interface",
    "map",
    "nil",
    "package",
    "range",
    "return",
    "select",
    "struct",
    "switch",
    "type",
    "var",
  ]),
  generic: genericKeywords,
};

const sourceClass = (token: string, language: string) => {
  if (token.startsWith("//") || token.startsWith("#") || token.startsWith("/*")) {
    return "text-slate-500";
  }
  if (token.startsWith('"') || token.startsWith("'") || token.startsWith("`")) {
    return "text-amber-200";
  }
  if (/^-?\d/.test(token)) return "text-sky-200";
  if ((keywords[language] ?? genericKeywords).has(token)) return "text-violet-200";
  if (/^[A-Za-z_]\w*$/.test(token)) return "text-emerald-200";
  return "text-[#eceff4]";
};
