export type WikiProxyMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

type NodeFetchRequestInit = RequestInit & {
  duplex?: "half";
};

export function wikiProxyRequestInit(
  request: Request,
  method: WikiProxyMethod,
  headers: Headers,
): NodeFetchRequestInit {
  const body = method !== "GET" && method !== "DELETE" ? request.body : null;
  return {
    method,
    cache: "no-store",
    headers,
    signal: request.signal,
    ...(body ? { body, duplex: "half" as const } : {}),
  };
}
