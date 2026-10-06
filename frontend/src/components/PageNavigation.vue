<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { ref, watch } from "vue";
import { t } from "../i18n";
const props = defineProps<{
  label: string;
  compact?: boolean;
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
  <nav class="page-navigation" :class="{ 'page-navigation-compact': compact }" :aria-label="label">
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
      <IconButton
        type="button"
        class="secondary"
        :disabled="loading || !previous"
        @click="emit('previous')"
        icon="back"
        :label="t('Previous page')"
      />
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
        <IconButton
          class="secondary"
          :disabled="loading || total === 0"
          type="submit"
          icon="arrow"
          :label="t('Go')"
        />
      </form>
      <IconButton
        type="button"
        class="secondary"
        :disabled="loading || !next"
        @click="emit('next')"
        icon="arrow"
        :label="t('Next page')"
      />
      <IconButton
        type="button"
        class="secondary"
        :disabled="loading"
        @click="emit('refresh')"
        icon="refresh"
        :label="t('Refresh')"
      />
    </div>
    <p v-if="invalid" class="error" role="alert">
      {{ t("Enter a page from 1 to {pages}.", { pages: totalPages || 1 }) }}
    </p>
  </nav>
</template>
