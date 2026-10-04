<script setup lang="ts">
import { ref, watch } from "vue";
import { t } from "../i18n";
const props = defineProps<{
  label: string;
  page: number;
  previous: boolean;
  next: boolean;
  loading: boolean;
  total?: number;
  totalPages?: number;
}>();
const emit = defineEmits<{
  previous: [];
  next: [];
  refresh: [];
  go: [number];
}>();
const requested = ref(String(props.page)),
  invalid = ref(false);
watch(
  () => [props.page, props.totalPages],
  () => {
    requested.value = String(props.page);
    invalid.value = false;
  },
);
function jump() {
  const n = Number(requested.value);
  invalid.value =
    !/^[1-9]\d*$/.test(requested.value) ||
    !Number.isSafeInteger(n) ||
    n > (props.totalPages || 1);
  if (!invalid.value) emit("go", n);
}
</script>
<template>
  <nav class="page-navigation" :aria-label="label">
    <span class="pagination-summary"
      >{{
        total !== undefined ? t("{count} items", { count: total }) : label
      }}
      ·
      {{
        totalPages
          ? t("Page {page} of {pages}", { page, pages: totalPages })
          : t("Page {page}", { page })
      }}</span
    >
    <div class="pagination-actions">
      <button
        type="button"
        class="secondary"
        :disabled="loading || !previous"
        @click="emit('previous')"
      >
        {{ t("Previous page") }}
      </button>
      <form
        v-if="totalPages !== undefined"
        class="page-jump"
        @submit.prevent="jump"
      >
        <label
          ><span class="sr-only">{{ t("Page number") }}</span
          ><input
            v-model="requested"
            :aria-label="`${label}: ${t('Page number')}`"
            inputmode="numeric"
            :aria-invalid="invalid"
            :disabled="loading || total === 0"
            @input="invalid = false"
        /></label>
        <button class="secondary" :disabled="loading || total === 0">
          {{ t("Go") }}
        </button>
      </form>
      <button
        type="button"
        class="secondary"
        :disabled="loading || !next"
        @click="emit('next')"
      >
        {{ t("Next page") }}
      </button>
      <button
        type="button"
        class="secondary"
        :disabled="loading"
        @click="emit('refresh')"
      >
        {{ t("Refresh") }}
      </button>
    </div>
    <p v-if="invalid" class="error" role="alert">
      {{ t("Enter a page from 1 to {pages}.", { pages: totalPages || 1 }) }}
    </p>
  </nav>
</template>
