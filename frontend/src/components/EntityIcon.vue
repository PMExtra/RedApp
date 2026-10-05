<script setup lang="ts">
import { ref, watch } from "vue";
import Icon from "./Icon.vue";
const props = withDefaults(defineProps<{ src?: string; size?: number }>(), {
  size: 28,
});
const failed = ref(false);
watch(
  () => props.src,
  () => {
    failed.value = false;
  },
);
function failedImage(event: Event) {
  if ((event.target as HTMLImageElement).getAttribute("src") === props.src)
    failed.value = true;
}
</script>
<template>
  <span
    class="entity-icon"
    :style="{ width: `${size}px`, height: `${size}px` }"
    aria-hidden="true"
  >
    <img
      v-if="src && !failed"
      :key="src"
      :src="src"
      :width="size"
      :height="size"
      alt=""
      @error="failedImage"
    />
    <Icon v-else name="box" :size="size" />
  </span>
</template>
