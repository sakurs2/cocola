"use client";

import {
  MarkdownTextPrimitive,
  type CodeHeaderProps,
  type SyntaxHighlighterProps,
} from "@assistant-ui/react-markdown";
import { CheckIcon, CopyIcon } from "lucide-react";
import {
  Children,
  type ComponentProps,
  type FC,
  isValidElement,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import { useTranslations } from "next-intl";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

const answerMarkdownComponents = {
  a: ({ node: _node, href, ...props }) => {
    const external = /^https?:\/\//i.test(href ?? "");
    return (
      <a
        {...props}
        href={href}
        target={external ? "_blank" : undefined}
        rel={external ? "noreferrer" : undefined}
      />
    );
  },
  table: AnswerMarkdownTable,
} satisfies Components;

function AnswerMarkdownTable({
  node: _node,
  ...props
}: ComponentProps<"table"> & { node?: unknown }) {
  const t = useTranslations("chat.markdown");
  return (
    <div
      className="my-5 max-w-full overflow-x-auto rounded-xl border border-border/70 bg-surface shadow-[0_1px_2px_oklch(from var(--foreground) l c h / 0.03)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-inset"
      role="region"
      aria-label={t("scrollableTable")}
      tabIndex={0}
    >
      <table
        {...props}
        className="w-full min-w-[32rem] border-separate border-spacing-0 text-left text-[13px] leading-5"
      />
    </div>
  );
}

// Live assistant answers use the streaming primitive, while persisted answers
// use AnswerMarkdownContent below. Both share the same GFM and presentation
// contract; Plan and file-preview Markdown intentionally remain separate.
export function MarkdownText() {
  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      defer
      components={{
        ...answerMarkdownComponents,
        CodeHeader,
        SyntaxHighlighter,
      }}
      className={answerMarkdownClassName}
    />
  );
}

export function AnswerMarkdownContent({ value, className }: { value: string; className?: string }) {
  return (
    <div className={cn(answerMarkdownClassName, className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          ...answerMarkdownComponents,
          pre: PersistedCodeBlock,
        }}
      >
        {value}
      </ReactMarkdown>
    </div>
  );
}

export function MarkdownContent({ value, className }: { value: string; className?: string }) {
  return (
    <div className={cn(contentMarkdownClassName, className)}>{renderMarkdownBlocks(value)}</div>
  );
}

export function CodeBlock({
  language,
  code,
  className,
}: {
  language?: string;
  code: string;
  className?: string;
}) {
  const normalizedLanguage = language ?? "text";

  return (
    <div className={className}>
      <CodeHeader language={normalizedLanguage} code={code} />
      <SyntaxHighlighter
        language={normalizedLanguage}
        code={code}
        components={{
          Pre: ({ children, className: preClassName }) => (
            <pre className={preClassName}>{children}</pre>
          ),
          Code: ({ children, className: codeClassName }) => (
            <code className={codeClassName}>{children}</code>
          ),
        }}
      />
    </div>
  );
}

