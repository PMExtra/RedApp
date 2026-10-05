<script setup lang="ts">
import IconButton from "../components/IconButton.vue";
import AutoRefresh from "../components/AutoRefresh.vue";
import { computed, reactive, ref } from "vue";
import { appAPI } from "../bootstrap";
import { type DistributionEvent } from "../api";
import { usePagedCollection } from "../composables/usePagedCollection";
import { errorText, t } from "../i18n";
import Events from "../components/Events.vue";
import PageNavigation from "../components/PageNavigation.vue";
const props = defineProps<{ application?: string }>();
const automatic = ref(true);
const page = reactive(
  usePagedCollection<DistributionEvent>(
    computed(() =>
      props.application ? `${appAPI(props.application)}/events` : "events",
    ),
    automatic,
  ),
);
</script>
<template>
  <div class="page-heading">
    <h1>{{ t("Events") }}</h1>
    <AutoRefresh v-model="automatic" />
  </div>
  <div v-if="page.error" class="error" role="alert">
    {{ errorText(page.error)
    }}<small v-if="page.loaded">{{
      t("Showing the last successful snapshot.")
    }}</small
    ><IconButton
      class="secondary"
      :disabled="page.loading"
      @click="page.refresh"
      icon="refresh"
      :label="t('Retry')"
    />
  </div>
  <p v-if="!page.loaded && page.loading" role="status">{{ t("Loading…") }}</p>
  <Events v-if="page.loaded" :events="page.items" />
  <PageNavigation
    :label="t('Event pages')"
    :page="page.page"
    :previous="page.previousAvailable"
    :next="page.nextAvailable"
    :loading="page.loading"
    @previous="page.previous"
    @next="page.next"
    @refresh="page.refresh"
  />
</template>
