<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { computed } from "vue";
import FieldReset from "./FieldReset.vue";
import { useConfiguration } from "../composables/useConfiguration";
import { errorText, t } from "../i18n";
const props = defineProps<{ application: string }>();
const {
  draft,
  configuration,
  unsets,
  loading,
  saving,
  error,
  load,
  save,
  saved,
  mark,
  restore,
  modified,
} = useConfiguration<{ cache_ttl_seconds: number }>(
  computed(() => `apps/${props.application}/configuration`),
  "",
  ["cache_ttl_seconds"],
);
</script>
<template>
  <section class="panel">
    <h2>{{ t("Metadata freshness") }}</h2>
    <p v-if="saved" class="notice" role="status">
      {{ t("Channel TTL saved") }}
    </p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <p v-if="configuration?.template_missing" class="notice">
      {{ t("Template unavailable; the last accepted defaults remain in use.") }}
    </p>
    <form class="ttl-form" @submit.prevent="save">
      <div class="resettable-field">
      <label
        >{{ t("Channel TTL (seconds)")
        }}<input
          :value="draft?.cache_ttl_seconds"
          type="number"
          min="1"
          max="86400"
          required
          :disabled="loading || saving || !draft"
          @input="
            draft &&
            ((draft.cache_ttl_seconds = Number(
              ($event.target as HTMLInputElement).value,
            )),
            mark('cache_ttl_seconds'))
          "
      /></label>
      <FieldReset
        :configuration="configuration"
        path="cache_ttl_seconds"
        :label="t('Channel TTL (seconds)')"
        :modified="modified('cache_ttl_seconds')"
        :restored="unsets.has('cache_ttl_seconds')"
        :disabled="loading || saving"
        @reset="restore('cache_ttl_seconds')"
      />
      </div>
      <div class="form-actions">
        <button :disabled="saving || loading || !draft">
          {{ saving ? t("Saving…") : t("Save TTL") }}</button
        ><IconButton
          class="secondary"
          type="button"
          :disabled="saving || loading"
          @click="load()"
          icon="refresh"
          :label="t('Reload')"
        />
      </div>
    </form>
  </section>
</template>
