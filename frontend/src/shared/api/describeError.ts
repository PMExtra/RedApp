import { hasTranslation, translate } from "@/shared/i18n";
import { isApiError, type AnyErrorCode } from "./errors";

export interface ErrorDescription {
  /** Localized, user-facing summary. */
  message: string;
  /** Server detail worth showing (it names the invalid field), in English. */
  detail: string | null;
  requestId: string | null;
  retryable: boolean;
}

/** Codes whose server message carries specifics the localized text lacks. */
const DETAIL_CODES: ReadonlySet<AnyErrorCode> = new Set([
  "VALIDATION_FAILED",
  "INVALID_REQUEST",
  "PACKAGE_INVALID",
  "ICON_INVALID",
  "CATEGORY_AMBIGUOUS",
]);

/** Maps any thrown value to localized text for toasts and inline errors. */
export function describeError(error: unknown): ErrorDescription {
  if (!isApiError(error)) {
    return {
      message: translate("errors.generic"),
      detail: null,
      requestId: null,
      retryable: false,
    };
  }
  let message: string;
  if (error.code === "NETWORK_ERROR") message = translate("errors.network");
  else if (error.code === "UNEXPECTED_RESPONSE")
    message = translate("errors.unexpected", { status: error.status });
  else if (hasTranslation(`errors.codes.${error.code}`))
    message = translate(`errors.codes.${error.code}`);
  else message = error.message;
  const detail = DETAIL_CODES.has(error.code) && error.message ? error.message : null;
  return { message, detail, requestId: error.requestId, retryable: error.retryable };
}
