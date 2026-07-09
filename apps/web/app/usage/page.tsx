import { redirect } from "next/navigation";

// Usage moved into the profile page. Keep this route as a permanent redirect
// so existing links / bookmarks don't 404.
export default function UsagePage() {
  redirect("/profile");
}
