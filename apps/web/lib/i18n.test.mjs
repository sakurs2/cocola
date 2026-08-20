import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import {
  DEFAULT_LOCALE,
  DEFAULT_TIME_ZONE,
  isLocale,
  localeFromAcceptLanguage,
  resolveLocale,
  SUPPORTED_LOCALES,
} from "../i18n/config.ts";

const WEB_ROOT = dirname(dirname(fileURLToPath(import.meta.url)));

test("locale resolution prefers a valid cookie and otherwise follows Accept-Language", () => {
  assert.deepEqual(SUPPORTED_LOCALES, ["en", "zh-CN"]);
  assert.equal(DEFAULT_LOCALE, "en");
  assert.equal(DEFAULT_TIME_ZONE, "UTC");
  assert.equal(resolveLocale("en", "zh-CN,zh;q=0.9"), "en");
  assert.equal(resolveLocale("zh-CN", "en-US,en;q=0.9"), "zh-CN");
  assert.equal(resolveLocale("invalid", "zh-TW;q=0.9,en;q=0.7"), "zh-CN");
  assert.equal(localeFromAcceptLanguage("en-US,en;q=0.9,zh;q=0.8"), "en");
  assert.equal(localeFromAcceptLanguage("en;q=0.7,zh-CN;q=0.9"), "zh-CN");
  assert.equal(localeFromAcceptLanguage("fr-FR,fr;q=0.9"), "en");
  assert.equal(isLocale("zh-CN"), true);
  assert.equal(isLocale("zh"), false);
});

test("server and client internationalization share an explicit deterministic time zone", () => {
  const request = readFileSync(join(WEB_ROOT, "i18n/request.ts"), "utf8");
  const layout = readFileSync(join(WEB_ROOT, "app/layout.tsx"), "utf8");
  const provider = readFileSync(join(WEB_ROOT, "components/i18n/app-i18n-provider.tsx"), "utf8");

  assert.match(request, /timeZone: DEFAULT_TIME_ZONE/);
  assert.match(layout, /getTimeZone\(\)/);
  assert.match(layout, /timeZone=\{timeZone\}/);
  assert.match(provider, /timeZone=\{timeZone\}/);
});

test("English and Simplified Chinese dictionaries have identical, non-empty contracts", () => {
  const english = readMessages("en");
  const chinese = readMessages("zh-CN");
  assert.deepEqual(Object.keys(chinese).sort(), Object.keys(english).sort());

  const englishLeaves = flattenMessages(english);
  const chineseLeaves = flattenMessages(chinese);
  assert.deepEqual(Object.keys(chineseLeaves).sort(), Object.keys(englishLeaves).sort());

  for (const [key, englishValue] of Object.entries(englishLeaves)) {
    const chineseValue = chineseLeaves[key];
    assert.ok(englishValue.trim(), `English translation ${key} must not be empty`);
    assert.ok(chineseValue.trim(), `Chinese translation ${key} must not be empty`);
    assert.deepEqual(
      icuParameters(chineseValue),
      icuParameters(englishValue),
      `ICU parameters must match for ${key}`,
    );
  }
});

test("chat directs users without a model to the Admin page", () => {
  const english = readMessages("en").chat;
  const chinese = readMessages("zh-CN").chat;
  const keys = ["noCompatibleModel", "noModelConfigured"];

  for (const key of keys) {
    assert.equal(english[key], "Please configure a model on the Admin page");
    assert.equal(chinese[key], "请在管理员页面配置模型");
  }
  assert.equal(english.model.noneConfigured, "Please configure a model on the Admin page");
  assert.equal(chinese.model.noneConfigured, "请在管理员页面配置模型");
});