const answerMarkdownClassName = cn(
  "aui-answer-markdown aui-stream-in min-w-0 font-sans text-[15px] font-normal leading-6 tracking-[-0.005em] text-foreground antialiased",
  "[&>*:first-child]:mt-0 [&>*:last-child]:mb-0",
  "[&_p]:my-2 [&_p]:max-w-[76ch] [&_p]:break-words [&_p]:text-pretty",
  "[&_strong]:font-semibold [&_strong]:text-foreground",
  "[&_em]:text-foreground/90",
  "[&_h1]:mt-7 [&_h1]:mb-3 [&_h1]:max-w-[76ch] [&_h1]:text-[1.35rem] [&_h1]:leading-7 [&_h1]:font-semibold [&_h1]:tracking-[-0.018em]",
  "[&_h2]:mt-7 [&_h2]:mb-2.5 [&_h2]:max-w-[76ch] [&_h2]:border-b [&_h2]:border-border/60 [&_h2]:pb-2 [&_h2]:text-[1.125rem] [&_h2]:leading-7 [&_h2]:font-semibold [&_h2]:tracking-[-0.012em]",
  "[&_h3]:mt-5 [&_h3]:mb-2 [&_h3]:max-w-[76ch] [&_h3]:text-[0.975rem] [&_h3]:leading-6 [&_h3]:font-semibold",
  "[&_ul]:my-2.5 [&_ul]:max-w-[76ch] [&_ul]:list-disc [&_ul]:pl-5",
  "[&_ol]:my-2.5 [&_ol]:max-w-[76ch] [&_ol]:list-decimal [&_ol]:pl-5",
  "[&_li]:my-1 [&_li]:pl-1 [&_li>p]:my-1",
  "[&_li::marker]:font-medium [&_li::marker]:text-muted/75",
  "[&_ul_ul]:my-1.5 [&_ul_ol]:my-1.5 [&_ol_ul]:my-1.5 [&_ol_ol]:my-1.5",
  "[&_.task-list-item]:list-none [&_.task-list-item]:pl-0",
  "[&_.task-list-item>input]:mr-2 [&_.task-list-item>input]:size-3.5 [&_.task-list-item>input]:accent-primary",
  "[&_blockquote]:my-3 [&_blockquote]:max-w-[76ch] [&_blockquote]:rounded-r-xl [&_blockquote]:border-l-[3px] [&_blockquote]:border-indigo-500/55 [&_blockquote]:bg-indigo-500/[0.045] [&_blockquote]:px-4 [&_blockquote]:py-2.5 [&_blockquote]:text-foreground/80",
  "[&_blockquote_p]:my-1.5",
  "[&_a]:font-medium [&_a]:text-accent [&_a]:underline [&_a]:decoration-primary/30 [&_a]:decoration-1 [&_a]:underline-offset-4 [&_a]:transition-colors [&_a]:[overflow-wrap:anywhere] [&_a:hover]:decoration-primary [&_a:focus-visible]:rounded-sm [&_a:focus-visible]:outline-none [&_a:focus-visible]:ring-2 [&_a:focus-visible]:ring-focus [&_a:focus-visible]:ring-offset-2",
  "[&_code]:rounded-md [&_code]:border [&_code]:border-border/70 [&_code]:bg-surface-secondary/75 [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[0.86em] [&_code]:font-medium",
  "[&_pre_code]:border-0 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:text-inherit",
  "[&_thead]:bg-surface-secondary/45",
  "[&_th]:whitespace-nowrap [&_th]:border-b [&_th]:border-border/70 [&_th]:px-3.5 [&_th]:py-2.5 [&_th]:font-semibold [&_th]:text-foreground",
  "[&_td]:border-b [&_td]:border-border/55 [&_td]:px-3.5 [&_td]:py-2.5 [&_td]:align-top",
  "[&_tbody_tr:last-child_td]:border-b-0",
  "[&_tbody_tr]:transition-colors [&_tbody_tr:hover]:bg-surface-secondary/25",
  "[&_hr]:my-7 [&_hr]:border-border/70",
  "[&_del]:text-muted [&_del]:decoration-muted-foreground/60",
  "[&_img]:my-5 [&_img]:max-h-[32rem] [&_img]:max-w-full [&_img]:rounded-xl [&_img]:border [&_img]:border-border/70 [&_img]:bg-surface-secondary/20 [&_img]:object-contain",
);

