<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { describeError } from "@/shared/api";
import Alert from "./Alert.vue";
import Button from "./Button.vue";
import Skeleton from "./Skeleton.vue";

/**
 * Loading / error / empty frame for query-backed content:
 * `<AsyncState :loading="q.isPending.value" :error="q.error.value" :empty="!q.data.value?.items.length" @retry="q.refetch()">`.
 * Keeps showing the content while refetching after it was loaded once.
 */
const props = defineProps<{
  loading?: boolean;
  error?: unknown;
  empty?: boolean;
  emptyText?: string;
}>();
const emit = defineEmits<{ retry: [] }>();
defineSlots<{ default?: () => unknown; loading?: () => unknown; empty?: () => unknown }>();
const { t } = useI18n();
const failure = computed(() => (props.error ? describeError(props.error) : null));
</script>

<template>
  <div v-if="loading" aria-busy="true">
    <slot name="loading">
      <div class="flex flex-col gap-2">
        <Skeleton class="h-5 w-1/3" />
        <Skeleton class="h-5 w-2/3" />
        <Skeleton class="h-5 w-1/2" />
      </div>
    </slot>
    <span class="sr-only">{{ t("common.states.loading") }}</span>
  </div>
  <Alert v-else-if="failure" tone="danger" :title="t('ui.asyncState.error')">
    {{ failure.message }}
    <span v-if="failure.requestId" class="mt-1 block text-xs text-muted">
      {{ t("common.requestId") }}: <code>{{ failure.requestId }}</code>
    </span>
    <template #actions>
      <Button size="sm" @click="emit('retry')">{{ t("common.actions.retry") }}</Button>
    </template>
  </Alert>
  <div v-else-if="empty" class="py-8 text-center text-sm text-muted">
    <slot name="empty">{{ emptyText ?? t("common.states.empty") }}</slot>
  </div>
  <slot v-else />
</template>
