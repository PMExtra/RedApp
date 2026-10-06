<script setup lang="ts">
import { computed } from "vue";
import { useSetting } from "../composables/useSetting";
import { errorText, t } from "../i18n";
import IconButton from "./IconButton.vue";
const props = defineProps<{ endpoint: string; readonly?: boolean }>();
const { draft, loading, saving, error, saved, load, save } = useSetting<{ text: string }>(computed(() => props.endpoint));
</script>
<template>
  <section class="panel admin-notes-editor">
    <h2>{{ t('Admin Notes') }}</h2>
    <p class="muted">{{ t('Private maintenance notes for administrators. Not shown on public pages.') }}</p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <p v-if="saved" class="notice" role="status">{{ t('Changes saved.') }}</p>
    <p v-if="loading" role="status">{{ t('Loading…') }}</p>
    <form @submit.prevent="save">
      <fieldset :disabled="readonly || loading || saving">
        <label v-if="draft">{{ t('Admin Notes') }}<textarea v-model="draft.text" name="admin-notes" rows="14" maxlength="12000" /></label>
        <div class="form-actions">
          <button :disabled="!draft">{{ saving ? t('Saving…') : t('Save notes') }}</button>
          <IconButton type="button" class="secondary" icon="refresh" :label="t('Reload')" @click="load()" />
        </div>
      </fieldset>
    </form>
  </section>
</template>