const contentMarkdownClassName = cn(
  "aui-stream-in text-[15px] leading-7 text-foreground",
  "[&_p]:my-2.5 [&_p:first-child]:mt-0 [&_p:last-child]:mb-0",
  "[&_ul]:my-2.5 [&_ol]:my-2.5 [&_li]:my-1 [&_li>p]:my-1",
  "[&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5",
  "[&_h1]:mb-2.5 [&_h1]:mt-5 [&_h1]:text-xl [&_h1]:font-semibold [&_h1]:tracking-normal",
  "[&_h2]:mb-2 [&_h2]:mt-5 [&_h2]:border-b [&_h2]:border-border/70 [&_h2]:pb-1.5 [&_h2]:text-lg [&_h2]:font-semibold [&_h2]:tracking-normal",
  "[&_h3]:mb-1.5 [&_h3]:mt-4 [&_h3]:text-base [&_h3]:font-semibold [&_h3]:tracking-normal",
  "[&_a]:font-medium [&_a]:text-accent [&_a]:underline [&_a]:decoration-border [&_a]:underline-offset-4 [&_a:hover]:decoration-primary",
  "[&_code]:rounded-md [&_code]:border [&_code]:border-border/70 [&_code]:bg-surface-secondary/80 [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[0.85em]",
  "[&_pre_code]:border-0 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:text-inherit",
  "[&_blockquote]:my-3 [&_blockquote]:border-l-2 [&_blockquote]:border-accent/60 [&_blockquote]:bg-surface-secondary/30 [&_blockquote]:py-1 [&_blockquote]:pl-4 [&_blockquote]:text-muted",
  "[&_table]:my-3 [&_table]:w-full [&_table]:border-collapse [&_table]:text-sm",
  "[&_th]:border [&_th]:border-border [&_th]:bg-surface-secondary/50 [&_th]:px-2.5 [&_th]:py-1.5 [&_th]:text-left [&_th]:font-medium",
  "[&_td]:border [&_td]:border-border [&_td]:px-2.5 [&_td]:py-1.5",
  "[&_hr]:my-5 [&_hr]:border-border",
);

const PersistedCodeBlock: Components["pre"] = ({ node: _node, children, ...props }) => {
  const child = Children.toArray(children)[0];
  if (
    Children.count(children) === 1 &&
    isValidElement<{ className?: string; children?: ReactNode }>(child) &&
    typeof child.props.children === "string"
  ) {
    const language = /language-([\w+-]+)/.exec(child.props.className ?? "")?.[1] ?? "text";
    return <CodeBlock language={language} code={child.props.children} />;
  }
  return <pre {...props}>{children}</pre>;
};

