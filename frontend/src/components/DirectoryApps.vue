<script setup lang="ts">
import EntityIcon from "./EntityIcon.vue";
import IconButton from "./IconButton.vue";
import RelativeTime from "./RelativeTime.vue";
import { computed, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import { directoryIcon, applicationPath, type ManagedApplication, type Vendor } from "../directory";
import { errorText, language, t } from "../i18n";
import PageNavigation from "./PageNavigation.vue";
const props = defineProps<{ vendor: Vendor; query: string; state: string }>();
interface ApplicationRow extends ManagedApplication {
  latest_version?: string;
  version_discovered_at?: string | null;
  successful_downloads?: number | null;
}
const route = useRoute(), router = useRouter();
const search = computed({ get: () => props.query, set: (q: string) => { void router.replace({ query: { ...route.query, q: q || undefined } }); } });
const sort = ref("name"), order = ref("asc");
const columns = [{ key: "name", label: "Application" }, { key: "version", label: "Latest version" }, { key: "updated", label: "Version discovered" }, { key: "downloads", label: "Successful downloads" }] as const;
function sortBy(key: string) {
  if (sort.value === key) order.value = order.value === "asc" ? "desc" : "asc";
  else { order.value = key === "name" ? "asc" : "desc"; sort.value = key; }
}
const list = reactive(useNumberedCollection<ApplicationRow>(computed(() =>
  `vendors/${props.vendor.id}/apps?${new URLSearchParams({ q: props.query, state: props.state, view: "table", sort: sort.value, order: order.value, lang: language.value })}`), 20));
</script>
<template>
  <div class="expanded-apps">
    <div class="application-table-toolbar">
      <label>{{ t('Search applications') }}<input v-model="search" type="search" maxlength="128" :placeholder="t('Search applications')" /></label>
      <RouterLink v-if="!vendor.deleted_at" class="button-link" :aria-label="t('Add application')" :to="`/admin/vendors/${vendor.id}/apps/new`">{{ t('Add application') }}</RouterLink>
    </div>
    <p class="muted small-text">{{ t('Version time is when RedApp first discovered the latest known version, not its publication or configuration time. Downloads count recorded successful transfers across source changes; unavailable statistics show —.') }}</p>
    <p v-if="list.error" class="error" role="alert">{{ errorText(list.error) }}<IconButton class="secondary" @click="list.refresh" icon="refresh" :label="t('Retry')" /></p>
    <p v-if="list.loading && !list.loaded" role="status">{{ t('Loading…') }}</p>
    <div class="application-table-scroll">
      <table class="application-table" :aria-label="t('Applications')" :aria-busy="list.loading">
        <thead><tr><th v-for="column in columns" :key="column.key" scope="col" :aria-sort="sort === column.key ? (order === 'asc' ? 'ascending' : 'descending') : 'none'"><button type="button" @click="sortBy(column.key)">{{ t(column.label) }}<span aria-hidden="true">{{ sort === column.key ? (order === 'asc' ? ' ↑' : ' ↓') : ' ↕' }}</span></button></th></tr></thead>
        <tbody>
          <tr v-for="app in list.items" :key="app.uid" :class="{ 'is-disabled': !app.enabled || !vendor.enabled }">
            <td><RouterLink :to="applicationPath(app, app.deleted_at ? 'settings' : undefined)"><EntityIcon :src="directoryIcon(app.icon)" size="tile" /><span>{{ app.name[language] }}</span></RouterLink><small v-if="app.deleted_at">{{ t('Deleted') }}</small><small v-else-if="!app.enabled || !vendor.enabled">{{ app.enabled ? t('Disabled by vendor') : t('Disabled') }}</small></td>
            <td>{{ app.latest_version || '—' }}</td>
            <td><RelativeTime v-if="app.version_discovered_at" :value="app.version_discovered_at" /><span v-else>—</span></td>
            <td>{{ app.successful_downloads == null ? '—' : app.successful_downloads.toLocaleString(language) }}</td>
          </tr>
          <tr v-if="list.loaded && !list.items.length"><td colspan="4">{{ t('No applications found.') }}</td></tr>
        </tbody>
      </table>
    </div>
    <PageNavigation :label="t('Application pages')" :page="list.page" :total="list.total" :total-pages="list.totalPages" :previous="list.previousAvailable" :next="list.nextAvailable" :loading="list.loading" @previous="list.previous" @next="list.next" @go="list.go" @refresh="list.refresh" />
  </div>
</template>
