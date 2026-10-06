<script setup lang="ts">
import { computed, ref, watch } from "vue";
import Icon from "./Icon.vue";
import { entityIconSizes } from "../entityIconSizes";
const props = withDefaults(defineProps<{ src?: string; size?: keyof typeof entityIconSizes }>(), { size: "search" });
const pixels = computed(() => entityIconSizes[props.size]);
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
    :style="{ width: `${pixels}px`, height: `${pixels}px` }"
    aria-hidden="true"
  >
    <img
      v-if="src && !failed"
      :key="src"
      :src="src"
      :width="pixels"
      :height="pixels"
      alt=""
      @error="failedImage"
    />
    <Icon v-else name="box" :size="pixels" />
  </span>
</template>
