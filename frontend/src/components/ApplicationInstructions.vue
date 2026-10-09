<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { computed } from "vue";
import { useConfiguration } from "../composables/useConfiguration";
import FieldReset from "./FieldReset.vue";
import { invalidateBootstrap, loadBootstrap } from "../bootstrap";
import { errorText, t } from "../i18n";
import type { LocalizedText } from "../site";
const variableHint = computed(() =>
  t(
    "Use {{base_url}} for the public address, {{app_path}} for /vendor/app, and {{latest_version}} for the latest known version or <version> when unavailable.",
  ),
);
const props = defineProps<{ application: string; readonly?: boolean }>();
const {
  draft,
  configuration,
  unsets,
  loading,
  saving,
  error,
  saved,
  load,
  save,
  mark,
  restore,
  modified,
} = useConfiguration<LocalizedText>(
  computed(() => `apps/${props.application}/configuration`),
  "instructions",
  ["en", "zh-CN"],
  () => {
    invalidateBootstrap();
    void loadBootstrap();
  },
);
</script>
<template>
  <section class="panel application-instructions-editor">
    <h2>{{ t("Usage instructions") }}</h2>
    <p class="muted">
      {{
        t(
          "Markdown, HTML, JavaScript and external resources are supported. Only administrators can edit these instructions.",
        )
      }}
    </p>
    <p class="muted small-text">{{ variableHint }}</p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <p v-if="configuration?.template_missing" class="notice">
      {{ t("Template unavailable; the last accepted defaults remain in use.") }}
    </p>
    <form @submit.prevent="save">
      <fieldset :disabled="readonly || loading || saving">
        <div v-if="draft" class="two-columns">
          <div
            v-for="locale in ['en', 'zh-CN'] as const"
            :key="locale"
            class="resettable-field"
          >
            <label
              >{{ locale === "en" ? "English" : "简体中文"
              }}<textarea
                v-model="draft[locale]"
                @input="mark(locale)"
                :name="`instructions-${locale}`"
                :lang="locale"
                maxlength="12000"
                rows="9"
            /></label>
            <FieldReset
              :configuration="configuration"
              :path="`instructions.${locale}`"
              :label="locale === 'en' ? 'English' : '简体中文'"
              :modified="modified(locale)"
              :restored="unsets.has(locale)"
              @reset="restore(locale)"
            />
          </div>
        </div>
        <div class="form-actions">
          <button :disabled="!draft">
            {{ saving ? t("Saving…") : t("Save instructions") }}</button
          ><IconButton
            type="button"
            class="secondary"
            @click="load()"
            icon="refresh"
            :label="t('Reload')"
          />
        </div>
      </fieldset>
    </form>
  </section>
</template>
