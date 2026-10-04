<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";
import { language, t } from "../i18n";
const props = defineProps<{ application: string; revision?: string }>();
const frame = ref<HTMLIFrameElement>();
let observer: ResizeObserver | undefined;
const source = computed(
  () =>
    `/api/apps/${props.application}/instructions/document?lang=${language.value}`,
);
function size() {
  const doc = frame.value?.contentDocument;
  if (frame.value && doc?.body)
    frame.value.style.height = Math.max(120, doc.body.scrollHeight + 20) + "px";
}
function loaded() {
  observer?.disconnect();
  const body = frame.value?.contentDocument?.body;
  if (body && typeof ResizeObserver !== "undefined") {
    observer = new ResizeObserver(size);
    observer.observe(body);
  }
  size();
}
onUnmounted(() => observer?.disconnect());
</script>
<template>
  <iframe
    ref="frame"
    :key="`${source}:${revision}`"
    class="instructions-document"
    :src="source"
    :title="t('Usage instructions')"
    @load="loaded"
  />
</template>
