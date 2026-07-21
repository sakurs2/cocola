import { currentAccount, mutateAccount } from "@/lib/account-proxy";

export async function GET() {
  const account = await currentAccount();
  return account instanceof Response ? account : Response.json(account);
}

export async function PATCH(req: Request) {
  return mutateAccount(req, "/me/account", "PATCH");
}
