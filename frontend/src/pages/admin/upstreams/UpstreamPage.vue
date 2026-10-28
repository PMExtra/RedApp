<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import { UpstreamCatalog, UpstreamDialog, useUpstream } from "@/features/mirrors";
import { isApiError } from "@/shared/api";
import { useFormat } from "@/shared/i18n";
import { useDocumentTitle } from "@/shared/lib";
import { AsyncState, Button, EmptyState, PageHeader, type Crumb } from "@/shared/ui";

/** One upstream: its address and identity, and the applications it publishes. */
const { t } = useI18n();
const route = useRoute();
const format = useFormat();
const uid = computed(() => String(route.params.upstream));
const upstream = useUpstream(uid);
const editing = ref(false);

const name = computed(() => upstream.data.value?.name ?? t("adminShell.titles.upstream"));
useDocumentTitle(name);
const notFound = computed(() => isApiError(upstream.error.value, "UPSTREAM_NOT_FOUND"));
const breadcrumbs = computed<Crumb[]>(() => [
  { label: t("adminShell.titles.upstreams"), to: { name: "admin-upstreams" } },
  { label: name.value },
]);
</script>

<template>
  <div class="flex flex-col gap-6">
    <template v-if="notFound">
      <PageHeader :title="t('adminShell.titles.upstream')" :breadcrumbs="breadcrumbs" />
      <EmptyState
        :title="t('mirrors.upstream.notFound')"
        :description="t('mirrors.upstream.notFoundHint')"
      >
        <template #action>
          <Button as-child>
            <RouterLink :to="{ name: 'admin-upstreams' }">{{
              t("mirrors.upstream.back")
            }}</RouterLink>
          </Button>
        </template>
      </EmptyState>
    </template>
    <AsyncState
      v-else
      :loading="upstream.isPending.value"
      :error="upstream.data.value ? undefined : upstream.error.value"
      @retry="upstream.refetch()"
    >
      <div v-if="upstream.data.value" class="flex flex-col gap-6">
        <PageHeader :title="upstream.data.value.name" :breadcrumbs="breadcrumbs">
          <template #meta>
            <dl class="flex flex-wrap gap-x-6 gap-y-1">
              <div class="flex gap-2">
                <dt>{{ t("mirrors.upstream.origin") }}</dt>
                <dd class="font-mono text-fg">{{ upstream.data.value.origin }}</dd>
              </div>
              <div class="flex gap-2">
                <dt>{{ t("mirrors.upstream.instance") }}</dt>
                <dd class="font-mono text-fg">{{ upstream.data.value.instance_id }}</dd>
              </div>
              <div class="flex gap-2">
                <dt>{{ t("mirrors.upstream.mirrors") }}</dt>
                <dd class="text-fg tabular-nums">
                  {{ format.number(upstream.data.value.mirrors) }}
                </dd>
              </div>
            </dl>
          </template>
          <template #actions>
            <Button @click="editing = true">
              <Pencil aria-hidden="true" /> {{ t("mirrors.upstream.edit") }}
            </Button>
          </template>
        </PageHeader>
        <UpstreamCatalog :upstream="upstream.data.value.uid" />
        <UpstreamDialog v-model:open="editing" :upstream="upstream.data.value.uid" />
      </div>
    </AsyncState>
  </div>
</template>
