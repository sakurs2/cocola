import { ConversationReadOnly } from "@/components/conversation-readonly";

export const dynamic = "force-dynamic";

export default async function ConversationPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ConversationReadOnly conversationId={id} />;
}
