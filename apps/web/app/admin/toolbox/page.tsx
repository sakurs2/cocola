import { ToolboxClient, type ToolboxToolId } from "./toolbox-client";

export default function AdminToolboxPage({
  searchParams,
}: {
  searchParams?: { tool?: string | string[] };
}) {
  const requested = Array.isArray(searchParams?.tool) ? searchParams?.tool[0] : searchParams?.tool;
  const initialTool: ToolboxToolId | null =
    requested === "system-prompt" || requested === "memory" ? requested : null;
  return <ToolboxClient initialTool={initialTool} />;
}
