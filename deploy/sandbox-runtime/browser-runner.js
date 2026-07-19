"use strict";

// Root-owned, one-shot Playwright runner for `cocola-sandbox browser ...`.
// It never opens a control port. The caller sends one JSON request on stdin;
// the runner launches a persistent headless context, prints one JSON result,
// then closes Chromium. Network access remains governed by Sandbox egress.

const fs = require("fs");
const path = require("path");
const { chromium } = require("playwright");

const STATE_DIR = "/session/runtime/browser/profile";
const MAX_REQUEST_BYTES = 1024 * 1024;

function fail(message, details = "") {
  process.stdout.write(JSON.stringify({ ok: false, error: message, details }) + "\n");
  process.exitCode = 1;
}

function validateRequest(request) {
  if (!request || typeof request !== "object" || Array.isArray(request)) {
    throw new Error("request must be a JSON object");
  }
  if (!new Set(["inspect", "screenshot", "pdf"]).has(request.action)) {
    throw new Error("unsupported browser action");
  }
  if (typeof request.url !== "string") {
    throw new Error("url is required");
  }
  const parsed = new URL(request.url);
  if (!new Set(["http:", "https:"]).has(parsed.protocol)) {
    throw new Error("only http:// and https:// URLs are supported");
  }
  if (request.output !== undefined && typeof request.output !== "string") {
    throw new Error("output must be a path string");
  }
  return parsed.toString();
}

async function inspectPage(page, request) {
  const maxTextChars = request.max_text_chars;
  let text = "";
  const body = page.locator("body");
  if ((await body.count()) > 0) {
    text = await body.innerText({ timeout: Math.min(request.timeout_ms, 10000) });
  }
  const links = await page.locator("a[href]").evaluateAll((elements) =>
    elements.slice(0, 100).map((element) => ({
      text: (element.textContent || "").trim().slice(0, 500),
      href: element.href,
    })),
  );
  return {
    ok: true,
    action: "inspect",
    url: page.url(),
    title: await page.title(),
    text: text.slice(0, maxTextChars),
    text_truncated: text.length > maxTextChars,
    links,
  };
}

async function saveScreenshot(page, request) {
  const output = request.output;
  fs.mkdirSync(path.dirname(output), { recursive: true });
  await page.screenshot({
    path: output,
    fullPage: request.full_page,
    type: "png",
  });
  const stat = fs.statSync(output);
  return {
    ok: true,
    action: "screenshot",
    url: page.url(),
    title: await page.title(),
    path: output,
    mime_type: "image/png",
    bytes: stat.size,
  };
}

async function savePDF(page, request) {
  const output = request.output;
  fs.mkdirSync(path.dirname(output), { recursive: true });
  await page.pdf({ path: output, printBackground: true, format: "A4" });
  const stat = fs.statSync(output);
  return {
    ok: true,
    action: "pdf",
    url: page.url(),
    title: await page.title(),
    path: output,
    mime_type: "application/pdf",
    bytes: stat.size,
  };
}

async function main(request) {
  const url = validateRequest(request);
  fs.mkdirSync(STATE_DIR, { recursive: true });

  const context = await chromium.launchPersistentContext(STATE_DIR, {
    executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || "/usr/local/bin/chromium",
    headless: true,
    viewport: {
      width: request.viewport_width,
      height: request.viewport_height,
    },
    acceptDownloads: true,
    downloadsPath: "/workspace/downloads",
    args: ["--no-sandbox", "--disable-dev-shm-usage"],
  });

  try {
    const pages = context.pages();
    const page = pages[0] || (await context.newPage());
    page.setDefaultTimeout(request.timeout_ms);
    await page.goto(url, {
      waitUntil: "domcontentloaded",
      timeout: request.timeout_ms,
    });
    if (request.action === "inspect") {
      return await inspectPage(page, request);
    }
    if (request.action === "screenshot") {
      return await saveScreenshot(page, request);
    }
    return await savePDF(page, request);
  } finally {
    await context.close();
  }
}

let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => {
  input += chunk;
  if (Buffer.byteLength(input, "utf8") > MAX_REQUEST_BYTES) {
    fail("browser request is too large");
    process.stdin.destroy();
  }
});
process.stdin.on("end", async () => {
  if (process.exitCode) return;
  try {
    const request = JSON.parse(input);
    const result = await main(request);
    process.stdout.write(JSON.stringify(result) + "\n");
  } catch (error) {
    fail(error instanceof Error ? error.message : String(error));
  }
});
