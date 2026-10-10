<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useFormat } from "@/shared/i18n";
import Tooltip from "./Tooltip.vue";

/** "5 minutes ago", refreshed every 30 s, with the exact local time as a tooltip. */
const props = defineProps<{ value: string | null | undefined; fallback?: string }>();
const format = useFormat();
const now = ref(new Date());
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  timer = setInterval(() => (now.value = new Date()), 30_000);
});
onBeforeUnmount(() => clearInterval(timer));
const exact = computed(() =>
  format.dateTime(props.value, { dateStyle: "full", timeStyle: "long" }),
);
</script>

<template>
  <span v-if="!value">{{ fallback ?? "—" }}</span>
  <Tooltip v-else :content="exact">
    <time :datetime="value" tabindex="0" class="rounded-sm focus-ring">
      {{ format.relativeTime(value, now) }}
    </time>
  </Tooltip>
</template>
