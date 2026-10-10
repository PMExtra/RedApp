<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

/**
 * Usage instructions rendered by the server as a standalone document (ADR 0006).
 * The iframe has no `allow-same-origin`, so administrator scripts run in an
 * opaque origin without access to the RedApp session or storage. The document
 * reports its height with postMessage; only numeric messages from this
 * frame's own window are accepted, clamped to 120–50000 px.
 */
const props = defineProps<{
  vendor: string;
  app: string;
  /** `PublicApp.revision`; a change re-creates the frame. */
  revision: string;
}>();
const { t, locale } = useI18n();
const frame = ref<HTMLIFrameElement>();

const SANDBOX =
  "allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation";
const MIN_HEIGHT = 120;
const MAX_HEIGHT = 50_000;
// Room for the document's bottom margin so no inner scrollbar appears.
const PADDING = 20;

const source = computed(
  () =>
    `/api/apps/${encodeURIComponent(props.vendor)}/${encodeURIComponent(props.app)}` +
    `/instructions/document?lang=${encodeURIComponent(locale.value)}`,
);

function onMessage(event: MessageEvent) {
  const target = frame.value;
  if (!target || event.source === null || event.source !== target.contentWindow) return;
  const data: unknown = event.data;
  if (typeof data !== "object" || data === null) return;
  const { type, height } = data as { type?: unknown; height?: unknown };
  if (type !== "redapp-instructions-height") return;
  if (typeof height !== "number" || !Number.isFinite(height)) return;
  const clamped = Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, Math.ceil(height) + PADDING));
  target.style.height = `${clamped}px`;
}

onMounted(() => {
  window.addEventListener("message", onMessage);
});
onBeforeUnmount(() => {
  window.removeEventListener("message", onMessage);
});
</script>

<template>
  <iframe
    ref="frame"
    :key="`${source}:${revision}`"
    :src="source"
    :title="t('instructions.frameTitle')"
    :sandbox="SANDBOX"
    allow="clipboard-write"
    loading="lazy"
    class="block h-[120px] w-full rounded-lg border border-border bg-white"
  />
</template>
