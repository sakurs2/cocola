import { ProfilePageContent } from "@/components/profile/profile-page-content";
import { isAuthFail, requireUser } from "@/lib/server-auth";
import { redirect } from "next/navigation";

export default async function ProfilePage() {
  const authResult = await requireUser();
  if (isAuthFail(authResult)) redirect("/login");
  return <ProfilePageContent user={authResult.user} />;
}
