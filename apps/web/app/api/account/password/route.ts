import { mutateAccount } from "@/lib/account-proxy";

export async function POST(req: Request) {
  return mutateAccount(req, "/me/account/password", "POST");
}
