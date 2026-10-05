<script setup lang="ts">
import IconButton from "./IconButton.vue";
import { computed } from "vue";
import { useSetting } from "../composables/useSetting";
import { invalidateBootstrap, loadBootstrap } from "../bootstrap";
import { errorText, t } from "../i18n";
import type { LocalizedText } from "../site";
const props = defineProps<{ application: string; readonly?: boolean }>();
const { draft, loading, saving, error, saved, load, save } =
  useSetting<LocalizedText>(
    computed(() => `apps/${props.application}/instructions`),
    (_value, isSaved) => {
      if (isSaved) {
        invalidateBootstrap();
        void loadBootstrap();
      }
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
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">{{ t("Changes saved.") }}</p>
    <p v-if="loading" role="status">{{ t("Loading…") }}</p>
    <form @submit.prevent="save">
      <fieldset :disabled="readonly || loading || saving">
        <div v-if="draft" class="two-columns">
          <label
            >English<textarea
              v-model="draft.en"
              name="instructions-en"
              lang="en"
              maxlength="12000"
              rows="9"
            /></label
          ><label
            >简体中文<textarea
              v-model="draft['zh-CN']"
              name="instructions-zh-CN"
              lang="zh-CN"
              maxlength="12000"
              rows="9"
            />
          </label>
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
