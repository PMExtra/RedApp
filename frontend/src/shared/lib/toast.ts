import { ref } from "vue";
import { describeError } from "@/shared/api";

export type ToastTone = "info" | "success" | "warning" | "error";

export interface ToastInput {
  title: string;
  description?: string | null;
  tone?: ToastTone;
  /** Shown with a copy button so users can report failures. */
  requestId?: string | null;
  /** Milliseconds; errors stay longer by default. */
  duration?: number;
}

export interface Toast extends Required<Omit<ToastInput, "description" | "requestId">> {
  id: number;
  description: string | null;
  requestId: string | null;
}

const toasts = ref<Toast[]>([]);
let nextId = 1;

/** Shows a notification; rendered by `Toaster`, which each app shell mounts once. */
export function toast(input: ToastInput): number {
  const tone = input.tone ?? "info";
  const id = nextId++;
  toasts.value = [
    ...toasts.value.slice(-4),
    {
      id,
      title: input.title,
      description: input.description ?? null,
      tone,
      requestId: input.requestId ?? null,
      duration: input.duration ?? (tone === "error" ? 10_000 : 5_000),
    },
  ];
  return id;
}

export function dismissToast(id: number): void {
  toasts.value = toasts.value.filter((item) => item.id !== id);
}

/** Error toast with the localized message, server detail and request ID. */
export function notifyError(error: unknown): number {
  const description = describeError(error);
  return toast({
    tone: "error",
    title: description.message,
    description: description.detail,
    requestId: description.requestId,
  });
}

/** For Toaster only. */
export function activeToasts() {
  return toasts;
}

export function useToast() {
  return { toast, notifyError, dismissToast };
}
