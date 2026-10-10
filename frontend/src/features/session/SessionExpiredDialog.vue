<script setup lang="ts">
import { computed } from "vue";
import { useQueryClient } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { Dialog } from "@/shared/ui";
import LoginForm from "./LoginForm.vue";
import { useSessionStore } from "./store";

/**
 * Re-authentication in place when the session expires mid-work, so unsaved
 * drafts on the page survive. Retry the failed action after signing in.
 */
const session = useSessionStore();
const queryClient = useQueryClient();
const { t } = useI18n();
const open = computed(() => session.status === "expired");

async function onSignedIn() {
  await queryClient.invalidateQueries();
}
</script>

<template>
  <Dialog
    :open="open"
    persistent
    size="sm"
    :title="t('session.expired.title')"
    :description="t('session.expired.description')"
  >
    <LoginForm @success="onSignedIn" />
  </Dialog>
</template>
