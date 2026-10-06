<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { api, isCancellation } from "../api";
import { directoryIcon, applicationNavigation, type Vendor } from "../directory";
import { errorText, language, t } from "../i18n";
import EntityIcon from "../components/EntityIcon.vue";
import IconButton from "../components/IconButton.vue";
const route = useRoute();
const id = computed(() => String(route.params.vendor));
const vendor = ref<Vendor>(), error = ref<unknown>(), loading = ref(false);
let ticket = 0, controller: AbortController | undefined;
async function load() {
  controller?.abort();
  const attempt = ++ticket, request = new AbortController();
  controller = request;
  vendor.value = undefined;
  error.value = undefined;
  loading.value = true;
  try {
    const result = await api<{ vendor: Vendor }>(`vendors/${id.value}`, undefined, request.signal);
    if (attempt === ticket) vendor.value = result.vendor;
  } catch (reason) {
    if (attempt === ticket && !isCancellation(reason)) error.value = reason;
  } finally {
    if (attempt === ticket) { loading.value = false; controller = undefined; }
  }
}
function updated(value: Vendor) {
  if (value.uid === vendor.value?.uid) vendor.value = value;
}
watch(id, load, { immediate: true, flush: "sync" });
onUnmounted(() => { ticket++; controller?.abort(); });
</script>
<template>
  <p v-if="loading" role="status">{{ t("Loading…") }}</p>
  <p v-else-if="error" class="error" role="alert">
    {{ errorText(error) }}<IconButton icon="refresh" :label="t('Retry')" @click="load" />
  </p>
  <template v-else-if="vendor">
    <nav class="breadcrumbs" :aria-label="t('Vendor sections')">
      <RouterLink to="/admin/vendors">{{ t("Vendors and applications") }}</RouterLink>
      <span aria-hidden="true">/</span><span>{{ vendor.name[language] }}</span>
    </nav>
    <header class="application-header vendor-header">
      <div class="application-header-identity">
        <EntityIcon :src="directoryIcon(vendor.icon)" size="detail" />
        <div><h1>{{ vendor.name[language] }}</h1><p class="application-meta">
          <span>{{ vendor.id }}</span><span class="state-label">{{ vendor.deleted_at ? t("Deleted") : vendor.enabled ? t("Enabled") : t("Disabled") }}</span>
        </p></div>
      </div>
    </header>
    <nav class="application-tabs" :aria-label="t('Vendor sections')">
      <RouterLink :to="`/admin/vendors/${vendor.id}/settings`">{{ t("Vendor settings") }}</RouterLink>
      <RouterLink :to="`/admin/vendors/${vendor.id}/apps`">{{ applicationNavigation[language] }}</RouterLink>
    </nav>
    <RouterView :key="vendor.uid" :vendor="vendor" @updated="updated" />
  </template>
</template>
