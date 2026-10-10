<script setup lang="ts">
import { ChevronLeft, ChevronRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import Button from "./Button.vue";

/** Previous/next navigation for cursor lists; pair with `useCursorPagination()`. */
defineProps<{ page: number; hasPrevious: boolean; hasNext: boolean; loading?: boolean }>();
const emit = defineEmits<{ previous: []; next: [] }>();
const { t } = useI18n();
</script>

<template>
  <nav
    v-if="hasPrevious || hasNext"
    :aria-label="t('ui.pagination.label')"
    class="flex items-center gap-2"
  >
    <Button size="sm" :disabled="!hasPrevious || loading" @click="emit('previous')">
      <ChevronLeft aria-hidden="true" />
      {{ t("ui.pagination.previous") }}
    </Button>
    <span class="text-xs text-muted tabular-nums">{{ t("ui.pagination.page", { page }) }}</span>
    <Button size="sm" :disabled="!hasNext || loading" @click="emit('next')">
      {{ t("ui.pagination.next") }}
      <ChevronRight aria-hidden="true" />
    </Button>
  </nav>
</template>
