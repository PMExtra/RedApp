import { onMounted, onUnmounted, ref, useId } from "vue";
// Shared dismissal/ownership; each consumer retains its listbox or menu keyboard model.
const opened = new Set<() => void>();
export function usePopover() {
  const id = useId(),
    open = ref(false),
    root = ref<HTMLElement>(),
    trigger = ref<HTMLButtonElement>();
  function close(focus = false) {
    open.value = false;
    opened.delete(close);
    if (focus) trigger.value?.focus();
  }
  function show() {
    for (const dismiss of [...opened]) dismiss();
    open.value = true;
    opened.add(close);
  }
  function outside(event: PointerEvent) {
    if (!root.value?.contains(event.target as Node)) close();
  }
  function escape(event: KeyboardEvent) {
    if (open.value && event.key === "Escape") {
      event.preventDefault();
      close(true);
    }
  }
  onMounted(() => {
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
  });
  onUnmounted(() => {
    close();
    document.removeEventListener("pointerdown", outside);
    document.removeEventListener("keydown", escape);
  });
  return { id, open, root, trigger, close, show };
}
