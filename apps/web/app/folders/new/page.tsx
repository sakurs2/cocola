"use client";

import { ArrowLeft, FolderPlus, Loader2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { useCocola } from "@/app/runtime-provider";

export default function NewFolderPage() {
  const router = useRouter();
  const { createFolder } = useCocola();
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const nextName = name.trim();
    if (!nextName || busy) return;
    setBusy(true);
    setError("");
    try {
      const folder = await createFolder(nextName);
      router.push(`/folders/${encodeURIComponent(folder.id)}`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not create folder");
      setBusy(false);
    }
  };

  return (
    <div className="h-full overflow-y-auto px-5 py-8 sm:px-8 lg:px-12">
      <main className="mx-auto w-full max-w-xl pb-16">
        <Link
          href="/folders"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          Folders
        </Link>

        <div className="mt-7 flex items-center gap-4">
          <div className="grid size-11 shrink-0 place-items-center rounded-2xl bg-amber-500/10 text-amber-600">
            <FolderPlus className="size-5" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">Create a folder</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Give related conversations a shared home.
            </p>
          </div>
        </div>

        <form
          onSubmit={submit}
          className="mt-8 rounded-2xl border border-border bg-card p-5 sm:p-6"
        >
          <label className="block">
            <span className="text-sm font-medium">Folder name</span>
            <input
              autoFocus
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="e.g. Product research"
              className="mt-2 h-11 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/15"
            />
          </label>

          {error ? (
            <p role="alert" className="mt-3 text-sm text-destructive">
              {error}
            </p>
          ) : null}

          <div className="mt-6 flex items-center justify-end gap-2">
            <Link
              href="/folders"
              className="inline-flex h-10 items-center rounded-xl px-4 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              Cancel
            </Link>
            <button
              type="submit"
              disabled={busy || !name.trim()}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busy ? <Loader2 className="size-4 animate-spin" /> : null}
              Create folder
            </button>
          </div>
        </form>
      </main>
    </div>
  );
}
