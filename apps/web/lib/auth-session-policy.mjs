export const AUTH_SESSION_VERSION = 2;

export function authTokenUserID(token) {
  if (!token || token.authVersion !== AUTH_SESSION_VERSION) return null;
  const userID = typeof token.id === "string" ? token.id.trim() : "";
  return userID || null;
}
