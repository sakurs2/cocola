import { ConversationReadOnly } from "@/components/conversation-readonly";

export const dynamic = "force-dynamic";

export default function ConversationPage({ params }: { params: { id: string } }) {
  return <ConversationReadOnly conversationId={params.id} />;
}
