import { describeError, isApiError, type Schema } from "@/shared/api";
import { translate } from "@/shared/i18n";

/** What the local application ID of a mirror points at. */
export type TargetStatus =
  | { kind: "invalid" }
  | { kind: "checking" }
  | { kind: "error"; message: string }
  | { kind: "new" }
  | { kind: "existing"; existing: Schema<"App"> }
  | { kind: "deleted"; existing: Schema<"App"> }
  | { kind: "mismatch"; existing: Schema<"App"> };

/** Hosted upstream applications are mirrored as HTTP cache applications. */
export function localProvider(remote: string): string {
  return remote === "hosted" ? "http-cache" : remote;
}

/** Default local IDs: the upstream's vendor and application IDs. */
export function splitKey(key: string): { vendor: string; app: string } {
  const [vendor = "", app = ""] = key.split("/");
  return { vendor, app };
}

/** One line for a failed mirror request. */
export function mirrorErrorText(error: unknown): string {
  if (isApiError(error, "ALREADY_EXISTS")) return translate("mirrors.mirror.alreadyMirrored");
  const { message, detail } = describeError(error);
  return detail ? `${message} ${detail}` : message;
}
