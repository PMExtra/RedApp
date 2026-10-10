/** 32 lowercase hex characters, the format of client-generated IDs (transfers, request keys). */
export function randomId(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}
