import { ToolboxClient, type ToolboxToolId } from "./toolbox-client";

export default async function AdminToolboxPage({
  searchParams,
}: {
  searchParams?: Promise<{ tool?: string | string[] }>;
}) {
  const resolvedSearchParams = await searchParams;
  const requested = Array.isArray(resolvedSearchParams?.tool)
    ? resolvedSearchParams.tool[0]
    : resolvedSearchParams?.tool;
  const initialTool: ToolboxToolId | null =
    requested === "system-prompt" || requested === "memory" ? requested : null;
  return <ToolboxClient initialTool={initialTool} />;
}
