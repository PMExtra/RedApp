import { shallowRef } from "vue";

export interface ConfirmOptions {
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** `danger` styles the confirm button for destructive actions. */
  tone?: "default" | "danger";
}

interface PendingConfirm {
  options: ConfirmOptions;
  resolve: (confirmed: boolean) => void;
}

const pending = shallowRef<PendingConfirm | null>(null);

/**
 * Asks the user to confirm; resolves `true` on confirm and `false` on cancel,
 * Escape or when another confirmation replaces it. Rendered by `ConfirmHost`,
 * which each app shell mounts once. Replaces `window.confirm`.
 */
export function confirm(options: ConfirmOptions): Promise<boolean> {
  pending.value?.resolve(false);
  return new Promise((resolve) => {
    pending.value = { options, resolve };
  });
}

export function useConfirm(): typeof confirm {
  return confirm;
}

/** For ConfirmHost only. */
export function pendingConfirm() {
  return pending;
}

export function settleConfirm(confirmed: boolean): void {
  const current = pending.value;
  pending.value = null;
  current?.resolve(confirmed);
}
