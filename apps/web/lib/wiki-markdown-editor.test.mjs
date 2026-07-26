import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { Editor } from "@tiptap/core";
import Highlight from "@tiptap/extension-highlight";
import Image from "@tiptap/extension-image";
import { TableKit } from "@tiptap/extension-table";
import TaskItem from "@tiptap/extension-task-item";
import TaskList from "@tiptap/extension-task-list";
import { Markdown } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";

const editorSource = await readFile(
  new URL("../components/wiki/wiki-markdown-editor.tsx", import.meta.url),
  "utf8",
);

function createEditor(content, options = {}) {
  return new Editor({
    extensions: [
      StarterKit.configure({ heading: { levels: [1, 2, 3] } }),
      Highlight,
      TableKit,
      TaskList,
      TaskItem.configure({ nested: true }),
      Image,
      Markdown.configure({ markedOptions: { gfm: true } }),
    ],
    content,
    contentType: "markdown",
    ...options,
  });
}

test("wiki editor round-trips supported Markdown formatting", () => {
  const editor = createEditor(`# Page

**bold** *italic* ++underline++ ==highlight== [link](https://example.com)

- [x] done
- [ ] todo

> quote

\`\`\`ts
const answer = 42;
\`\`\`

![diagram](https://example.com/diagram.png)

| Name | Value |
| --- | --- |
| answer | 42 |
`);

  try {
    const markdown = editor.getMarkdown();
    assert.match(markdown, /^# Page/m);
    assert.match(markdown, /\*\*bold\*\*/);
    assert.match(markdown, /\+\+underline\+\+/);
    assert.match(markdown, /==highlight==/);
    assert.match(markdown, /- \[x\] done/);
    assert.match(markdown, /^> quote/m);
    assert.match(markdown, /```ts\nconst answer = 42;\n```/);
    assert.match(markdown, /!\[diagram\]\(https:\/\/example\.com\/diagram\.png\)/);
    assert.match(markdown, /\| Name\s+\| Value\s+\|/);
    assert.match(markdown, /\| answer\s+\| 42\s+\|/);
  } finally {
    editor.destroy();
  }
});

test("changing editability does not emit a content update", () => {
  let updates = 0;
  const editor = createEditor("# Page\n", {
    editable: false,
    onUpdate: () => {
      updates += 1;
    },
  });

  try {
    editor.setEditable(true, false);
    assert.equal(updates, 0);
  } finally {
    editor.destroy();
  }
});

test("wiki editor suppresses updates when applying its editability state", () => {
  assert.match(editorSource, /setEditable\(!readOnly,\s*false\)/);
});
