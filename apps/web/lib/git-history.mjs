export function formatGitRelativeTime(value, now = Date.now()) {
  const timestamp = Date.parse(value ?? "");
  if (!Number.isFinite(timestamp)) return "Unknown time";

  const seconds = Math.max(0, Math.floor((now - timestamp) / 1000));
  if (seconds < 60) return seconds < 10 ? "just now" : `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  return new Date(timestamp).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: new Date(timestamp).getFullYear() === new Date(now).getFullYear() ? undefined : "numeric",
  });
}

export function gitCommitBadges(commit, snapshot) {
  const badges = [];
  if (commit.sha === snapshot?.head_sha) badges.push({ label: "HEAD", tone: "head" });
  if (commit.sha === snapshot?.base_sha) badges.push({ label: "BASE", tone: "base" });

  for (const ref of commit.refs ?? []) {
    const label = String(ref)
      .replace(/^HEAD -> /, "")
      .replace(/^refs\/heads\//, "")
      .replace(/^refs\/remotes\//, "")
      .replace(/^refs\/tags\//, "");
    if (!label || label === snapshot?.branch || badges.some((badge) => badge.label === label))
      continue;
    badges.push({ label, tone: ref.includes("tags/") ? "tag" : "ref" });
    if (badges.length >= 3) break;
  }
  return badges;
}

export function gitChangeCode(status) {
  const normalized = String(status ?? "")
    .replaceAll(".", "")
    .trim();
  if (normalized.includes("R")) return "R";
  if (normalized.includes("A") || status === "?") return "A";
  if (normalized.includes("D")) return "D";
  if (normalized.includes("U")) return "U";
  return normalized.slice(0, 1) || "M";
}

export function gitDiffGutterWidth(maxLineNumber) {
  const normalized = Number.isFinite(maxLineNumber) ? Math.max(0, Math.floor(maxLineNumber)) : 0;
  const digits = Math.max(1, String(normalized).length);
  return `${Math.max(4.5, digits + 1.5)}ch`;
}

export function gitCommitDescription(commit) {
  const subject = String(commit?.subject ?? "").trim();
  const body = String(commit?.body ?? "").trim();
  if (!body || body === subject) return "";
  if (subject && body.startsWith(`${subject}\n`)) return body.slice(subject.length).trim();
  return body;
}
