import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  WIKI_COMPOSER_CONTENT_TYPE,
  createWikiComposerAttachment,
  isWikiComposerAttachment,
  layoutWikiComposerMentions,
  mergeWikiComposerReferences,
  wikiComposerMentionText,
  wikiPromptText,
  wikiReferencesFromAttachments,
} from "./wiki-composer-reference.ts";

const reference = {
  nodeId: "4fc9a5c1-5bc2-4981-a673-62663b9b7359",
  filename: "用户调查体验.md",
  logicalPath: "研究/用户调查体验.md",
};

const threadSource = readFileSync(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
  "utf8",
);

test("Wiki mention selection renders an inline marker backed by an attachment", () => {
  assert.match(threadSource, /Unstable_TriggerPopover\.Action/);
  assert.match(threadSource, /WIKI_MENTION_FORMATTER/);
  assert.match(threadSource, /ComposerWikiMentionOverlay/);
  assert.match(threadSource, /createWikiComposerAttachment/);
});

test("Wiki composer reference uses a structured attachment instead of visible directive text", () => {
  const attachment = createWikiComposerAttachment(reference);

  assert.equal(attachment.id, `wiki:${reference.nodeId}`);
  assert.equal(attachment.type, "document");
  assert.equal(attachment.contentType, WIKI_COMPOSER_CONTENT_TYPE);
  assert.equal(isWikiComposerAttachment(attachment), true);
  assert.deepEqual(wikiReferencesFromAttachments([attachment]), [reference]);
});

test("Wiki composer attachment IDs can distinguish repeated inline mentions", () => {
  const first = createWikiComposerAttachment(reference, "first");
  const second = createWikiComposerAttachment(reference, "second");

  assert.notEqual(first.id, second.id);
  assert.equal(isWikiComposerAttachment(first), true);
  assert.equal(isWikiComposerAttachment(second), true);
});

test("Wiki composer mention layout preserves text and mention positions", () => {
  const secondReference = {
    nodeId: "second",
    filename: "研究计划.md",
    logicalPath: "研究/研究计划.md",
  };
  const text = `请比较 ${wikiComposerMentionText(reference.filename)} 和 ${wikiComposerMentionText(
    secondReference.filename,
  )} 的结论`;
  const layout = layoutWikiComposerMentions(text, [reference, secondReference]);

  assert.equal(layout.segments.map((segment) => segment.text).join(""), text);
  assert.deepEqual(layout.matchedReferenceIndexes, [0, 1]);
  assert.deepEqual(
    layout.segments.filter((segment) => segment.reference).map((segment) => segment.reference),
    [reference, secondReference],
  );
});

test("Wiki composer mention layout reports deleted references as unmatched", () => {
  const layout = layoutWikiComposerMentions("普通文本", [reference]);

  assert.deepEqual(layout.segments, [{ text: "普通文本" }]);
  assert.deepEqual(layout.matchedReferenceIndexes, []);
});

test("Wiki composer parser ignores regular and malformed attachments", () => {
  assert.deepEqual(
    wikiReferencesFromAttachments([
      {
        id: "regular",
        name: "notes.txt",
        contentType: "text/plain",
        content: [{ type: "text", text: "hello" }],
      },
      {
        id: "wiki:broken",
        name: "broken.md",
        contentType: WIKI_COMPOSER_CONTENT_TYPE,
        content: [{ type: "text", text: "not json" }],
      },
    ]),
    [],
  );
});

test("Wiki composer references are deduplicated while preserving richer attachment metadata", () => {
  assert.deepEqual(
    mergeWikiComposerReferences(
      [reference],
      [
        {
          nodeId: reference.nodeId,
          filename: "legacy.md",
          logicalPath: "legacy.md",
        },
        {
          nodeId: "second",
          filename: " second.md ",
          logicalPath: "",
        },
      ],
    ),
    [
      reference,
      {
        nodeId: "second",
        filename: "second.md",
        logicalPath: "second.md",
      },
    ],
  );
});

test("Wiki-only messages receive a minimal non-empty prompt", () => {
  assert.equal(wikiPromptText("", [reference]), "Use the referenced Wiki file.");
  assert.equal(
    wikiPromptText("", [reference, { ...reference, nodeId: "second" }]),
    "Use the referenced Wiki files.",
  );
  assert.equal(wikiPromptText("总结这份材料", [reference]), "总结这份材料");
  assert.equal(wikiPromptText("   ", []), "   ");
});
