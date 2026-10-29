<script setup lang="ts">
import { computed } from "vue";
import { RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { describeError, isApiError } from "@/shared/api";
import { useAutoRefresh, useCursorPagination } from "@/shared/lib";
import { Alert, AutoRefreshToggle, Button, CursorPagination, IconButton } from "@/shared/ui";
import EventsTable from "./EventsTable.vue";
import { useEvents, type EventsApp } from "./queries";

/**
 * Pages of events with auto refresh: every event, or those of one `app`.
 * A cursor can expire when old events are pruned; the first page always works.
 */
const props = defineProps<{ app?: EventsApp }>();
const { t } = useI18n();
const auto = useAutoRefresh();
const pages = useCursorPagination();
const events = useEvents(pages.cursor, {
  refetchInterval: auto.refetchInterval,
  app: () => props.app,
});
const page = computed(() => events.data.value);
const cursorExpired = computed(() => isApiError(events.error.value, "INVALID_CURSOR"));
// Failed refreshes keep the last page on screen with a warning.
const staleError = computed(() =>
  events.error.value && (page.value || cursorExpired.value)
    ? describeError(events.error.value)
    : null,
);
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-end gap-2">
      <AutoRefreshToggle v-model="auto.enabled.value" />
      <IconButton
        :label="t('common.actions.refresh')"
        variant="secondary"
        :loading="events.isFetching.value"
        @click="events.refetch()"
      >
        <RefreshCw aria-hidden="true" />
      </IconButton>
    </div>

    <Alert
      v-if="staleError"
      tone="warning"
      :title="t(cursorExpired ? 'events.cursorExpired' : 'events.stale')"
    >
      {{ staleError.message }}
      <span v-if="staleError.requestId" class="mt-1 block text-xs text-muted">
        {{ t("common.requestId") }}: <code>{{ staleError.requestId }}</code>
      </span>
      <template v-if="cursorExpired && pages.hasPrevious.value" #actions>
        <Button size="sm" @click="pages.reset()">{{ t("events.firstPage") }}</Button>
      </template>
    </Alert>

    <EventsTable
      :events="page?.items"
      :loading="events.isFetching.value"
      :error="cursorExpired ? undefined : events.error.value"
      :hide-app="Boolean(app)"
      @retry="events.refetch()"
    />

    <CursorPagination
      :page="pages.page.value"
      :has-previous="pages.hasPrevious.value"
      :has-next="Boolean(page?.next_cursor)"
      :loading="events.isFetching.value && events.isPlaceholderData.value"
      @previous="pages.previous()"
      @next="pages.next(page?.next_cursor)"
    />
  </div>
</template>
