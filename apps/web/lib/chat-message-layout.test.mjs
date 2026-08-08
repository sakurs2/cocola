import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const threadSource = await readFile(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
  "utf8",
);
const webPackage = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
const rootPackage = JSON.parse(
  await readFile(new URL("../../../package.json", import.meta.url), "utf8"),
);
const serverSource = await readFile(new URL("../server.mjs", import.meta.url), "utf8");
const nextConfigSource = await readFile(new URL("../next.config.mjs", import.meta.url), "utf8");
const sidebarSource = await readFile(
  new URL("../components/assistant-ui/heroui-workspace-sidebar.tsx", import.meta.url),
  "utf8",
);
const layoutSource = await readFile(new URL("../app/layout.tsx", import.meta.url), "utf8");
const themeToggleSource = await readFile(
  new URL("../components/assistant-ui/workspace-theme-toggle.tsx", import.meta.url),
  "utf8",
);
const wordmarkSource = await readFile(
  new URL("../components/assistant-ui/cocola-wordmark.tsx", import.meta.url),
  "utf8",
);
const demoStylesSource = await readFile(
  new URL("../app/cocola-web-demo.css", import.meta.url),
  "utf8",
);
const railSource = await readFile(
  new URL("../components/assistant-ui/rail.tsx", import.meta.url),
  "utf8",
);
const markdownSource = await readFile(
  new URL("../components/assistant-ui/markdown-text.tsx", import.meta.url),
  "utf8",
);
const globalStylesSource = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
test("conversation messages use the Cocola compatibility skeleton without replacing Cocola parts", () => {
  assert.match(threadSource, /from "@cocola\/ui-compat\/chat-message"/);
  assert.match(threadSource, /<ChatMessage\.User/);
  assert.match(threadSource, /<ChatMessage\.Assistant/);
  assert.match(threadSource, /<MessagePrimitive\.Parts components=\{ASSISTANT_PART_COMPONENTS\}/);
  assert.match(threadSource, /"run-summary": RunSummaryPart/);
  assert.match(threadSource, /"structured-result": StructuredResultCardPart/);
});

