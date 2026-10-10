<script setup lang="ts">
import { computed } from "vue";
import { RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { EventsTable, useEvents } from "@/features/events";
import { describeError, isApiError } from "@/shared/api";
import { useAutoRefresh, useCursorPagination } from "@/shared/lib";
import {
  Alert,
  AutoRefreshToggle,
  Button,
  CursorPagination,
  IconButton,
  PageHeader,
} from "@/shared/ui";

const { t } = useI18n();
const auto = useAutoRefresh();
const pages = useCursorPagination();
const events = useEvents(pages.cursor, { refetchInterval: auto.refetchInterval });
const page = computed(() => events.data.value);
// A cursor can expire when old events are pruned; the first page always works.
const cursorExpired = computed(() => isApiError(events.error.value, "INVALID_CURSOR"));
// Failed refreshes keep the last page on screen with a warning.
const staleError = computed(() =>
  events.error.value && (page.value || cursorExpired.value)
    ? describeError(events.error.value)
    : null,
);
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader :title="t('adminShell.titles.events')" :description="t('eventsPage.description')">
      <template #actions>
        <AutoRefreshToggle v-model="auto.enabled.value" />
        <IconButton
          :label="t('common.actions.refresh')"
          variant="secondary"
          :loading="events.isFetching.value"
          @click="events.refetch()"
        >
          <RefreshCw aria-hidden="true" />
        </IconButton>
      </template>
    </PageHeader>

    <Alert
      v-if="staleError"
      tone="warning"
      :title="t(cursorExpired ? 'eventsPage.cursorExpired' : 'eventsPage.stale')"
    >
      {{ staleError.message }}
      <span v-if="staleError.requestId" class="mt-1 block text-xs text-muted">
        {{ t("common.requestId") }}: <code>{{ staleError.requestId }}</code>
      </span>
      <template v-if="cursorExpired && pages.hasPrevious.value" #actions>
        <Button size="sm" @click="pages.reset()">{{ t("eventsPage.firstPage") }}</Button>
      </template>
    </Alert>

    <EventsTable
      :events="page?.items"
      :loading="events.isFetching.value"
      :error="cursorExpired ? undefined : events.error.value"
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