const renderMarkdownBlocks = (value: string): ReactNode[] => {
  const lines = value.replace(/\r\n/g, "\n").split("\n");
  const nodes: ReactNode[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index] ?? "";
    if (!line.trim()) {
      index += 1;
      continue;
    }

    const fence = /^```([\w+-]*)\s*$/.exec(line.trim());
    if (fence) {
      const code: string[] = [];
      index += 1;
      while (index < lines.length && !/^```\s*$/.test((lines[index] ?? "").trim())) {
        code.push(lines[index] ?? "");
        index += 1;
      }
      if (index < lines.length) index += 1;
      nodes.push(
        <CodeBlock
          key={`code-${index}`}
          language={fence[1] || "text"}
          code={`${code.join("\n")}\n`}
          className="my-3"
        />,
      );
      continue;
    }

    const heading = /^(#{1,3})\s+(.+)$/.exec(line);
    if (heading) {
      const level = (heading[1] ?? "").length;
      const content = renderInline(heading[2] ?? "", `h-${index}`);
      nodes.push(
        level === 1 ? (
          <h1 key={`h-${index}`}>{content}</h1>
        ) : level === 2 ? (
          <h2 key={`h-${index}`}>{content}</h2>
        ) : (
          <h3 key={`h-${index}`}>{content}</h3>
        ),
      );
      index += 1;
      continue;
    }

    if (line.trimStart().startsWith(">")) {
      const quote: string[] = [];
      while (index < lines.length && (lines[index] ?? "").trimStart().startsWith(">")) {
        quote.push((lines[index] ?? "").trimStart().replace(/^>\s?/, ""));
        index += 1;
      }
      nodes.push(
        <blockquote key={`q-${index}`}>{renderInline(quote.join(" "), `q-${index}`)}</blockquote>,
      );
      continue;
    }

    if (isTableStart(lines, index)) {
      const rows: string[][] = [];
      rows.push(splitTableRow(lines[index] ?? ""));
      index += 2;
      while (index < lines.length && (lines[index] ?? "").includes("|")) {
        rows.push(splitTableRow(lines[index] ?? ""));
        index += 1;
      }
      const [head = [], ...body] = rows;
      nodes.push(
        <table key={`t-${index}`}>
          <thead>
            <tr>
              {head.map((cell, cellIndex) => (
                <th key={cellIndex}>{renderInline(cell, `th-${index}-${cellIndex}`)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {body.map((row, rowIndex) => (
              <tr key={rowIndex}>
                {row.map((cell, cellIndex) => (
                  <td key={cellIndex}>
                    {renderInline(cell, `td-${index}-${rowIndex}-${cellIndex}`)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>,
      );
      continue;
    }

    const listMatch = /^(\s*)([-*+]|\d+\.)\s+(.+)$/.exec(line);
    if (listMatch) {
      const ordered = /\d+\./.test(listMatch[2] ?? "");
      const items: string[] = [];
      while (index < lines.length) {
        const item = /^(\s*)([-*+]|\d+\.)\s+(.+)$/.exec(lines[index] ?? "");
        if (!item || /\d+\./.test(item[2] ?? "") !== ordered) break;
        items.push(item[3] ?? "");
        index += 1;
      }
      nodes.push(
        ordered ? (
          <ol key={`ol-${index}`}>
            {items.map((item, itemIndex) => (
              <li key={itemIndex}>{renderInline(item, `ol-${index}-${itemIndex}`)}</li>
            ))}
          </ol>
        ) : (
          <ul key={`ul-${index}`}>
            {items.map((item, itemIndex) => (
              <li key={itemIndex}>{renderInline(item, `ul-${index}-${itemIndex}`)}</li>
            ))}
          </ul>
        ),
      );
      continue;
    }

    const paragraph: string[] = [];
    while (index < lines.length && lines[index]?.trim() && !isBlockBoundary(lines, index)) {
      paragraph.push(lines[index] ?? "");
      index += 1;
    }
    nodes.push(<p key={`p-${index}`}>{renderInline(paragraph.join(" "), `p-${index}`)}</p>);
  }

  return nodes;
};

const isBlockBoundary = (lines: string[], index: number) => {
  const line = lines[index] ?? "";
  return (
    /^```/.test(line.trim()) ||
    /^(#{1,3})\s+/.test(line) ||
    line.trimStart().startsWith(">") ||
    /^(\s*)([-*+]|\d+\.)\s+/.test(line) ||
    isTableStart(lines, index)
  );
};

const isTableStart = (lines: string[], index: number) =>
  (lines[index] ?? "").includes("|") &&
  /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[index + 1] ?? "");

const splitTableRow = (line: string) =>
  line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((cell) => cell.trim());

const renderInline = (text: string, keyPrefix: string): ReactNode[] => {
  const nodes: ReactNode[] = [];
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g;
  let last = 0;

  for (const match of text.matchAll(pattern)) {
    const token = match[0];
    const index = match.index ?? 0;
    if (index > last) nodes.push(text.slice(last, index));
    nodes.push(renderInlineToken(token, `${keyPrefix}-${index}`));
    last = index + token.length;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return nodes.length > 0 ? nodes : [text];
};

const renderInlineToken = (token: string, key: string): ReactNode => {
  if (token.startsWith("`") && token.endsWith("`")) {
    return <code key={key}>{token.slice(1, -1)}</code>;
  }
  if (token.startsWith("**") && token.endsWith("**")) {
    return <strong key={key}>{token.slice(2, -2)}</strong>;
  }
  const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(token);
  if (link) {
    const label = link[1] ?? "";
    const href = safeHref(link[2] ?? "");
    return href ? (
      <a key={key} href={href} target="_blank" rel="noreferrer">
        {label}
      </a>
    ) : (
      label
    );
  }
  return token;
};

const safeHref = (href: string) =>
  /^(https?:|mailto:|\/|#)/i.test(href.trim()) ? href.trim() : "";

async function copyTextToClipboard(text: string) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // HTTP deployments may expose the Clipboard API but reject writes.
    }
  }

  const activeElement =
    document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.readOnly = true;
  textarea.style.position = "fixed";
  textarea.style.inset = "0";
  textarea.style.opacity = "0";
  textarea.style.pointerEvents = "none";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();

  let copied = false;
  try {
    copied = document.execCommand("copy");
  } finally {
    textarea.remove();
    activeElement?.focus();
  }
  if (!copied) throw new Error("Clipboard copy failed");
}

const CodeHeader: FC<CodeHeaderProps> = ({ language, code }) => {
  const t = useTranslations("chat.markdown");
  const [copied, setCopied] = useState(false);
  const copiedTimeoutRef = useRef<number | null>(null);
  const label = language && language !== "unknown" ? language : "text";
  const shell = normalizeLanguage(language) === "shell";

  useEffect(
    () => () => {
      if (copiedTimeoutRef.current != null) window.clearTimeout(copiedTimeoutRef.current);
    },
    [],
  );

  const onCopy = async () => {
    if (copiedTimeoutRef.current != null) {
      window.clearTimeout(copiedTimeoutRef.current);
      copiedTimeoutRef.current = null;
    }
    try {
      await copyTextToClipboard(code);
      setCopied(true);
      copiedTimeoutRef.current = window.setTimeout(() => {
        setCopied(false);
        copiedTimeoutRef.current = null;
      }, 1200);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className="mt-3 grid h-7 grid-cols-[1fr_auto_1fr] items-center rounded-t-lg border-x border-t border-zinc-700/80 bg-[#191b1f] px-2.5">
      <span className="flex items-center gap-1.5" aria-hidden="true">
        {shell ? (
          <>
            <span className="size-2 rounded-full bg-[#ff5f57]" />
            <span className="size-2 rounded-full bg-[#febc2e]" />
            <span className="size-2 rounded-full bg-[#28c840]" />
          </>
        ) : null}
      </span>
      <span className="min-w-0 truncate font-mono text-[10px] lowercase tracking-[0.04em] text-zinc-400">
        {label}
      </span>
      <button
        type="button"
        onClick={onCopy}
        className="aui-code-action ml-auto inline-flex size-6 items-center justify-center rounded-md text-zinc-400 transition-colors hover:bg-white/10 hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-focus"
        aria-label={copied ? t("copiedCode") : t("copyCode")}
        title={copied ? t("copied") : t("copyCode")}
      >
        {copied ? (
          <CheckIcon className="size-3 text-emerald-400" />
        ) : (
          <CopyIcon className="size-3" />
        )}
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
  const shell = normalized === "shell";
  const lines = code.replace(/\n$/, "").split("\n");

  return (
    <Pre
      className={cn(
        "mb-3 rounded-b-lg border-x border-b border-zinc-700/80 bg-[#0d0f12] px-3 py-2.5 text-[12px] leading-5 text-[#eceff4] shadow-[0_1px_2px_rgba(0,0,0,0.16)]",
        shell ? "overflow-x-hidden" : "overflow-x-auto",
      )}
    >
      <Code
        className={cn(
          "block min-w-0 font-mono",
          shell ? "whitespace-pre-wrap break-words [overflow-wrap:anywhere]" : "whitespace-pre",
        )}
      >
        {lines.map((line, index) => (
          <span key={index} className={cn("block min-h-5", diffLineClass(normalized, line))}>
            {highlightLine(line, normalized)}
          </span>
        ))}
      </Code>
    </Pre>
  );
};

export function HighlightedCode({
  language,
  code,
  compact = false,
  className,
}: {
  language?: string;
  code: string;
  compact?: boolean;
  className?: string;
}) {
  const normalized = normalizeLanguage(language);
  if (compact) {
    return (
      <code className={className}>
        {highlightLine(code.replace(/\s+/g, " ").trim(), normalized)}
      </code>
    );
  }
  const lines = code.replace(/\n$/, "").split("\n");
  return (
    <code className={className}>
      {lines.map((line, index) => (
        <span key={index} className={cn("block min-h-5", diffLineClass(normalized, line))}>
          {highlightLine(line, normalized)}
        </span>
      ))}
    </code>
  );
}

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
  return "text-muted";
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
