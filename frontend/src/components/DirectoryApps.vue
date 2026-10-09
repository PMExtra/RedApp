<script setup lang="ts">
import DisabledReason from "./DisabledReason.vue";
import Icon from "./Icon.vue";
import DeleteConfirmation from "./DeleteConfirmation.vue";
import { api, ApiError, isCancellation } from "../api";
import { invalidateBootstrap, loadBootstrap } from "../bootstrap";
import EntityIcon from "./EntityIcon.vue";
import IconButton from "./IconButton.vue";
import RelativeTime from "./RelativeTime.vue";
import { computed, reactive, ref, onUnmounted, watch, useId } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import { directoryIcon, applicationPath, patchEntityEnabled, type ManagedApplication, type Vendor } from "../directory";
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
const state = computed({ get: () => props.state, set: (value: string) => { void router.replace({ query: { ...route.query, state: value === "current" ? undefined : value } }); } });
const hintId = useId();
const pending = ref<string>(), actionError = ref<unknown>(), deleting = ref<ManagedApplication>(), notice = ref("");
let controller: AbortController | undefined, generation = 0;
function cancelAction() { generation++; controller?.abort(); controller = undefined; pending.value = undefined; deleting.value = undefined; actionError.value = undefined; notice.value = ""; }
watch(() => props.vendor.uid, cancelAction, { flush: "sync" });
onUnmounted(cancelAction);
async function mutate(app: ManagedApplication, remove = false) {
  if (pending.value || (remove && app.builtin_template) || (!remove && app.deleted_at)) return;
  const request = new AbortController(), attempt = ++generation;
  controller = request; pending.value = app.uid; actionError.value = undefined; notice.value = "";
  try {
    if (remove) {
      const result = await api<{ cleanup_pending?: boolean }>(`apps/${app.key}`, { revision: app.revision, confirm_uid: app.uid, confirm_key: app.key }, request.signal, {}, "DELETE");
      if (attempt !== generation) return;
      if (result.cleanup_pending) notice.value = t("Deleted. Some stored files are awaiting cleanup; restart the server to retry cleanup.");
      deleting.value = undefined;
    } else {
      const result = await patchEntityEnabled("app", app.key, app.revision, !app.enabled, request.signal);
      if (attempt !== generation) return;
      if (!result.app || result.app.uid !== app.uid) throw Error("Saved record unavailable");
      list.items = list.items.map(item => item.uid === app.uid ? { ...item, ...result.app! } : item);
    }
    invalidateBootstrap(); void loadBootstrap();
    await list.reload();
  } catch (reason) {
    if (attempt !== generation || isCancellation(reason)) return;
    actionError.value = reason;
    if (reason instanceof ApiError && reason.status === 409) { deleting.value = undefined; await list.reload(); }
  } finally {
    if (attempt === generation) { pending.value = undefined; controller = undefined; }
  }
}
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
      <label class="search-field"><span class="sr-only">{{ t('Search applications') }}</span><Icon name="search" class="search-input-icon" /><input v-model="search" type="search" maxlength="128" :placeholder="t('Search applications')" /></label>
      <div class="button-group status-filter" role="group" :aria-label="t('Availability')"><button v-for="option in [{ value: 'current', label: t('All') }, { value: 'enabled', label: t('Enabled') }, { value: 'disabled', label: t('Disabled') }]" :key="option.value" class="secondary" type="button" :aria-pressed="state === option.value" @click="state = option.value">{{ option.label }}</button></div>
    <div class="table-help"><button type="button" class="table-help-trigger" :aria-label="t('Statistics information')" :aria-describedby="hintId"><Icon name="info" /></button><span :id="hintId" role="tooltip">{{ t('Version time is when RedApp first discovered the latest known version, not its publication or configuration time. Downloads count recorded successful transfers across source changes; unavailable statistics show —.') }}</span></div>
      <RouterLink v-if="!vendor.deleted_at" class="button-link" :aria-label="t('Add application')" :to="`/admin/vendors/${vendor.id}/apps/new`"><Icon name="plus" />{{ t('Add application') }}</RouterLink>
    </div>

    <p v-if="actionError" class="error" role="alert">{{ errorText(actionError) }}</p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <DeleteConfirmation v-if="deleting" application :record-key="deleting.key" :busy="!!pending" @confirm="mutate(deleting!, true)" @cancel="deleting = undefined" />
    <p v-if="list.error" class="error" role="alert">{{ errorText(list.error) }}<IconButton class="secondary" @click="list.refresh" icon="refresh" :label="t('Retry')" /></p>
    <p v-if="list.loading && !list.loaded" role="status">{{ t('Loading…') }}</p>
    <div class="application-table-scroll">
      <table class="application-table" :aria-label="t('Applications')" :aria-busy="list.loading">
        <thead><tr><th v-for="column in columns" :key="column.key" scope="col" :aria-sort="sort === column.key ? (order === 'asc' ? 'ascending' : 'descending') : 'none'"><button type="button" @click="sortBy(column.key)">{{ t(column.label) }}<Icon :name="sort === column.key ? (order === 'asc' ? 'ascending' : 'descending') : 'sort'" :size="15" /></button></th><th scope="col">{{ t('Actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="app in list.items" :key="app.uid" :class="{ 'is-disabled': !app.enabled || !vendor.enabled }">
            <td><RouterLink :to="applicationPath(app, app.deleted_at ? 'settings' : undefined)"><EntityIcon :src="directoryIcon(app.icon)" size="tile" /><span :title="app.name[language]">{{ app.name[language] }}</span></RouterLink></td>
            <td>{{ app.latest_version || '—' }}</td>
            <td class="table-time"><RelativeTime v-if="app.version_discovered_at" :value="app.version_discovered_at" /><span v-else>—</span></td>
            <td class="numeric">{{ app.successful_downloads == null ? '—' : app.successful_downloads.toLocaleString(language) }}</td>
            <td class="application-row-actions"><button type="button" class="link-action" :disabled="!!pending || !!app.deleted_at" :aria-busy="pending === app.uid" :title="!vendor.enabled ? t('Disabled by vendor') : undefined" @click="mutate(app)">{{ app.enabled ? t('Enabled') : t('Disabled') }}</button><RouterLink :to="applicationPath(app, 'settings')">{{ t('Edit') }}</RouterLink><DisabledReason :reason="app.builtin_template ? t('Preset templates cannot be deleted. You can disable them instead.') : undefined"><button type="button" class="link-action danger-link" :disabled="!!pending || !!app.builtin_template" @click="deleting = app">{{ t('Delete') }}</button></DisabledReason></td>
          </tr>
          <tr v-if="list.loaded && !list.items.length"><td colspan="5">{{ t('No applications found.') }}</td></tr>
        </tbody>
      </table>
    </div>
    <PageNavigation compact :label="t('Application pages')" :page="list.page" :total="list.total" :total-pages="list.totalPages" :previous="list.previousAvailable" :next="list.nextAvailable" :loading="list.loading" @previous="list.previous" @next="list.next" @go="list.go" @refresh="list.refresh" />
  </div>
</template>
