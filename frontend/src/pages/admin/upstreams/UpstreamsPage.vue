<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil, Plus, Trash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRouter } from "vue-router";
import { UpstreamDialog, useDeleteUpstream, useUpstreams, type Upstream } from "@/features/mirrors";
import { isApiError } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { confirm, toast } from "@/shared/lib";
import {
  Alert,
  Button,
  DataTable,
  IconButton,
  PageHeader,
  RelativeTime,
  type DataTableColumn,
} from "@/shared/ui";

/** Upstream RedApp instances: add, rename or move, delete unused ones. */
const { t } = useI18n();
const router = useRouter();
const format = useFormat();
const upstreams = useUpstreams();
const remove = useDeleteUpstream();
const editing = ref<string | undefined>();
const dialogOpen = ref(false);
/** Name of the upstream whose deletion was refused because it is in use. */
const inUse = ref<string | null>(null);

const columns = computed<DataTableColumn[]>(() => [
  { key: "name", label: t("mirrors.list.name") },
  { key: "origin", label: t("mirrors.list.origin") },
  { key: "mirrors", label: t("mirrors.list.mirrors"), align: "end" },
  { key: "created", label: t("mirrors.list.created") },
  { key: "actions", label: t("mirrors.list.actions"), hideLabel: true, align: "end" },
]);

function add(): void {
  editing.value = undefined;
  dialogOpen.value = true;
}

function edit(upstream: Upstream): void {
  editing.value = upstream.uid;
  dialogOpen.value = true;
}

async function created(upstream: Upstream): Promise<void> {
  await router.push({ name: "admin-upstream", params: { upstream: upstream.uid } });
}

async function destroy(upstream: Upstream): Promise<void> {
  inUse.value = null;
  const confirmed = await confirm({
    title: t("mirrors.list.deleteTitle", { name: upstream.name }),
    description: t("mirrors.list.deleteWarning"),
    confirmLabel: t("common.actions.delete"),
    tone: "danger",
  });
  if (!confirmed) return;
  try {
    await remove.mutateAsync(upstream);
    toast({ tone: "success", title: t("mirrors.list.deleted", { name: upstream.name }) });
  } catch (error) {
    if (isApiError(error, "UPSTREAM_IN_USE")) inUse.value = upstream.name;
    else if (isApiError(error, "REVISION_CONFLICT")) {
      toast({ tone: "warning", title: t("mirrors.list.conflict") });
    }
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      :title="t('adminShell.titles.upstreams')"
      :description="t('mirrors.list.description')"
    >
      <template #actions>
        <Button variant="primary" @click="add">
          <Plus aria-hidden="true" /> {{ t("mirrors.list.add") }}
        </Button>
      </template>
    </PageHeader>
    <Alert v-if="inUse" tone="danger">{{ t("mirrors.list.inUse", { name: inUse }) }}</Alert>
    <DataTable
      :caption="t('mirrors.list.caption')"
      :columns="columns"
      :rows="upstreams.data.value?.items"
      :row-key="(row: Upstream) => row.uid"
      :loading="upstreams.isFetching.value"
      :error="upstreams.error.value"
      :empty-text="t('mirrors.list.empty')"
      @retry="upstreams.refetch()"
    >
      <template #cell-name="{ row }">
        <RouterLink
          :to="{ name: 'admin-upstream', params: { upstream: row.uid } }"
          class="rounded-sm font-medium hover:underline focus-ring"
        >
          {{ row.name }}
        </RouterLink>
      </template>
      <template #cell-origin="{ row }">
        <span class="font-mono text-xs">{{ row.origin }}</span>
      </template>
      <template #cell-mirrors="{ row }">
        <span class="tabular-nums">{{ format.number(row.mirrors) }}</span>
      </template>
      <template #cell-created="{ row }">
        <RelativeTime :value="row.created_at" />
      </template>
      <template #cell-actions="{ row }">
        <div class="flex justify-end gap-1">
          <IconButton
            :label="t('mirrors.list.edit', { name: row.name })"
            size="sm"
            @click="edit(row)"
          >
            <Pencil aria-hidden="true" />
          </IconButton>
          <IconButton
            :label="t('mirrors.list.delete', { name: row.name })"
            size="sm"
            :disabled="remove.isPending.value"
            @click="destroy(row)"
          >
            <Trash2 aria-hidden="true" />
          </IconButton>
        </div>
      </template>
    </DataTable>
    <UpstreamDialog v-model:open="dialogOpen" :upstream="editing" @created="created" />
  </div>
</template>
