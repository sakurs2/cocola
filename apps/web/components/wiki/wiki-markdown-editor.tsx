"use client";

import Highlight from "@tiptap/extension-highlight";
import Image from "@tiptap/extension-image";
import Placeholder from "@tiptap/extension-placeholder";
import { TableKit } from "@tiptap/extension-table";
import TaskItem from "@tiptap/extension-task-item";
import TaskList from "@tiptap/extension-task-list";
import { Markdown } from "@tiptap/markdown";
import { EditorContent, useEditor, useEditorState } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import {
  Bold,
  Code2,
  Heading1,
  Heading2,
  Heading3,
  Highlighter,
  Image as ImageIcon,
  Italic,
  Link2,
  List,
  ListChecks,
  ListOrdered,
  Minus,
  Quote,
  Redo2,
  RemoveFormatting,
  SquareCode,
  Strikethrough,
  Underline,
  Undo2,
} from "lucide-react";
import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from "react";
import type { Editor } from "@tiptap/core";
import { TextInputDialog } from "@/components/ui/action-dialog";
import { cn } from "@/lib/utils";
import { isLosslessMarkdownRoundTrip } from "@/lib/wiki-markdown-compat.mjs";
import styles from "./wiki-markdown-editor.module.css";

const editorExtensions = [
  StarterKit.configure({
    heading: { levels: [1, 2, 3] },
    link: {
      autolink: true,
      defaultProtocol: "https",
      openOnClick: false,
    },
  }),
  Highlight,
  TableKit,
  TaskList,
  TaskItem.configure({ nested: true }),
  Image.configure({ allowBase64: false }),
  Placeholder.configure({ placeholder: "Start writing your page…" }),
  Markdown.configure({
    markedOptions: {
      breaks: false,
      gfm: true,
    },
  }),
];

export function WikiMarkdownEditor({
  value,
  readOnly = false,
  onChange,
}: {
  value: string;
  readOnly?: boolean;
  onChange: (markdown: string) => void;
}) {
  const appliedValue = useRef<string | null>(null);
  const onChangeRef = useRef(onChange);
  const [mode, setMode] = useState<"checking" | "rich" | "source">("checking");
  onChangeRef.current = onChange;

  const editor = useEditor({
    immediatelyRender: false,
    extensions: editorExtensions,
    content: "",
    contentType: "markdown",
    editable: !readOnly,
    editorProps: {
      attributes: {
        "aria-label": "Markdown page editor",
        class: styles.proseMirror ?? "",
        spellcheck: "true",
      },
    },
    onUpdate: ({ editor: currentEditor }) => {
      const markdown = currentEditor.getMarkdown();
      appliedValue.current = markdown;
      onChangeRef.current(markdown);
    },
  });

  useEffect(() => {
    if (!editor || appliedValue.current === value) return;
    if (mode === "source") {
      appliedValue.current = value;
      return;
    }
    editor.commands.setContent(value, {
      contentType: "markdown",
      emitUpdate: false,
    });
    appliedValue.current = value;
    if (mode === "checking") {
      setMode(isLosslessMarkdownRoundTrip(value, editor.getMarkdown()) ? "rich" : "source");
    }
  }, [editor, mode, value]);

  useEffect(() => {
    editor?.setEditable(!readOnly, false);
  }, [editor, readOnly]);

  if (mode === "source") {
    return (
      <div className={styles.editorShell}>
        <div className={styles.sourceWarning} role="status">
          This page uses Markdown syntax that the rich-text editor cannot safely preserve. Edit the
          source directly to keep all content intact.
        </div>
        <textarea
          aria-label="Markdown source editor"
          className={styles.sourceEditor}
          value={value}
          readOnly={readOnly}
          spellCheck
          onChange={(event) => onChangeRef.current(event.target.value)}
        />
      </div>
    );
  }

  return (
    <div className={styles.editorShell}>
      {mode === "rich" && editor ? (
        <>
          <EditorToolbar editor={editor} readOnly={readOnly} />
          <EditorContent editor={editor} className={styles.editorContent} />
        </>
      ) : (
        <div className={styles.editorChecking}>Checking Markdown compatibility…</div>
      )}
    </div>
  );
}

