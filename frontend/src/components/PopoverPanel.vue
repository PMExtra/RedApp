<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
const props = withDefaults(defineProps<{ as?: "ul" | "div"; topbar?: boolean }>(), { as: "div" });
const panel = ref<HTMLElement>();
const shift = ref(0);
const style = computed(() => props.topbar ? { transform: `translateX(${shift.value}px)` } : undefined);
let observer: ResizeObserver | undefined;
function position() {
  if (!props.topbar || !panel.value) return;
  const rect = panel.value.getBoundingClientRect();
  const left = rect.left - shift.value;
  const viewport = document.documentElement.clientWidth || window.innerWidth;
  const gutter = 16;
  shift.value = Math.max(gutter, Math.min(left, viewport - gutter - rect.width)) - left;
}
onMounted(() => {
  if (!props.topbar) return;
  position();
  window.addEventListener("resize", position);
  window.addEventListener("scroll", position, true);
  if (typeof ResizeObserver !== "undefined") {
    observer = new ResizeObserver(position);
    observer.observe(panel.value!);
    if (panel.value?.parentElement) observer.observe(panel.value.parentElement);
  }
});
onUnmounted(() => {
  observer?.disconnect();
  window.removeEventListener("resize", position);
  window.removeEventListener("scroll", position, true);
});
</script>
<template>
  <component :is="as" ref="panel" class="popover-panel" :class="{ 'topbar-menu': topbar }" :style="style"><slot /></component>
</template>