test("locale preference endpoint owns a strict, one-year HttpOnly cookie", () => {
  const source = readFileSync(join(WEB_ROOT, "app/api/preferences/locale/route.ts"), "utf8");
  assert.match(source, /if \(!isLocale\(locale\)\)/);
  assert.match(source, /status: 204/);
  assert.match(source, /httpOnly: true/);
  assert.match(source, /sameSite: "lax"/);
  assert.match(source, /path: "\/"/);
  assert.match(source, /secure: requestUsesHTTPS\(request\)/);
  assert.match(source, /request\.headers\.get\("x-forwarded-proto"\)/);
  assert.match(source, /new URL\(request\.url\)\.protocol === "https:"/);
  assert.doesNotMatch(source, /secure: process\.env\.NODE_ENV/);
  assert.match(source, /maxAge: LOCALE_COOKIE_MAX_AGE/);
  assert.doesNotMatch(source, /getServerSession|auth\(/);
});

test("language menu refreshes the current route and is present before and after login", () => {
  const menu = readFileSync(join(WEB_ROOT, "components/i18n/language-menu.tsx"), "utf8");
  const login = readFileSync(join(WEB_ROOT, "app/login/page.tsx"), "utf8");
  const header = readFileSync(
    join(WEB_ROOT, "components/assistant-ui/workspace-header-actions.tsx"),
    "utf8",
  );
  assert.match(menu, /router\.refresh\(\)/);
  assert.doesNotMatch(menu, /router\.(push|replace)\(|location\.(href|assign|replace)/);
  assert.match(login, /<LanguageMenu \/>/);
  assert.match(header, /<LanguageMenu \/>/);
});

test("Web UI source does not add unregistered visible English literals", () => {
  const violations = [];
  for (const file of sourceFiles(join(WEB_ROOT, "app"), join(WEB_ROOT, "components"))) {
    if (file.includes("/app/api/")) continue;
    const source = readFileSync(file, "utf8");
    const sourceFile = ts.createSourceFile(
      file,
      source,
      ts.ScriptTarget.Latest,
      true,
      file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );

    const visit = (node) => {
      if (ts.isJsxText(node)) {
        checkLiteral(node.text.replace(/\s+/g, " ").trim(), node, "JSX text");
      }
      if (ts.isJsxAttribute(node) && VISIBLE_ATTRIBUTES.has(node.name.text) && node.initializer) {
        if (ts.isStringLiteral(node.initializer)) {
          checkLiteral(node.initializer.text, node, node.name.text);
        } else if (ts.isJsxExpression(node.initializer) && node.initializer.expression) {
          checkVisibleExpression(node.initializer.expression, node.name.text, checkLiteral);
        }
      }
      if (ts.isPropertyAssignment(node)) {
        const name =
          ts.isIdentifier(node.name) || ts.isStringLiteral(node.name) ? node.name.text : "";
        if (VISIBLE_PROPERTIES.has(name)) {
          checkVisibleExpression(node.initializer, name, checkLiteral);
        }
      }
      ts.forEachChild(node, visit);
    };

    const checkLiteral = (value, node, kind) => {
      if (!/[A-Za-z]/.test(value) || allowedVisibleLiteral(value, kind)) return;
      const line = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;
      violations.push(
        `${file.slice(WEB_ROOT.length + 1)}:${line} ${kind}: ${JSON.stringify(value)}`,
      );
    };

    visit(sourceFile);
  }
  assert.deepEqual(violations, [], `Untranslated Web literals:\n${violations.join("\n")}`);
});

const VISIBLE_ATTRIBUTES = new Set([
  "aria-label",
  "cancelLabel",
  "confirmLabel",
  "description",
  "emptyMessage",
  "eyebrow",
  "label",
  "placeholder",
  "summary",
  "title",
]);

const VISIBLE_PROPERTIES = new Set([
  "cancelLabel",
  "confirmLabel",
  "description",
  "eyebrow",
  "label",
  "message",
  "placeholder",
  "summary",
  "title",
]);

const ALLOWED_VISIBLE_LITERALS = new Set([
  "/ 32 KB",
  "/bind",
  "24h",
  "30d",
  "7d",
  "90d",
  "AGENTS.md",
  "App ID",
  "App Secret",
  "Anthropic Messages API",
  "Authorization=Bearer ...",
  "CPU",
  "Feishu",
  "GB",
  "GitHub",
  "GITHUB_TOKEN=...",
  "GPT-5",
  "HTTP · URL",
  "ID",
  "ID ·",
  "Lark",
  "MB",
  "MCP",
  "OpenAI Embeddings",
  "OpenAI Embeddings API",
  "Notes.md",
  "Pod",
  "SHA256",
  "SKILL.md",
  "SSE · URL",
  "TTFT",
  "URL",
  "cocola",
  "d",
  "esc",
  "gpt-5",
  "https://api.openai.com/v1",
  "https://example.feishu.cn/docx/...",
  "localhost:",
  "main",
  "my-project",
  "npx",
  "-y\n@modelcontextprotocol/server-github",
  "openai-prod",
  "skills/example",
  "stdio · Command",
  "stdio · ",
  "text-embedding-3-large",
  "v",
]);

function allowedVisibleLiteral(value, kind) {
  if (ALLOWED_VISIBLE_LITERALS.has(value)) return true;
  if (kind === "placeholder" && /^(?:https?:\/\/|cli_|[-@\w./:= ]+\.{3})/.test(value)) {
    return true;
  }
  return false;
}

function checkVisibleExpression(node, kind, checkLiteral) {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    checkLiteral(node.text, node, kind);
    return;
  }
  if (ts.isTemplateExpression(node)) {
    checkLiteral(node.head.text, node.head, kind);
    for (const span of node.templateSpans) {
      checkLiteral(span.literal.text, span.literal, kind);
    }
    return;
  }
  if (ts.isConditionalExpression(node)) {
    checkVisibleExpression(node.whenTrue, kind, checkLiteral);
    checkVisibleExpression(node.whenFalse, kind, checkLiteral);
    return;
  }
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken) {
    checkVisibleExpression(node.left, kind, checkLiteral);
    checkVisibleExpression(node.right, kind, checkLiteral);
    return;
  }
  if (ts.isParenthesizedExpression(node)) {
    checkVisibleExpression(node.expression, kind, checkLiteral);
    return;
  }
  if (
    ts.isCallExpression(node) &&
    (ts.isIdentifier(node.expression) || ts.isPropertyAccessExpression(node.expression))
  ) {
    const callee = ts.isIdentifier(node.expression)
      ? node.expression.text
      : node.expression.name.text;
    if (callee === "t" || callee.endsWith("T")) return;
  }
}

function readMessages(locale) {
  const directory = join(WEB_ROOT, "messages", locale);
  return Object.fromEntries(
    readdirSync(directory)
      .filter((name) => name.endsWith(".json"))
      .sort()
      .map((name) => [name.slice(0, -5), JSON.parse(readFileSync(join(directory, name), "utf8"))]),
  );
}

function flattenMessages(value, prefix = "", result = {}) {
  for (const [key, child] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof child === "string") result[path] = child;
    else if (child && typeof child === "object" && !Array.isArray(child)) {
      flattenMessages(child, path, result);
    } else {
      assert.fail(`Translation ${path} must be a string or object`);
    }
  }
  return result;
}

function icuParameters(value) {
  return [...value.matchAll(/\{([A-Za-z][\w]*)\b/g)].map((match) => match[1]).sort();
}

function sourceFiles(...roots) {
  const files = [];
  const walk = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) walk(path);
      else if (/\.tsx?$/.test(entry.name) && !entry.name.includes(".test.")) files.push(path);
    }
  };
  for (const root of roots) walk(root);
  return files;
}
