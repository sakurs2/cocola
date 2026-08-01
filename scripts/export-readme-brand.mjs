#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const assetsDir = join(repoRoot, "docs", "assets");
const wordmarkSource = readFileSync(
  join(repoRoot, "apps", "web", "components", "assistant-ui", "cocola-wordmark.tsx"),
  "utf8",
);
const logoSource = readFileSync(
  join(repoRoot, "apps", "web", "components", "cocola-logo.tsx"),
  "utf8",
);
const taglineSource = readFileSync(
  join(repoRoot, "apps", "web", "components", "assistant-ui", "cocola-tagline.tsx"),
  "utf8",
);

const match = (source, pattern, label) => {
  const value = source.match(pattern)?.[1];
  if (!value) throw new Error(`Unable to extract ${label}`);
  return value;
};

const clipPath = match(wordmarkSource, /const CLIP_D\s*=\s*\n\s*"([^"]+)";/, "wordmark clip path");
const penPathBlock = match(
  wordmarkSource,
  /const PEN_PATHS[^=]*=\s*\[(.*?)\n\];/s,
  "wordmark pen paths",
);
const penPaths = [...penPathBlock.matchAll(/^\s*"([^"]+)",?$/gm)].map((item) => item[1]);
if (penPaths.length === 0) throw new Error("Unable to extract wordmark pen paths");

const sparkle = match(logoSource, /const sparkle = "([^"]+)";/, "logo path");
const tagline = match(taglineSource, /export const TAGLINE_TEXT = "([^"]+)";/, "tagline");
const font = readFileSync(
  join(repoRoot, "apps", "web", "app", "fonts", "cormorant-garamond-italic-500-latin.woff2"),
).toString("base64");

const wordmarkPaths = penPaths
  .map(
    (path) =>
      `<path d="${path}" fill="none" stroke="url(#cocola-ink)" stroke-width="130" stroke-linecap="round" stroke-linejoin="round" />`,
  )
  .join("\n              ");

const html = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Cocola README brand export</title>
    <style>
      @font-face {
        font-family: "Cocola Cormorant";
        src: url("data:font/woff2;base64,${font}") format("woff2");
        font-style: italic;
        font-weight: 500;
        font-display: block;
      }

      * { box-sizing: border-box; }
      html, body {
        width: 640px;
        height: 220px;
        margin: 0;
        overflow: hidden;
        background: #fff;
      }
      body {
        display: flex;
        align-items: center;
        justify-content: center;
      }
      .brand {
        display: flex;
        align-items: center;
        justify-content: center;
        gap: 12px;
      }
      .logo {
        width: 128px;
        height: 128px;
        flex: none;
      }
      .copy {
        display: flex;
        flex-direction: column;
        align-items: center;
        margin-left: -24px;
        text-align: center;
      }
      .wordmark {
        display: block;
        width: auto;
        height: 144px;
        margin: -16px 0;
      }
      .tagline {
        margin: 0;
        color: hsl(222 28% 36%);
        font-family: "Cocola Cormorant", Georgia, "Times New Roman", serif;
        font-size: 31px;
        font-style: italic;
        font-weight: 500;
        line-height: 1.3;
        white-space: nowrap;
      }
    </style>
  </head>
  <body>
    <main class="brand" aria-label="Cocola — ${tagline}">
      <svg class="logo" viewBox="0 0 256 256" role="img" aria-label="Cocola">
        <defs>
          <linearGradient id="cocola-brand" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stop-color="#32A7FD" />
            <stop offset="1" stop-color="#7B48FC" />
          </linearGradient>
        </defs>
        <path d="${sparkle}" fill="url(#cocola-brand)" opacity=".2" />
        <path d="${sparkle}" fill="none" stroke="url(#cocola-brand)" stroke-width="16" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
      <div class="copy">
        <svg class="wordmark" viewBox="0 0 1600 854" role="img" aria-label="Cocola">
          <defs>
            <linearGradient id="cocola-ink" x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" stop-color="hsl(190 92% 44%)" />
              <stop offset="46%" stop-color="hsl(221 83% 58%)" />
              <stop offset="100%" stop-color="hsl(268 78% 60%)" />
            </linearGradient>
            <clipPath id="cocola-letters" clipPathUnits="userSpaceOnUse">
              <path d="${clipPath}" clip-rule="evenodd" />
            </clipPath>
          </defs>
          <g clip-path="url(#cocola-letters)">
              ${wordmarkPaths}
          </g>
        </svg>
        <p class="tagline">${tagline.replace("&", "&amp;")}</p>
      </div>
    </main>
  </body>
</html>
`;

const htmlPath = join(assetsDir, "cocola-readme-brand.html");
const imagePath = join(assetsDir, "cocola-readme-brand.png");
writeFileSync(htmlPath, html);

const chrome =
  process.env.CHROME_BIN ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
if (!existsSync(chrome)) throw new Error(`Chrome not found at ${chrome}`);

execFileSync(
  chrome,
  [
    "--headless=new",
    "--disable-gpu",
    "--hide-scrollbars",
    "--allow-file-access-from-files",
    "--force-device-scale-factor=2",
    "--window-size=640,220",
    "--run-all-compositor-stages-before-draw",
    "--virtual-time-budget=1000",
    `--screenshot=${imagePath}`,
    pathToFileURL(htmlPath).href,
  ],
  { stdio: "inherit" },
);

console.log(`Exported ${imagePath}`);
