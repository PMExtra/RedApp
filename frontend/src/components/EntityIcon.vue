<script setup lang="ts">
import { computed, ref, watch } from "vue";
import Icon from "./Icon.vue";
import { entityIconSizes } from "../entityIconSizes";
const props = withDefaults(defineProps<{ src?: string; fallbackSrc?: string; size?: keyof typeof entityIconSizes; vendor?: boolean }>(), { size: "search" });
const pixels = computed(() => props.vendor ? 32 : entityIconSizes[props.size]);
const failedURLs = ref(new Set<string>());
const loadedURLs = ref(new Set<string>());
const imageSource = computed(() => [props.src, props.fallbackSrc].find((url) => url && !failedURLs.value.has(url)) || "");
watch(() => [props.src, props.fallbackSrc], () => { failedURLs.value = new Set(); loadedURLs.value = new Set(); }, { flush: "sync" });
function loadedImage(event: Event) {
  const url = (event.target as HTMLImageElement).getAttribute("src");
  if (url === imageSource.value) loadedURLs.value = new Set([...loadedURLs.value, url]);
}
function failedImage(event: Event) {
  const url = (event.target as HTMLImageElement).getAttribute("src");
  if (url === imageSource.value) failedURLs.value = new Set([...failedURLs.value, url]);
}
</script>
<template>
  <span
    v-if="!vendor || imageSource"
    class="entity-icon"
    :class="{ 'entity-icon--vendor': vendor, 'entity-icon--loading': vendor && !loadedURLs.has(imageSource) }"
    :style="{ width: vendor && imageSource ? undefined : `${pixels}px`, height: `${pixels}px` }"
    aria-hidden="true"
  >
    <img
      v-if="imageSource"
      :key="imageSource"
      :src="imageSource"
      :width="vendor ? undefined : pixels"
      :height="pixels"
      alt=""
      @error="failedImage"
      @load="loadedImage"
    />
    <Icon v-else name="box" :size="pixels" />
  </span>
</template>
