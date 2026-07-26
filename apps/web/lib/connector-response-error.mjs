/**
 * Read the two error response shapes used by Cocola's Web proxies.
 *
 * @param {Response} response
 * @returns {Promise<string>}
 */
export async function connectorResponseError(response) {
  const payload = await response.json().catch(() => null);
  if (!payload || typeof payload !== "object") {
    return `Request failed (${response.status})`;
  }

  const error = payload.error;
  if (typeof error === "string" && error.trim()) {
    return error.trim();
  }
  if (
    error &&
    typeof error === "object" &&
    typeof error.message === "string" &&
    error.message.trim()
  ) {
    return error.message.trim();
  }
  return `Request failed (${response.status})`;
}
