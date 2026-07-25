type MessageIdentity = {
  id: string;
  role: string;
};

export function reconcileWikiUserMessage<T extends MessageIdentity>(
  current: T[],
  persisted: T[],
  optimisticUserId: string,
  durableUserId: string,
): T[] {
  const durableUser = persisted.find(
    (message) => message.id === durableUserId && message.role === "user",
  );
  if (!durableUser) return current;

  let changed = false;
  const reconciled = current.map((message) => {
    if (message.id !== optimisticUserId && message.id !== durableUserId) return message;
    if (message === durableUser) return message;
    changed = true;
    return durableUser;
  });
  return changed ? reconciled : current;
}
