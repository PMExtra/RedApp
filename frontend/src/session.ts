import { ref } from "vue";
import {
  api,
  ApiError,
  isCancellation,
  recheckCSRF,
  setCSRF,
  setUnauthorizedHandler,
} from "./api";
import type { Message } from "./i18n";
export const signedIn = ref(false),
  sessionChecked = ref(false),
  sessionBusy = ref(false);
export const sessionNotice = ref<Message>();
export const sessionError = ref<unknown>();
let ticket = 0;
let checkController: AbortController | undefined;
export function cancelSessionCheck() {
  ticket++;
  checkController?.abort();
  checkController = undefined;
  sessionBusy.value = false;
}
export function expireSession(
  message: Message = "Your session expired. Sign in again.",
) {
  cancelSessionCheck();
  signedIn.value = false;
  sessionChecked.value = true;
  setCSRF("");
  sessionNotice.value = message;
}
setUnauthorizedHandler(() => {
  if (signedIn.value) expireSession();
});
export async function checkSession() {
  if (sessionBusy.value) return;
  const attempt = ++ticket;
  const request = new AbortController();
  checkController = request;
  sessionBusy.value = true;
  sessionError.value = undefined;
  try {
    const data = await api<{ csrf: string }>(
      "session",
      undefined,
      request.signal,
    );
    if (typeof data.csrf !== "string" || !data.csrf)
      throw new ApiError({ code: "INVALID_RESPONSE" }, 200);
    if (attempt === ticket) {
      recheckCSRF(data.csrf);
      signedIn.value = true;
    }
  } catch (reason) {
    if (attempt === ticket) {
      if (reason instanceof ApiError && reason.status === 401)
        signedIn.value = false;
      else if (!isCancellation(reason)) sessionError.value = reason;
    }
  } finally {
    if (attempt === ticket) {
      sessionChecked.value = true;
      sessionBusy.value = false;
      checkController = undefined;
    }
  }
}
export async function loginSession(password: string, signal?: AbortSignal) {
  cancelSessionCheck();
  const data = await api<{ csrf: string }>("login", { password }, signal);
  if (signal?.aborted) return;
  ticket++;
  setCSRF(data.csrf);
  signedIn.value = true;
  sessionChecked.value = true;
  sessionNotice.value = undefined;
  sessionError.value = undefined;
}
export async function logoutSession() {
  await api("logout", {});
  expireSession("Signed out");
}
