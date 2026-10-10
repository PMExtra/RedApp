<script setup lang="ts">
import { useI18n } from "vue-i18n";
import Alert from "./Alert.vue";
import Button from "./Button.vue";

/**
 * Shown when a save returned 409 REVISION_CONFLICT (see useRevisionedMutation).
 * The draft stays in the form; `reload` loads the latest version.
 */
defineProps<{ reloading?: boolean }>();
const emit = defineEmits<{ reload: [] }>();
const { t } = useI18n();
</script>

<template>
  <Alert tone="warning" :title="t('ui.conflict.title')">
    {{ t("ui.conflict.description") }}
    <template #actions>
      <Button size="sm" :loading="reloading" @click="emit('reload')">
        {{ t("ui.conflict.reload") }}
      </Button>
    </template>
  </Alert>
</template>
