<script setup lang="ts">
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { Card } from "@/shared/ui";
import RetentionPolicyForm from "./RetentionPolicyForm.vue";
import RetentionRun from "./RetentionRun.vue";
import RetentionStatus from "./RetentionStatus.vue";

/**
 * Keep-latest retention of a release application: the saved policy, the last
 * automatic run, and a manual run as preview → review → execute.
 */
defineProps<{
  vendor: string;
  app: string;
  readOnly?: boolean;
  /** The application or vendor is disabled: manual runs are refused. */
  inactive?: boolean;
}>();
const { t } = useI18n();
// Unsaved policy changes: a manual run uses the saved policy, so save first.
const dirty = ref(false);
</script>

<template>
  <Card :title="t('retention.title')" :description="t('retention.description')">
    <div class="flex flex-col gap-6">
      <RetentionPolicyForm
        :vendor="vendor"
        :app="app"
        :read-only="readOnly"
        @update:dirty="dirty = $event"
      />
      <RetentionStatus :vendor="vendor" :app="app" />
      <RetentionRun
        :vendor="vendor"
        :app="app"
        :read-only="readOnly"
        :inactive="inactive"
        :dirty="dirty"
      />
    </div>
  </Card>
</template>
