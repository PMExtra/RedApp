<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, useId } from "vue";
import { language, localDate, t } from "../i18n";
const props = withDefaults(defineProps<{ value?: string | null; focusable?: boolean }>(), { focusable: true });
const now = ref(Date.now());
const id = useId();
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => { timer = setInterval(() => { now.value = Date.now(); }, 30000); });
onUnmounted(() => clearInterval(timer));
const timestamp = computed(() => props.value ? Date.parse(props.value) : NaN);
const known = computed(() => Number.isFinite(timestamp.value) && timestamp.value > 0);
const exact = computed(() => known.value ? localDate(props.value!) : "");
const relative = computed(() => {
  const seconds = Math.max(0, (now.value - timestamp.value) / 1000);
  if (seconds < 60) return t("Just now");
  const units = [[31536000, "year"], [2592000, "month"], [604800, "week"], [86400, "day"], [3600, "hour"], [60, "minute"]] as const;
  const [size, unit] = units.find(([size]) => seconds >= size)!;
  return new Intl.RelativeTimeFormat(language.value, { numeric: "always" }).format(-Math.floor(seconds / size), unit);
});
</script>
<template>
  <span v-if="known" class="relative-time">
    <time :datetime="value!" :title="exact" :tabindex="focusable ? 0 : undefined" :aria-label="t('Discovered {time}', { time: relative })" :aria-describedby="id">{{ relative }}</time>
    <span :id="id" role="tooltip" class="relative-time-tooltip">{{ exact }}</span>
  </span>
  <span v-else>{{ t("Discovery time unknown") }}</span>
</template>
