<script setup lang="ts">
import { computed, ref } from "vue";
import { Building2, Plus, Search, Tags, Upload } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";
import {
  StateFilter,
  useListQuery,
  useVendorList,
  VENDOR_PAGE_SIZE,
  VendorCard,
} from "@/features/directory";
import { ImportDialog } from "@/features/exchange";
import {
  Alert,
  AsyncState,
  Button,
  EmptyState,
  Field,
  Input,
  PageHeader,
  Pagination,
} from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const list = useListQuery();
const vendors = useVendorList(() => ({
  q: list.q.value,
  state: list.state.value,
  page: list.page.value,
}));
const importing = ref(false);
const items = computed(() => vendors.data.value?.items ?? []);
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader
      :title="t('adminShell.titles.vendors')"
      :description="t('directory.list.description')"
    >
      <template #actions>
        <Button @click="importing = true">
          <Upload aria-hidden="true" /> {{ t("exchange.import.action") }}
        </Button>
        <Button as-child>
          <RouterLink :to="{ name: 'admin-categories' }">
            <Tags aria-hidden="true" /> {{ t("directory.list.categories") }}
          </RouterLink>
        </Button>
        <Button as-child variant="primary">
          <RouterLink :to="{ name: 'admin-vendor-new' }">
            <Plus aria-hidden="true" /> {{ t("directory.actions.addVendor") }}
          </RouterLink>
        </Button>
      </template>
    </PageHeader>

    <Alert v-if="route.query.cleanup === 'pending'" tone="warning">
      {{ t("directory.list.cleanupPending") }}
    </Alert>

    <div class="flex flex-wrap items-end gap-3">
      <div class="relative w-full max-w-sm">
        <Field v-slot="{ control }" :label="t('directory.list.search')" hide-label>
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
            :placeholder="t('directory.list.search')"
          />
        </Field>
      </div>
      <StateFilter :model-value="list.state.value" @update:model-value="list.setState" />
      <p
        v-if="vendors.data.value"
        class="ms-auto text-sm text-muted"
        aria-live="polite"
        aria-atomic="true"
      >
        {{
          t("directory.list.total", { count: vendors.data.value.total }, vendors.data.value.total)
        }}
      </p>
    </div>

    <AsyncState
      :loading="vendors.isPending.value"
      :error="vendors.error.value"
      :empty="items.length === 0"
      @retry="vendors.refetch()"
    >
      <template #empty>
        <EmptyState
          v-if="list.filtered.value"
          :title="t('directory.list.emptyFiltered')"
          :description="t('directory.list.emptyFilteredHint')"
        >
          <template #icon><Search aria-hidden="true" /></template>
        </EmptyState>
        <EmptyState
          v-else
          :title="t('directory.list.empty')"
          :description="t('directory.list.emptyHint')"
        >
          <template #icon><Building2 aria-hidden="true" /></template>
          <template #action>
            <Button as-child variant="primary">
              <RouterLink :to="{ name: 'admin-vendor-new' }">
                <Plus aria-hidden="true" /> {{ t("directory.actions.addVendor") }}
              </RouterLink>
            </Button>
          </template>
        </EmptyState>
      </template>
      <div
        class="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
        :aria-busy="vendors.isFetching.value || undefined"
      >
        <VendorCard
          v-for="vendor in items"
          :key="vendor.uid"
          :vendor="vendor"
          :q="list.q.value"
          :state="list.state.value"
        />
      </div>
    </AsyncState>

    <Pagination
      v-if="vendors.data.value"
      :page="list.page.value"
      :total="vendors.data.value.total"
      :page-size="VENDOR_PAGE_SIZE"
      @update:page="list.setPage"
    />

    <ImportDialog v-model:open="importing" />
  </div>
</template>
