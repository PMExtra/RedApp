<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { language, t } from "../i18n";
const props = defineProps<{ application: string; revision?: string }>();
const frame = ref<HTMLIFrameElement>();
// Administrator scripts run in an opaque-origin sandbox, so the document reports
// its own height. Only the current frame's numeric messages are accepted.
const minimumHeight = 120,
  maximumHeight = 50000;
const source = computed(
  () =>
    `/api/apps/${props.application}/instructions/document?lang=${language.value}`,
);
function received(event: MessageEvent) {
  const target = frame.value;
  if (!target || !event.source || event.source !== target.contentWindow) return;
  const data: unknown = event.data;
  if (
    typeof data !== "object" ||
    data === null ||
    (data as { type?: unknown }).type !== "redapp-instructions-height"
  )
    return;
  const height = (data as { height?: unknown }).height;
  if (typeof height !== "number" || !Number.isFinite(height)) return;
  target.style.height =
    Math.min(maximumHeight, Math.max(minimumHeight, Math.ceil(height) + 20)) +
    "px";
}
onMounted(() => window.addEventListener("message", received));
onUnmounted(() => window.removeEventListener("message", received));
</script>
<template>
  <iframe
    ref="frame"
    :key="`${source}:${revision}`"
    class="instructions-document"
    :src="source"
    :title="t('Usage instructions')"
    sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation"
    allow="clipboard-write"
  />
</template>