test("assistant identity remains model-aware and the thread uses the bounded product layout", () => {
  assert.match(threadSource, /\["--thread-max-width" as string\]: "58rem"/);
  assert.match(threadSource, /<ModelIcon icon=\{icon\}/);
  assert.match(threadSource, /metadata\?\.model_label \|\| selectedModel\?\.label/);
  assert.match(threadSource, /metadata\?\.interaction_mode === "plan"/);
  assert.match(threadSource, /<div className="h-6 w-full shrink-0" aria-hidden="true" \/>/);
  assert.match(threadSource, /cocola-web-prompt-starter/);
  assert.match(threadSource, /cocola-web-prompt-starter group !h-10 !min-h-10/);
  assert.match(threadSource, /cocola-web-prompt-starter-icon flex size-7/);
  assert.doesNotMatch(threadSource, /cocola-web-prompt-starter group !min-h-11/);
  assert.match(threadSource, /cocola-chat-user-bubble/);
  assert.match(threadSource, /cocola-chat-user-bubble max-w-\[min\(72%,42rem\)\]/);
  assert.match(
    threadSource,
    /ThreadPrimitive\.Viewport[\s\S]*?<ActiveExecutionDock \/>[\s\S]*?<ChatConversation\.Content/,
  );
  assert.match(threadSource, /pointer-events-none sticky top-12/);
  assert.doesNotMatch(threadSource, /pointer-events-none sticky top-12[^"\n]*bg-gradient-to-b/);
  assert.match(threadSource, /max-w-\[var\(--thread-max-width\)\] pl-\[2\.375rem\]/);
  assert.match(
    railSource,
    /rounded-2xl border border-border\/80 bg-surface px-4 py-3 shadow-surface/,
  );
  assert.match(railSource, /grid min-w-0 grid-cols-\[1rem_minmax\(0,1fr\)\] items-center gap-2/);
  assert.match(railSource, /text-accent flex h-7 items-center justify-end/);
  assert.match(railSource, /flex h-5 items-center justify-end/);
  assert.doesNotMatch(railSource, /bg-accent-soft text-accent flex size-7/);
  assert.match(railSource, /border-2 border-foreground\/25/);
  assert.match(railSource, /overscroll-contain pr-1 pt-1/);
});

test("assistant answers inherit Inter and use the compact product text rhythm", () => {
  assert.match(
    markdownSource,
    /aui-answer-markdown[^"\n]*font-sans[^"\n]*text-\[15px\][^"\n]*leading-6/,
  );
  assert.match(markdownSource, /\[&_p\]:my-2 /);
  assert.match(markdownSource, /\[&_li\]:my-1 /);
  assert.doesNotMatch(markdownSource, /aui-answer-markdown[^"\n]*leading-7/);
});

test("shell code uses a compact macOS terminal treatment", () => {
  assert.match(markdownSource, /normalizeLanguage\(language\) === "shell"/);
  assert.match(markdownSource, /bg-\[#ff5f57\]/);
  assert.match(markdownSource, /bg-\[#febc2e\]/);
  assert.match(markdownSource, /bg-\[#28c840\]/);
  assert.match(markdownSource, /grid h-7 grid-cols-\[1fr_auto_1fr\]/);
  assert.match(markdownSource, /bg-\[#0d0f12\][^"\n]*text-\[12px\][^"\n]*leading-5/);
  assert.doesNotMatch(markdownSource, /rounded-t-xl[^"\n]*bg-surface-secondary\/50/);
});

test("the empty thread preserves the HeroUI demo welcome composition", () => {
  assert.match(
    threadSource,
    /mx-auto flex h-\[calc\(100svh-3\.5rem\)\] min-h-0 w-full max-w-5xl flex-col px-4 py-4 sm:px-6 lg:px-8/,
  );
  assert.match(threadSource, /flex flex-1 flex-col items-center justify-center py-10 text-center/);
  assert.match(threadSource, /<CocolaLogo className="h-28 w-28 shrink-0 sm:h-32 sm:w-32" \/>/);
  assert.match(
    threadSource,
    /<CocolaWordmark className="cocola-wordmark -my-4 h-32 w-auto max-w-\[min\(90vw,460px\)\] sm:h-36" \/>/,
  );
  assert.match(threadSource, /<CocolaTagline \/>/);
  assert.match(wordmarkSource, /const \[animationReady, setAnimationReady\] = useState\(false\)/);
  assert.match(wordmarkSource, /style=\{\{ opacity: animationReady \? 1 : 0 \}\}/);
  assert.match(wordmarkSource, /instance\.reset\?\.\(\);[\s\S]*?setAnimationReady\(true\)/);
  assert.doesNotMatch(threadSource, /src="\/cocola-wordmark\.svg"/);
  assert.doesNotMatch(threadSource, /Auto · choose for me/);
  assert.match(threadSource, /mt-7 w-full max-w-3xl/);
  assert.match(threadSource, /mx-auto flex w-full max-w-3xl flex-wrap justify-center gap-2/);
  assert.match(threadSource, /\{visiblePromptStarters\.map\(\(starter\) => \{/);
  assert.match(threadSource, /"cocola-web-composer w-full"/);
  assert.match(threadSource, /hasComposerHeader && "cocola-web-composer--with-header"/);
  assert.match(
    threadSource,
    /<ComposerPrimitive\.AddAttachment[\s\S]*?className="cocola-web-composer-action inline-flex size-9/,
  );
  assert.doesNotMatch(
    threadSource,
    /<ComposerPrimitive\.AddAttachment asChild>[\s\S]*?<PromptInput\.Action/,
  );
  assert.match(threadSource, /<div className="relative min-w-0 text-left">/);
  assert.match(threadSource, /prompt-input__textarea[^"\n]*text-left[^"\n]*text-\[15px\]/);
  assert.match(
    threadSource,
    /pointer-events-none absolute inset-0[^"\n]*text-left[^"\n]*text-\[15px\]/,
  );
  assert.match(
    threadSource,
    /iconClassName: "bg-emerald-500\/10 text-emerald-600 dark:text-emerald-300"/,
  );
  assert.match(threadSource, /!border-border/);
  assert.match(threadSource, /selectedAgent\?\.name \?\? "None"/);
  assert.match(threadSource, /<Dropdown\.Item id="none" textValue="None">/);
  assert.match(threadSource, /avatarKey=\{agent\.avatar_key\}/);
  assert.doesNotMatch(threadSource, /PlanModeContextStrip/);
  assert.match(threadSource, /<PlanModeIndicator \/>/);
  assert.match(
    threadSource,
    /cocola-web-plan-indicator inline-flex h-9 shrink-0 items-center gap-2 rounded-xl px-2\.5 text-xs font-medium/,
  );
  assert.match(threadSource, /variant="ghost"[\s\S]*?setInteractionMode\("execute"\)/);
  assert.doesNotMatch(threadSource, /cocola-web-plan-indicator h-9 shrink-0 rounded-full/);
  assert.match(
    demoStylesSource,
    /\.cocola-user-ui \.cocola-web-plan-indicator \{[\s\S]*?background: transparent !important;/,
  );
  assert.match(
    demoStylesSource,
    /\.cocola-user-ui \.cocola-web-plan-indicator:hover,[\s\S]*?transform: none;/,
  );
  assert.match(threadSource, /PLAN_MODE_COPY\.initialPlaceholder/);
  assert.match(demoStylesSource, /\.prompt-input__shell \{[\s\S]*?var\(--border\) 88%/);
  assert.match(
    demoStylesSource,
    /\.cocola-user-ui \.cocola-web-composer \.prompt-input__textarea \{[\s\S]*?min-height: 5\.75rem;[\s\S]*?margin-bottom: 3\.75rem;/,
  );
  assert.match(
    demoStylesSource,
    /\.cocola-user-ui \.cocola-web-composer--with-header \.prompt-input__textarea \{[\s\S]*?min-height: 3\.5rem;/,
  );
  assert.match(
    demoStylesSource,
    /\.cocola-user-ui \.cocola-web-composer \.prompt-input__toolbar \{[\s\S]*?inset-inline: 0\.75rem;[\s\S]*?bottom: 0\.75rem;/,
  );
});

test("the real workspace uses the approved HeroUI demo shell treatment", () => {
  assert.match(sidebarSource, /cocola-web-new-chat/);
  assert.match(sidebarSource, /cocola-sidebar-tab/);
  assert.match(sidebarSource, /adminOnly: true,[\s\S]*?href: "\/admin"/);
  assert.match(sidebarSource, /const isAdmin = session\?\.user\?\.role === "admin"/);
  assert.match(
    sidebarSource,
    /const visibleWorkspaceNavigation = WORKSPACE_NAVIGATION\.filter\([\s\S]*?!item\.adminOnly \|\| isAdmin/,
  );
  assert.match(sidebarSource, /\{visibleWorkspaceNavigation\.map\(\(item\) => \(/);
  assert.match(sidebarSource, /<Sidebar\.MenuActions className="cocola-sidebar-create-actions">/);
  assert.match(sidebarSource, /className="size-7 min-h-7 min-w-7 p-0"/);
  assert.match(sidebarSource, /cocola-sidebar-rename-input[^"]*h-6[^"]*py-0[^"]*leading-5/);
  assert.doesNotMatch(sidebarSource, /aria-label="Conversation title"\s+className="[^"]*py-1/);
  assert.match(
    sidebarSource,
    /<Dropdown\.Item id="rename"[\s\S]*?<Pencil[\s\S]*?<Dropdown\.SubmenuTrigger>[\s\S]*?Move to folder/,
  );
  assert.match(
    sidebarSource,
    /<Dropdown\.Popover placement="right top">[\s\S]*?id="move-root" textValue="No folder"[\s\S]*?folders\.map/,
  );
  assert.match(sidebarSource, /!conversation\.folder_id[\s\S]*?<Check/);
  assert.match(sidebarSource, /conversation\.folder_id === folder\.id[\s\S]*?<Check/);
  assert.match(
    sidebarSource,
    /<Dropdown\.Item id="delete" textValue="Delete" variant="danger">[\s\S]*?<TrashBin/,
  );
  assert.doesNotMatch(sidebarSource, />Move to \{folder\.name\}</);
  assert.match(
    demoStylesSource,
    /\.cocola-user-ui \.cocola-sidebar-create-actions \{[\s\S]*?display: flex;[\s\S]*?opacity: 0;/,
  );
  assert.match(sidebarSource, /<CocolaCoreLogo className="size-10 shrink-0" \/>/);
  assert.doesNotMatch(sidebarSource, /linear-gradient\(135deg,#2563eb,#7c3aed\)/);
  assert.doesNotMatch(layoutSource, /className=\{`dark /);
  assert.match(themeToggleSource, /cocola:color-mode/);
  assert.match(themeToggleSource, /Switch to \$\{nextMode\} mode/);
});

test("formal Cocola has no production HeroUI Pro dependency or worktree proxy", () => {
  assert.equal(webPackage.dependencies["@heroui-pro/react"], undefined);
  assert.equal(webPackage.dependencies["@cocola/ui-compat"], "workspace:*");
  assert.doesNotMatch(globalStylesSource, /@heroui-pro\/react/);
  assert.match(globalStylesSource, /@cocola\/ui-compat\/styles\.css/);
  assert.doesNotMatch(nextConfigSource, /motion\/react|HeroUI Pro/);
  assert.equal(webPackage.scripts["dev:frontend-only"], undefined);
  assert.equal(rootPackage.scripts["dev:web:frontend-only"], undefined);
  assert.doesNotMatch(nextConfigSource, /COCOLA_WEB_BACKEND_ORIGIN|backendOrigin/);
  assert.doesNotMatch(serverSource, /FRONTEND_ONLY_BACKEND|tunnelToWebBackend/);
});
