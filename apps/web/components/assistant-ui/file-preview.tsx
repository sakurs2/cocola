"use client";

import { MarkdownContent } from "@/components/assistant-ui/markdown-text";
import { FileQuestion, LoaderCircle, RefreshCw } from "lucide-react";
import dynamic from "next/dynamic";
import { useEffect, useState } from "react";

const MonacoEditor = dynamic(() => import("@monaco-editor/react"), { ssr: false });

export type PreviewFile = {
  filename: string;
  size: number;
  mimeType: string;
  url: string;
  previewKind?: "markdown" | "code" | "image" | "pdf";
};

export function ReadonlyFilePreview({
  file,
  renderHtml = false,
  fetchBinary = false,
  unsupportedMessage = "This file type cannot be previewed here.",
}: {
  file: PreviewFile;
  renderHtml?: boolean;
  fetchBinary?: boolean;
  unsupportedMessage?: string;
}) {
  const [text, setText] = useState("");
  const [binaryUrl, setBinaryUrl] = useState("");
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const canText =
    file.previewKind === "markdown" ||
    file.previewKind === "code" ||
    isTextPreview(file.mimeType, file.filename);
  const canHtml = isHtmlPreview(file.mimeType, file.filename);
  const canImage = file.previewKind === "image" || file.mimeType.startsWith("image/");
  const canPdf = file.previewKind === "pdf" || file.mimeType === "application/pdf";
  const fetchPreview = canText || (fetchBinary && (canImage || canPdf));
  const [loading, setLoading] = useState(fetchPreview);
  const previewKind =
    file.previewKind === "markdown" ? "markdown" : getTextPreviewKind(file.mimeType, file.filename);
  const language = getCodeLanguage(file.mimeType, file.filename);

  useEffect(() => {
    let cancelled = false;
    let createdUrl = "";
    setText("");
    setBinaryUrl("");
    setError("");
    setLoading(fetchPreview);
    if (!fetchPreview) return;
    void (async () => {
      try {
        const response = await fetch(file.url, { cache: "no-store" });
        if (!response.ok) {
          const body = (await response.json().catch(() => null)) as {
            error?: { message?: string };
          } | null;
          throw new Error(body?.error?.message || `Preview failed (${response.status})`);
        }
        if (canText) {
          const body = await response.text();
          if (!cancelled) setText(body);
        } else {
          createdUrl = URL.createObjectURL(await response.blob());
          if (cancelled) {
            URL.revokeObjectURL(createdUrl);
            createdUrl = "";
          } else {
            setBinaryUrl(createdUrl);
          }
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
      if (createdUrl) URL.revokeObjectURL(createdUrl);
    };
  }, [canText, fetchPreview, file.url, retry]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <LoaderCircle className="size-4 animate-spin" /> Loading preview
      </div>
    );
  }
  if (error) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
        <p className="max-w-80 text-xs leading-5 text-destructive">{error}</p>
        <button
          type="button"
          onClick={() => setRetry((value) => value + 1)}
          className="inline-flex items-center gap-2 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted"
        >
          <RefreshCw className="size-3.5" /> Retry
        </button>
      </div>
    );
  }

  if (canImage) {
    return (
      <div className="flex min-h-full items-start justify-center p-4">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={fetchBinary ? binaryUrl : file.url}
          alt={file.filename}
          className="max-h-full max-w-full rounded-xl border border-border bg-card object-contain shadow-sm"
        />
      </div>
    );
  }
  if (canPdf) {
    return (
      <iframe
        title={file.filename}
        src={fetchBinary ? binaryUrl : file.url}
        className="h-full w-full"
      />
    );
  }
  if (canHtml && renderHtml && !error && text) {
    return (
      <iframe
        title={file.filename}
        srcDoc={text}
        sandbox="allow-forms allow-modals allow-popups allow-scripts"
        className="h-full w-full bg-white"
      />
    );
  }
  if (canText) {
    return <TextFilePreview text={text} kind={previewKind} language={language} />;
  }
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
      <FileQuestion className="size-8" />
      <p className="text-sm font-medium text-foreground">Preview not supported</p>
      <p className="text-xs">{unsupportedMessage}</p>
    </div>
  );
}

function TextFilePreview({
  text,
  kind,
  language,
}: {
  text: string;
  kind: "markdown" | "code" | "plain";
  language: string;
}) {
  if (kind === "markdown") {
    return <MarkdownContent value={text} className="p-4" />;
  }
  return (
    <div className="h-full min-h-[420px]">
      <MonacoEditor
        language={kind === "code" ? toMonacoLanguage(language) : "plaintext"}
        value={text}
        theme="vs"
        options={{
          readOnly: true,
          minimap: { enabled: false },
          fontSize: 12,
          lineHeight: 20,
          scrollBeyondLastLine: false,
          wordWrap: "on",
          renderLineHighlight: "none",
          overviewRulerBorder: false,
        }}
      />
    </div>
  );
}

function toMonacoLanguage(language: string): string {
  if (language === "shell") return "shell";
  if (language === "text") return "plaintext";
  return language;
}

export function isHtmlPreview(mime: string, filename: string): boolean {
  const ext = getKnownTextExtension(filename);
  return mime === "text/html" || ext === "html" || ext === "htm";
}

function isTextPreview(mime: string, filename: string): boolean {
  if (mime.startsWith("text/")) return true;
  if (["application/json", "application/xml", "application/javascript"].includes(mime)) return true;
  return getKnownTextExtension(filename) !== "";
}

function getTextPreviewKind(mime: string, filename: string): "markdown" | "code" | "plain" {
  const ext = getKnownTextExtension(filename);
  if (mime === "text/markdown" || ext === "md" || ext === "markdown") return "markdown";
  if (getCodeLanguage(mime, filename) !== "text") return "code";
  return "plain";
}

function getCodeLanguage(mime: string, filename: string): string {
  if (mime === "application/json") return "json";
  if (mime === "application/xml") return "xml";
  if (mime === "application/javascript") return "javascript";

  const ext = getKnownTextExtension(filename);
  const languages: Record<string, string> = {
    bash: "shell",
    c: "c",
    cpp: "cpp",
    css: "css",
    csv: "text",
    diff: "diff",
    go: "go",
    h: "c",
    htm: "html",
    html: "html",
    java: "java",
    js: "javascript",
    jsx: "javascript",
    json: "json",
    kt: "kotlin",
    log: "text",
    md: "markdown",
    patch: "diff",
    py: "python",
    rs: "rust",
    sh: "shell",
    toml: "toml",
    ts: "typescript",
    tsx: "typescript",
    xml: "xml",
    yaml: "yaml",
    yml: "yaml",
    zsh: "shell",
  };
  return languages[ext] ?? "text";
}

function getKnownTextExtension(filename: string): string {
  const match = /\.([a-z0-9]+)$/i.exec(filename);
  const ext = match?.[1]?.toLowerCase() ?? "";
  const known = new Set([
    "bash",
    "c",
    "cpp",
    "css",
    "csv",
    "diff",
    "go",
    "h",
    "htm",
    "html",
    "java",
    "js",
    "jsx",
    "json",
    "kt",
    "log",
    "markdown",
    "md",
    "patch",
    "py",
    "rs",
    "sh",
    "toml",
    "ts",
    "tsx",
    "txt",
    "xml",
    "yaml",
    "yml",
    "zsh",
  ]);
  return known.has(ext) ? ext : "";
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  if (!Number.isFinite(bytes) || bytes < 0) return "Unknown size";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}
