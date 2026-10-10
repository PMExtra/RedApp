<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil, Plus, Search, Trash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { useFormat, useLocalized, type Locale } from "@/shared/i18n";
import { confirm, toast } from "@/shared/lib";
import {
  Button,
  DataTable,
  EntityIcon,
  Field,
  Input,
  Pagination,
  RelativeTime,
  Switch,
  Tooltip,
  type DataTableColumn,
  type DataTableSort,
} from "@/shared/ui";
import { appRoute } from "./links";
import { useListQuery } from "./listQuery";
import {
  APP_PAGE_SIZE,
  useAppEnabled,
  useAppList,
  useDeleteApp,
  type AppListItem,
  type AppSort,
  type Vendor,
} from "./queries";
import StateFilter from "./StateFilter.vue";

/**
 * Applications of one vendor: server-side search, filter, sorting (name
 * ascending first, the other columns descending first) and paging, with
 * enable, edit and delete per row.
 */
const props = defineProps<{ vendor: Vendor }>();
const { t, locale } = useI18n();
const localized = useLocalized();
const format = useFormat();
const list = useListQuery();

const sort = ref<DataTableSort>({ key: "name", direction: "asc" });
const sortModel = computed<DataTableSort | null>({
  get: () => sort.value,
  set: (next) => {
    if (!next) return;
    sort.value =
      next.key === sort.value.key
        ? next
        : { key: next.key, direction: next.key === "name" ? "asc" : "desc" };
    list.setPage(1);
  },
});

const apps = useAppList(() => ({
  vendor: props.vendor.id,
  q: list.q.value,
  state: list.state.value,
  sort: sort.value.key as AppSort,
  order: sort.value.direction,
  lang: locale.value as Locale,
  page: list.page.value,
}));

const columns = computed<DataTableColumn[]>(() => [
  { key: "name", label: t("directory.table.name"), sortable: true },
  { key: "version", label: t("directory.table.version"), sortable: true },
  { key: "updated", label: t("directory.table.discovered"), sortable: true },
  { key: "downloads", label: t("directory.table.downloads"), sortable: true, align: "end" },
  { key: "enabled", label: t("directory.fields.enabled") },
  { key: "actions", label: t("directory.table.actions"), hideLabel: true, align: "end" },
]);

const enable = useAppEnabled();
const remove = useDeleteApp();
const busy = ref<string | null>(null);

function toggle(app: AppListItem, enabled: boolean): void {
  busy.value = app.uid;
  enable.mutate(
    { app, enabled },
    {
      onSettled: () => {
        busy.value = null;
      },
    },
  );
}

async function destroy(app: AppListItem): Promise<void> {
  const confirmed = await confirm({
    title: t("directory.delete.appTitle", { key: app.key }),
    description: t("directory.delete.appWarning"),
    confirmLabel: t("common.actions.delete"),
    tone: "danger",
  });
  if (!confirmed) return;
  busy.value = app.uid;
  try {
    const result = await remove.mutateAsync(app);
    toast({
      tone: result.cleanup_pending ? "warning" : "success",
      title: t("directory.delete.done", { key: app.key }),
      description: result.cleanup_pending ? t("directory.delete.cleanupPending") : null,
    });
  } catch {
    // Reported by the global error handler or the conflict notice.
  } finally {
    busy.value = null;
  }
}

function onDelete(app: AppListItem): void {
  if (!app.builtin_template && busy.value === null) void destroy(app);
}

const emptyText = computed(() =>
  list.filtered.value ? t("directory.table.noMatches") : t("directory.table.empty"),
);
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-end gap-3">
      <div class="relative w-full max-w-sm">
        <Field v-slot="{ control }" :label="t('directory.table.search')" hide-label>
          <Search
            class="pointer-events-none absolute start-2.5 top-2.5 size-4 text-muted"
            aria-hidden="true"
          />
          <Input
            v-bind="control"
            v-model="list.search.value"
            type="search"
            maxlength="128"
            class="ps-8"
            :placeholder="t('directory.table.search')"
          />
        </Field>
      </div>
      <StateFilter :model-value="list.state.value" @update:model-value="list.setState" />
      <Button v-if="!vendor.deleted_at" as-child variant="primary" class="ms-auto">
        <RouterLink :to="{ name: 'admin-app-new', params: { vendor: vendor.id } }">
          <Plus aria-hidden="true" /> {{ t("directory.actions.addApp") }}
        </RouterLink>
      </Button>
    </div>

    <DataTable
      v-model:sort="sortModel"
      :caption="t('directory.table.caption', { vendor: localized(vendor.name) || vendor.id })"
      :columns="columns"
      :rows="apps.data.value?.items"
      :row-key="(row: AppListItem) => row.uid"
      :loading="apps.isFetching.value"
      :error="apps.error.value"
      :empty-text="emptyText"
      @retry="apps.refetch()"
    >
      <template #cell-name="{ row }">
        <RouterLink
          :to="appRoute(row)"
          class="flex items-center gap-3 rounded-md font-medium hover:underline focus-ring"
          :class="(!row.enabled || !vendor.enabled || row.deleted_at) && 'opacity-60'"
        >
          <EntityIcon :src="row.icon" size="sm" />
          <span class="flex flex-col">
            <span>{{ localized(row.name) || row.id }}</span>
            <span class="font-mono text-xs font-normal text-muted">{{ row.id }}</span>
          </span>
        </RouterLink>
      </template>
      <template #cell-version="{ row }">
        <span class="font-mono text-xs">{{
          row.latest_version ?? t("common.states.unknown")
        }}</span>
      </template>
      <template #cell-updated="{ row }">
        <RelativeTime :value="row.version_discovered_at" />
      </template>
      <template #cell-downloads="{ row }">
        <span class="tabular-nums">{{ format.number(row.successful_downloads) }}</span>
      </template>
      <template #cell-enabled="{ row }">
        <Switch
          :model-value="row.enabled"
          :aria-label="t('directory.table.enableApp', { name: localized(row.name) || row.id })"
          :disabled="busy !== null || row.deleted_at !== null"
          @update:model-value="toggle(row, $event)"
        />
      </template>
      <template #cell-actions="{ row }">
        <div class="flex justify-end gap-1">
          <Tooltip :content="t('directory.table.edit', { name: localized(row.name) || row.id })">
            <Button as-child variant="ghost" size="sm" icon>
              <RouterLink
                :to="appRoute(row, 'settings')"
                :aria-label="t('directory.table.edit', { name: localized(row.name) || row.id })"
              >
                <Pencil aria-hidden="true" />
              </RouterLink>
            </Button>
          </Tooltip>
          <Tooltip
            :content="
              row.builtin_template
                ? t('directory.delete.builtinApp')
                : t('directory.table.delete', { name: localized(row.name) || row.id })
            "
          >
            <Button
              variant="ghost"
              size="sm"
              icon
              :aria-label="t('directory.table.delete', { name: localized(row.name) || row.id })"
              :aria-disabled="row.builtin_template || busy !== null || undefined"
              @click="onDelete(row)"
            >
              <Trash2 aria-hidden="true" />
            </Button>
          </Tooltip>
        </div>
      </template>
    </DataTable>

    <Pagination
      v-if="apps.data.value"
      :page="list.page.value"
      :total="apps.data.value.total"
      :page-size="APP_PAGE_SIZE"
      @update:page="list.setPage"
    />
  </div>
</template>
