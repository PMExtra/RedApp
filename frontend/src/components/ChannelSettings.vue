<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { computed } from "vue";
import { appAPI } from "../bootstrap";
import { useSetting } from "../composables/useSetting";
import { errorText, t } from "../i18n";
const props = defineProps<{ application: string }>();
const { draft, loading, saving, error, load, save, saved } = useSetting<{ channel_ttl_seconds: number }>(
  computed(() => `${appAPI(props.application)}/settings`),
);
</script>
<template>
  <section class="panel">
    <h2>{{ t("Metadata freshness") }}</h2>
    <p v-if="saved" class="notice" role="status">{{ t("Channel TTL saved") }}</p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <form class="ttl-form" @submit.prevent="save">
      <label>{{ t("Channel TTL (seconds)") }}<input :value="draft?.channel_ttl_seconds" type="number" min="1" max="86400" required :disabled="loading || saving || !draft" @input="draft && (draft.channel_ttl_seconds = Number(($event.target as HTMLInputElement).value))" /></label>
      <div class="form-actions"><button :disabled="saving || loading || !draft">{{ saving ? t("Saving…") : t("Save TTL") }}</button><IconButton class="secondary" type="button" :disabled="saving || loading" @click="load()" icon="refresh" :label="t('Reload')" /></div>
    </form>
  </section>
</template>
