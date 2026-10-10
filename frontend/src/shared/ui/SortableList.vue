<script setup lang="ts" generic="T">
import { nextTick, ref, useId } from "vue";
import { GripVertical } from "@lucide/vue";
import { useI18n } from "vue-i18n";

/**
 * Reorderable list. Each row has a drag handle that also works from the
 * keyboard (Arrow Up/Down, Home/End); moves are announced to screen readers.
 * Pointer drags can be cancelled with Escape.
 */
const items = defineModel<T[]>({ required: true });
const props = defineProps<{
  itemKey: (item: T) => string;
  itemLabel: (item: T) => string;
  disabled?: boolean;
}>();
defineSlots<{ item: (props: { item: T; index: number }) => unknown }>();
const { t } = useI18n();
const instructionsId = useId();
const announcement = ref("");
const list = ref<HTMLElement>();
const dragging = ref<string | null>(null);
let snapshot: T[] | null = null;

function move(from: number, to: number, focus = true) {
  const next = [...items.value];
  const [moved] = next.splice(from, 1);
  if (moved === undefined || to < 0 || to >= items.value.length || from === to) return;
  next.splice(to, 0, moved);
  items.value = next;
  announcement.value = t("ui.sortable.moved", {
    label: props.itemLabel(moved),
    position: to + 1,
    total: next.length,
  });
  if (focus) {
    const key = props.itemKey(moved);
    void nextTick(() =>
      list.value
        ?.querySelector<HTMLElement>(`[data-sortable-handle="${CSS.escape(key)}"]`)
        ?.focus(),
    );
  }
}

function onKeydown(event: KeyboardEvent, index: number) {
  if (props.disabled) return;
  const last = items.value.length - 1;
  const target =
    event.key === "ArrowUp"
      ? index - 1
      : event.key === "ArrowDown"
        ? index + 1
        : event.key === "Home"
          ? 0
          : event.key === "End"
            ? last
            : null;
  if (target === null) {
    if (event.key === "Escape" && snapshot) cancelDrag();
    return;
  }
  event.preventDefault();
  move(index, Math.min(last, Math.max(0, target)));
}

function onPointerDown(event: PointerEvent, item: T) {
  if (props.disabled || event.button !== 0) return;
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  dragging.value = props.itemKey(item);
  snapshot = [...items.value];
}

function onPointerMove(event: PointerEvent) {
  if (dragging.value === null || !list.value) return;
  const rows = [...list.value.querySelectorAll<HTMLElement>("[data-sortable-row]")];
  const from = items.value.findIndex((item) => props.itemKey(item) === dragging.value);
  let to = rows.findIndex((row) => {
    const rect = row.getBoundingClientRect();
    return event.clientY < rect.top + rect.height / 2;
  });
  if (to === -1) to = rows.length - 1;
  else if (to > from) to -= 1;
  if (from !== -1 && to !== from) move(from, to, false);
}

function endDrag() {
  dragging.value = null;
  snapshot = null;
}

function cancelDrag() {
  if (snapshot) items.value = snapshot;
  endDrag();
}
</script>

<template>
  <div>
    <p :id="instructionsId" class="sr-only">{{ t("ui.sortable.instructions") }}</p>
    <ul ref="list" class="flex flex-col gap-2">
      <li
        v-for="(item, index) in items"
        :key="itemKey(item)"
        data-sortable-row
        class="flex items-center gap-2 rounded-lg border border-border bg-surface p-2"
        :class="dragging === itemKey(item) && 'border-primary shadow-md'"
      >
        <button
          type="button"
          :data-sortable-handle="itemKey(item)"
          :disabled="disabled"
          :aria-label="t('ui.sortable.handle', { label: itemLabel(item) })"
          :aria-describedby="instructionsId"
          class="cursor-grab touch-none rounded-md p-1 text-muted hover:bg-surface-hover hover:text-fg focus-ring disabled:cursor-not-allowed disabled:opacity-50"
          @keydown="onKeydown($event, index)"
          @pointerdown="onPointerDown($event, item)"
          @pointermove="onPointerMove"
          @pointerup="endDrag"
          @pointercancel="cancelDrag"
        >
          <GripVertical class="size-4" aria-hidden="true" />
        </button>
        <div class="min-w-0 flex-1"><slot name="item" :item="item" :index="index" /></div>
      </li>
    </ul>
    <p class="sr-only" aria-live="polite">{{ announcement }}</p>
  </div>
</template>
