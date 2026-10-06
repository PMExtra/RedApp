<script setup lang="ts">
import EntityIcon from "./EntityIcon.vue";
import IconButton from "./IconButton.vue";
import { ref, watch } from "vue";
import {
  directoryIcon,
  applicationPath,
  type ManagedApplication,
  type Vendor,
} from "../directory";
import { language, t } from "../i18n";
import DirectoryApps from "./DirectoryApps.vue";
import Icon from "./Icon.vue";
export interface Card extends Vendor {
  apps: ManagedApplication[];
  app_total: number;
}
const props = defineProps<{ vendor: Card; query: string; state: string }>();
const expanded = ref(false);
watch(
  () => props.query,
  () => (expanded.value = false),
);
</script>
<template>
  <article
    class="panel vendor-card"
    :class="{ 'is-disabled': !vendor.enabled }"
  >
    <div class="directory-heading">
      <EntityIcon :src="directoryIcon(vendor.icon)" size="vendor" />
      <h2>
        <RouterLink :to="`/admin/vendors/${vendor.id}/settings`">{{
          vendor.name[language]
        }}</RouterLink
        ><span class="app-count">{{
          t("{count} applications", { count: vendor.app_total })
        }}</span>
      </h2>
      <span v-if="vendor.deleted_at" class="state-label">{{
        t("Deleted")
      }}</span
      ><span v-else-if="!vendor.enabled" class="state-label">{{
        t("Disabled")
      }}</span>
    </div>
    <p v-if="vendor.description[language]" class="muted">
      {{ vendor.description[language] }}
    </p>
    <DirectoryApps
      v-if="expanded"
      :vendor="vendor"
      :query="query"
      :state="state"
    />
    <ul v-else class="directory-apps">
      <li v-if="!vendor.deleted_at" class="add-application">
        <RouterLink
          :to="`/admin/vendors/${vendor.id}/apps/new`"
          :aria-label="t('Add application')"
          :title="t('Add application')"
          ><Icon name="plus" :size="24"
        /></RouterLink>
      </li>
      <li
        v-for="app in vendor.apps"
        :key="app.uid"
        :class="{ 'is-disabled': !app.enabled || !vendor.enabled }"
      >
        <RouterLink
          :to="applicationPath(app, app.deleted_at ? 'settings' : undefined)"
          ><EntityIcon :src="directoryIcon(app.icon)" size="tile" /><span>{{
            app.name[language]
          }}</span></RouterLink
        >
        <span v-if="app.deleted_at" class="state-label">{{ t("Deleted") }}</span
        ><span
          v-else-if="!app.enabled || !vendor.enabled"
          class="state-label"
          >{{
            app.enabled && !vendor.enabled
              ? t("Disabled by vendor")
              : t("Disabled")
          }}</span
        >
      </li>
    </ul>
    <div class="vendor-actions">
      <IconButton
        v-if="vendor.app_total > 5"
        class="secondary"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
        icon="chevron"
        :label="expanded ? t('Collapse') : t('Show all applications')"
      />
    </div>
  </article>
</template>