function EditorToolbar({ editor, readOnly }: { editor: Editor; readOnly: boolean }) {
  const [urlDialog, setURLDialog] = useState<{
    kind: "link" | "image";
    initialValue: string;
    error: string;
  } | null>(null);
  const state = useEditorState({
    editor,
    selector: ({ editor: currentEditor }) => ({
      blockquote: currentEditor.isActive("blockquote"),
      bold: currentEditor.isActive("bold"),
      bulletList: currentEditor.isActive("bulletList"),
      code: currentEditor.isActive("code"),
      codeBlock: currentEditor.isActive("codeBlock"),
      canRedo: currentEditor.can().redo(),
      canUndo: currentEditor.can().undo(),
      heading1: currentEditor.isActive("heading", { level: 1 }),
      heading2: currentEditor.isActive("heading", { level: 2 }),
      heading3: currentEditor.isActive("heading", { level: 3 }),
      highlight: currentEditor.isActive("highlight"),
      italic: currentEditor.isActive("italic"),
      link: currentEditor.isActive("link"),
      orderedList: currentEditor.isActive("orderedList"),
      strike: currentEditor.isActive("strike"),
      taskList: currentEditor.isActive("taskList"),
      underline: currentEditor.isActive("underline"),
    }),
  });

  const openLinkDialog = () => {
    setURLDialog({
      kind: "link",
      initialValue: String(editor.getAttributes("link").href ?? ""),
      error: "",
    });
  };

  const openImageDialog = () => {
    setURLDialog({ kind: "image", initialValue: "", error: "" });
  };

  const submitURL = (raw: string) => {
    if (!urlDialog) return;
    const image = urlDialog.kind === "image";
    const url = safeURL(raw, image);
    if (!url) {
      setURLDialog((current) =>
        current
          ? {
              ...current,
              error: image
                ? "Use an http(s) or site-relative image URL."
                : "Use an http(s), mailto, tel, or site-relative URL.",
            }
          : null,
      );
      return;
    }
    if (image) {
      editor.chain().focus().setImage({ src: url }).run();
    } else {
      editor.chain().focus().extendMarkRange("link").setLink({ href: url }).run();
    }
    setURLDialog(null);
  };

  return (
    <>
      <div className={styles.toolbar} role="toolbar" aria-label="Page formatting">
        <ToolbarGroup>
          <ToolbarButton
            label="Bold (Cmd+B)"
            active={state.bold}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleBold().run()}
          >
            <Bold />
          </ToolbarButton>
          <ToolbarButton
            label="Italic (Cmd+I)"
            active={state.italic}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleItalic().run()}
          >
            <Italic />
          </ToolbarButton>
          <ToolbarButton
            label="Underline (Cmd+U)"
            active={state.underline}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleUnderline().run()}
          >
            <Underline />
          </ToolbarButton>
          <ToolbarButton
            label="Strikethrough"
            active={state.strike}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleStrike().run()}
          >
            <Strikethrough />
          </ToolbarButton>
          <ToolbarButton
            label="Highlight"
            active={state.highlight}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleHighlight().run()}
          >
            <Highlighter />
          </ToolbarButton>
          <ToolbarButton
            label="Inline code"
            active={state.code}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleCode().run()}
          >
            <Code2 />
          </ToolbarButton>
        </ToolbarGroup>

        <ToolbarSeparator />

        <ToolbarGroup>
          <ToolbarButton
            label="Heading 1"
            active={state.heading1}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleHeading({ level: 1 }).run()}
          >
            <Heading1 />
          </ToolbarButton>
          <ToolbarButton
            label="Heading 2"
            active={state.heading2}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}
          >
            <Heading2 />
          </ToolbarButton>
          <ToolbarButton
            label="Heading 3"
            active={state.heading3}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleHeading({ level: 3 }).run()}
          >
            <Heading3 />
          </ToolbarButton>
        </ToolbarGroup>

        <ToolbarSeparator />

        <ToolbarGroup>
          <ToolbarButton
            label="Bullet list"
            active={state.bulletList}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleBulletList().run()}
          >
            <List />
          </ToolbarButton>
          <ToolbarButton
            label="Ordered list"
            active={state.orderedList}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleOrderedList().run()}
          >
            <ListOrdered />
          </ToolbarButton>
          <ToolbarButton
            label="Task list"
            active={state.taskList}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleTaskList().run()}
          >
            <ListChecks />
          </ToolbarButton>
          <ToolbarButton
            label="Blockquote"
            active={state.blockquote}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleBlockquote().run()}
          >
            <Quote />
          </ToolbarButton>
          <ToolbarButton
            label="Code block"
            active={state.codeBlock}
            disabled={readOnly}
            onClick={() => editor.chain().focus().toggleCodeBlock().run()}
          >
            <SquareCode />
          </ToolbarButton>
        </ToolbarGroup>

        <ToolbarSeparator />

        <ToolbarGroup>
          <ToolbarButton
            label="Insert or edit link"
            active={state.link}
            disabled={readOnly}
            onClick={openLinkDialog}
          >
            <Link2 />
          </ToolbarButton>
          <ToolbarButton label="Insert image by URL" disabled={readOnly} onClick={openImageDialog}>
            <ImageIcon />
          </ToolbarButton>
          <ToolbarButton
            label="Horizontal rule"
            disabled={readOnly}
            onClick={() => editor.chain().focus().setHorizontalRule().run()}
          >
            <Minus />
          </ToolbarButton>
          <ToolbarButton
            label="Clear formatting"
            disabled={readOnly}
            onClick={() => editor.chain().focus().unsetAllMarks().clearNodes().run()}
          >
            <RemoveFormatting />
          </ToolbarButton>
        </ToolbarGroup>

        <ToolbarSeparator />

        <ToolbarGroup>
          <ToolbarButton
            label="Undo (Cmd+Z)"
            disabled={readOnly || !state.canUndo}
            onClick={() => editor.chain().focus().undo().run()}
          >
            <Undo2 />
          </ToolbarButton>
          <ToolbarButton
            label="Redo (Cmd+Shift+Z)"
            disabled={readOnly || !state.canRedo}
            onClick={() => editor.chain().focus().redo().run()}
          >
            <Redo2 />
          </ToolbarButton>
        </ToolbarGroup>
      </div>

      <TextInputDialog
        open={urlDialog !== null}
        title={urlDialog?.kind === "image" ? "Insert image" : "Add link"}
        description={
          urlDialog?.kind === "image"
            ? "Add an image using a secure remote or site-relative URL."
            : "Link the selected text to a web page, email address, phone number, or Wiki URL."
        }
        label={urlDialog?.kind === "image" ? "Image URL" : "Link URL"}
        initialValue={urlDialog?.initialValue ?? ""}
        placeholder={
          urlDialog?.kind === "image" ? "https://example.com/image.png" : "https://example.com"
        }
        submitLabel={urlDialog?.kind === "image" ? "Insert image" : "Apply link"}
        secondaryLabel={
          urlDialog?.kind === "link" && urlDialog.initialValue ? "Remove link" : undefined
        }
        error={urlDialog?.error}
        icon={urlDialog?.kind === "image" ? ImageIcon : Link2}
        inputMode="url"
        onOpenChange={(open) => {
          if (!open) setURLDialog(null);
        }}
        onSubmit={submitURL}
        onSecondary={() => {
          editor.chain().focus().extendMarkRange("link").unsetLink().run();
          setURLDialog(null);
        }}
      />
    </>
  );
}

function ToolbarGroup({ children }: { children: ReactNode }) {
  return <div className={styles.toolbarGroup}>{children}</div>;
}

function ToolbarSeparator() {
  return <span className={styles.toolbarSeparator} aria-hidden="true" />;
}

function ToolbarButton({
  label,
  active,
  disabled,
  onClick,
  children,
}: {
  label: string;
  active?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  const keepSelection = (event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
  };

  return (
    <button
      type="button"
      className={cn(styles.toolbarButton, active && styles.toolbarButtonActive)}
      title={label}
      aria-label={label}
      aria-pressed={active === undefined ? undefined : active}
      disabled={disabled}
      onMouseDown={keepSelection}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

function safeURL(raw: string, image: boolean) {
  const value = raw.trim();
  if (!value) return "";
  if (value.startsWith("/") && !value.startsWith("//")) return value;

  const scheme = value.match(/^([a-z][a-z\d+.-]*):/i)?.[1]?.toLowerCase();
  if (scheme) {
    const allowed = image
      ? scheme === "http" || scheme === "https"
      : ["http", "https", "mailto", "tel"].includes(scheme);
    return allowed ? value : "";
  }
  if (value.startsWith("//")) return "";
  return `https://${value}`;
}
