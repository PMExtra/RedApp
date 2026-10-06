<script setup lang="ts" generic="T">
import { nextTick, onUnmounted, ref, useId, watch } from "vue";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{
  modelValue: T[];
  label: string;
  itemLabel: (item: T, index: number) => string;
  disabled?: boolean;
  rowClass?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [T[]] }>();
const root = ref<HTMLOListElement>();
const id = useId(),
  announcement = ref(""),
  dragging = ref<number | null>(null);
const keys = new WeakMap<object, number>();
let nextKey = 0;
function key(item: T, index: number) {
  if (item !== null && typeof item === "object") {
    if (!keys.has(item)) keys.set(item, nextKey++);
    return `item-${keys.get(item)}`;
  }
  // Editable primitive fields keep their input node while their value changes.
  return `position-${index}`;
}
let gesture:
  | {
      id: number;
      from: number;
      current: number;
      x: number;
      y: number;
      lastX: number;
      lastY: number;
      original: T[];
      order: T[];
    }
  | undefined;
let frame: number | undefined;
function focus(index: number) {
  void nextTick(() =>
    root.value
      ?.querySelectorAll<HTMLButtonElement>(".sort-handle")
      [index]?.focus(),
  );
}
function move(from: number, to: number) {
  if (
    props.disabled ||
    from === to ||
    from < 0 ||
    to < 0 ||
    from >= props.modelValue.length ||
    to >= props.modelValue.length
  )
    return false;
  const items = [...props.modelValue],
    [item] = items.splice(from, 1);
  items.splice(to, 0, item!);
  if (gesture) {
    gesture.current = to;
    gesture.order = items;
    dragging.value = to;
  }
  emit("update:modelValue", items);
  announcement.value = t("Moved {name} to position {position} of {count}.", {
    name: props.itemLabel(item!, to),
    position: to + 1,
    count: items.length,
  });
  return true;
}
function stop(restore = false) {
  const current = gesture;
  gesture = undefined;
  dragging.value = null;
  if (frame !== undefined) cancelAnimationFrame(frame);
  frame = undefined;
  if (!current) return;
  if (root.value?.hasPointerCapture?.(current.id))
    root.value.releasePointerCapture(current.id);
  if (restore) {
    emit("update:modelValue", current.original);
    announcement.value = t("Reordering cancelled.");
  }
  focus(restore ? current.from : current.current);
}
function hit(x: number, y: number) {
  const row = document
    .elementFromPoint(x, y)
    ?.closest<HTMLElement>("[data-sortable-row]");
  if (!gesture || !row || row.parentElement !== root.value) return;
  const target = Number(row.dataset.sortableRow),
    bounds = row.getBoundingClientRect();
  // Crossing the midpoint avoids oscillation when adjacent rows differ in height.
  if (
    bounds.height &&
    (target > gesture.current
      ? y < bounds.top + bounds.height / 2
      : y > bounds.top + bounds.height / 2)
  )
    return;
  move(gesture.current, target);
}
function scroll() {
  frame = undefined;
  if (!gesture || dragging.value === null) return;
  const y = gesture.lastY;
  const delta = y < 48 ? -12 : y > window.innerHeight - 48 ? 12 : 0;
  if (delta) {
    window.scrollBy(0, delta);
    hit(gesture.lastX, y);
  }
  frame = requestAnimationFrame(scroll);
}
function start(index: number, event: PointerEvent) {
  if (
    gesture ||
    props.disabled ||
    props.modelValue.length < 2 ||
    event.button !== 0 ||
    event.isPrimary === false
  )
    return;
  event.preventDefault();
  (event.currentTarget as HTMLElement).focus();
  gesture = {
    id: event.pointerId,
    from: index,
    current: index,
    x: event.clientX,
    y: event.clientY,
    lastX: event.clientX,
    lastY: event.clientY,
    original: [...props.modelValue],
    order: [...props.modelValue],
  };
  root.value?.setPointerCapture?.(event.pointerId);
}
function pointerMove(event: PointerEvent) {
  if (!gesture || event.pointerId !== gesture.id) return;
  gesture.lastX = event.clientX;
  gesture.lastY = event.clientY;
  if (
    dragging.value === null &&
    Math.hypot(event.clientX - gesture.x, event.clientY - gesture.y) < 4
  )
    return;
  event.preventDefault();
  dragging.value = gesture.current;
  hit(event.clientX, event.clientY);
  if (frame === undefined) frame = requestAnimationFrame(scroll);
}
function keyboard(index: number, event: KeyboardEvent) {
  const target = {
    ArrowUp: index - 1,
    ArrowDown: index + 1,
    Home: 0,
    End: props.modelValue.length - 1,
  }[event.key];
  if (target === undefined || props.disabled || gesture) return;
  event.preventDefault();
  if (move(index, target)) focus(target);
}
function cancelKey(event: KeyboardEvent) {
  if (gesture) {
    event.preventDefault();
    stop(true);
  }
}
watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) stop();
  },
);
watch(
  () => [...props.modelValue],
  (items) => {
    if (
      gesture &&
      (items.length !== gesture.order.length ||
        items.some((item, index) => item !== gesture!.order[index]))
    )
      stop();
  },
);
onUnmounted(() => {
  gesture = undefined;
  if (frame !== undefined) cancelAnimationFrame(frame);
});
</script>
<template>
  <div class="sortable-control">
    <p :id="`${id}-help`" class="sr-only">
      {{
        t(
          "Drag the handle to reorder. With the handle focused, use Up/Down or Home/End. Escape cancels a drag.",
        )
      }}
    </p>
    <ol
      ref="root"
      class="sortable-list"
      :aria-label="label"
      @pointermove="pointerMove"
      @pointerup="gesture?.id === $event.pointerId && stop()"
      @pointercancel="gesture?.id === $event.pointerId && stop(true)"
      @lostpointercapture="gesture?.id === $event.pointerId && stop(true)"
      @keydown.esc="cancelKey"
    >
      <li
        v-for="(item, index) in modelValue"
        :key="key(item, index)"
        :class="rowClass"
        :data-sortable-row="index"
        :data-dragging="dragging === index || undefined"
      >
        <button
          type="button"
          class="secondary icon-button sort-handle"
          :disabled="disabled || modelValue.length < 2"
          :title="t('Drag to reorder')"
          :aria-label="
            t('Reorder {name}, position {position} of {count}', {
              name: itemLabel(item, index),
              position: index + 1,
              count: modelValue.length,
            })
          "
          :aria-describedby="`${id}-help`"
          aria-keyshortcuts="ArrowUp ArrowDown Home End"
          @pointerdown="start(index, $event)"
          @keydown="keyboard(index, $event)"
          @dragstart.prevent
        >
          <Icon name="reorder" />
        </button>
        <div class="sortable-content">
          <slot :item="item" :index="index" />
        </div>
      </li>
    </ol>
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">
      {{ announcement }}
    </p>
  </div>
</template>
