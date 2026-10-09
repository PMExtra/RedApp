<script setup lang="ts">
import { t } from "../i18n";
defineProps<{ application: boolean; recordKey: string; busy: boolean }>();
defineEmits<{ confirm: []; cancel: [] }>();
</script>
<template>
  <div class="delete-review" role="group" :aria-label="t('Confirm deletion')">
    <strong>{{ recordKey }}</strong>
    <p>{{ t(application ? 'Permanently delete this record and its files, settings and history? Active downloads, uploads and background tasks of this application will be interrupted. This cannot be undone.' : 'Permanently delete this record and its files, settings and history? This cannot be undone.') }}</p>
    <p v-if="!application" class="muted">{{ t('Delete applications under this vendor first.') }}</p>
    <button class="danger" :disabled="busy" @click="$emit('confirm')">{{ t('Confirm deletion') }}</button>
    <button class="secondary" :disabled="busy" @click="$emit('cancel')">{{ t('Cancel') }}</button>
  </div>
</template>
