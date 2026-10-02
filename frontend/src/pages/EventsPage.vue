<script setup lang="ts">
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
    <button
      class="secondary auto-refresh"
      :aria-pressed="automatic"
      :title="t('Refresh every 5 seconds')"
      @click="automatic = !automatic"
    >
      {{ t("Auto refresh") }} {{ automatic ? t("On") : t("Off") }}
    </button>
  </div>
  <div v-if="page.error" class="error" role="alert">
    {{ errorText(page.error)
    }}<small v-if="page.loaded">{{
      t("Showing the last successful snapshot.")
    }}</small
    ><button class="secondary" :disabled="page.loading" @click="page.refresh">
      {{ t("Retry") }}
    </button>
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
