<script setup lang="ts">
import { computed, reactive } from "vue";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import {
  applicationPath,
  type ManagedApplication,
  type Vendor,
} from "../directory";
import { errorText, language, t } from "../i18n";
import PageNavigation from "./PageNavigation.vue";
const props = defineProps<{ vendor: Vendor; query: string; state: string }>();
const list = reactive(
  useNumberedCollection<ManagedApplication>(
    computed(
      () =>
        `vendors/${props.vendor.id}/apps?${new URLSearchParams({ q: props.query, state: props.state })}`,
    ),
    20,
  ),
);
</script>
<template>
  <div class="expanded-apps">
    <p v-if="list.error" class="error" role="alert">
      {{ errorText(list.error)
      }}<button class="secondary" @click="list.refresh">
        {{ t("Retry") }}
      </button>
    </p>
    <p v-if="list.loading && !list.loaded" role="status">{{ t("Loading…") }}</p>
    <ul class="directory-apps">
      <li v-for="app in list.items" :key="app.uid">
        <RouterLink
          :to="applicationPath(app, app.deleted_at ? 'settings' : undefined)"
          ><img
            v-if="app.icon"
            :src="app.icon"
            alt=""
            width="24"
            height="24"
          /><span>{{ app.name[language] }}</span></RouterLink
        >
        <span v-if="app.deleted_at" class="state-label">{{ t("Deleted") }}</span
        ><span
          v-else-if="!app.enabled || !vendor.enabled"
          class="state-label"
          >{{ t("Disabled") }}</span
        >
      </li>
    </ul>
    <PageNavigation
      :label="t('Application pages')"
      :page="list.page"
      :total="list.total"
      :total-pages="list.totalPages"
      :previous="list.previousAvailable"
      :next="list.nextAvailable"
      :loading="list.loading"
      @previous="list.previous"
      @next="list.next"
      @go="list.go"
      @refresh="list.refresh"
    />
  </div>
</template>
