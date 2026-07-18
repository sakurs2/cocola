// Maps a filename / MIME type to a display "kind" that drives the file card:
// which Material icon to render, the short type badge, and which actions to show.
// Icon names below MUST exist in material-file-icons.tsx (validated at build time).

export type FileActionKind = "image" | "text" | "binary";

export interface ResolvedFileType {
  /** Material Icon Theme icon name (key in material-file-icons.tsx). */
  icon: string;
  /** Short uppercase badge, e.g. "PYTHON", "PDF". */
  badge: string;
  /** Whether the card can offer an inline preview. */
  previewable: boolean;
  /** Whether the content is copyable text (adds a copy action). */
  copyable: boolean;
  /** True for raster/vector images -> render a real thumbnail instead of an icon. */
  isImage: boolean;
}

const extIcon: Record<string, string> = {
  // languages
  py: "python",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  ts: "typescript",
  tsx: "react",
  jsx: "react",
  go: "go",
  rs: "rust",
  java: "java",
  cs: "csharp",
  php: "php",
  rb: "ruby",
  c: "c",
  h: "c",
  cpp: "cpp",
  cc: "cpp",
  cxx: "cpp",
  hpp: "cpp",
  sh: "console",
  bash: "console",
  zsh: "console",
  ps1: "powershell",
  vue: "vue",
  css: "css",
  scss: "css",
  less: "css",
  html: "html",
  htm: "html",
  xml: "xml",
  // data / config
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  sql: "database",
  db: "database",
  sqlite: "database",
  lock: "lock",
  ini: "settings",
  conf: "settings",
  cfg: "settings",
  env: "settings",
  // docs
  md: "markdown",
  markdown: "markdown",
  doc: "word",
  docx: "word",
  pdf: "pdf",
  txt: "document",
  rtf: "document",
  // sheets
  xls: "table",
  xlsx: "table",
  csv: "table",
  tsv: "table",
  // media
  png: "image",
  jpg: "image",
  jpeg: "image",
  gif: "image",
  webp: "image",
  bmp: "image",
  ico: "image",
  svg: "svg",
  mp3: "audio",
  wav: "audio",
  ogg: "audio",
  flac: "audio",
  m4a: "audio",
  mp4: "video",
  mov: "video",
  webm: "video",
  mkv: "video",
  avi: "video",
  // archives
  zip: "zip",
  tar: "zip",
  gz: "zip",
  tgz: "zip",
  rar: "zip",
  "7z": "zip",
};

const textExts = new Set([
  "py","js","mjs","cjs","ts","tsx","jsx","go","rs","java","cs","php","rb",
  "c","h","cpp","cc","cxx","hpp","sh","bash","zsh","ps1","vue","css","scss",
  "less","html","htm","xml","json","yaml","yml","toml","sql","lock","ini",
  "conf","cfg","env","md","markdown","txt","rtf","csv","tsv","svg",
]);

const imageExts = new Set(["png","jpg","jpeg","gif","webp","bmp","ico","svg"]);

// Extensions that have a first-class in-app preview surface.
const previewExts = new Set([
  "png","jpg","jpeg","gif","webp","bmp","ico","svg","pdf","md","markdown",
  "txt","csv","tsv","json","yaml","yml","html","htm",
]);

const getExt = (filename: string): string => {
  const base = filename.split(/[\\/]/).pop() ?? filename;
  const dot = base.lastIndexOf(".");
  if (dot <= 0) return "";
  return base.slice(dot + 1).toLowerCase();
};

const badgeFor = (ext: string, mimeType: string): string => {
  if (ext) return ext.toUpperCase();
  const sub = mimeType.split("/").pop() ?? "";
  return (sub || "file").toUpperCase();
};

/**
 * Resolve a file's display type from its name (preferred) and MIME type.
 * Never throws; unknown types fall back to a generic document with download-only.
 */
export const resolveFileType = (
  filename: string,
  mimeType = "",
): ResolvedFileType => {
  const ext = getExt(filename);
  const mime = mimeType.toLowerCase();
  const isImage = imageExts.has(ext) || mime.startsWith("image/");

  let icon = extIcon[ext];
  if (!icon) {
    if (mime.startsWith("image/")) icon = "image";
    else if (mime.startsWith("audio/")) icon = "audio";
    else if (mime.startsWith("video/")) icon = "video";
    else if (mime.startsWith("text/")) icon = "document";
    else if (mime.includes("json")) icon = "json";
    else if (mime.includes("pdf")) icon = "pdf";
    else if (mime.includes("zip") || mime.includes("compressed")) icon = "zip";
    else icon = "document";
  }

  const copyable =
    textExts.has(ext) ||
    (!ext && (mime.startsWith("text/") || mime.includes("json")));
  const previewable =
    isImage || previewExts.has(ext) || mime.startsWith("text/") ||
    mime.startsWith("image/") || mime.includes("pdf");

  return {
    icon,
    badge: badgeFor(ext, mime),
    previewable,
    copyable,
    isImage,
  };
};
