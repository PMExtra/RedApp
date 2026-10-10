<script setup lang="ts">
import { computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { LoginForm, safeReturnPath, useSessionStore } from "@/features/session";
import { describeError } from "@/shared/api";
import { Alert, Button, Card } from "@/shared/ui";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const checkFailure = computed(() =>
  session.checkError ? describeError(session.checkError).message : null,
);

async function onSuccess() {
  await router.replace(safeReturnPath(route.query.returnTo));
}

// Leave once signed in: after the form succeeds or a retried check finds a session.
watch(
  () => session.signedIn,
  async (signedIn) => {
    if (signedIn) await onSuccess();
  },
);
</script>

<template>
  <div class="mx-auto flex max-w-sm flex-col gap-4 pt-8">
    <Alert v-if="session.notice" tone="info">{{ t(`session.notices.${session.notice}`) }}</Alert>
    <Alert v-if="checkFailure" tone="danger" :title="t('session.checkFailed')">
      {{ checkFailure }}
      <template #actions>
        <Button size="sm" @click="session.check()">{{ t("common.actions.retry") }}</Button>
      </template>
    </Alert>
    <Card>
      <div class="flex flex-col gap-4">
        <div class="flex flex-col gap-1">
          <h1 class="text-xl font-semibold">{{ t("session.login.title") }}</h1>
          <p class="text-sm text-muted">{{ t("session.login.description") }}</p>
        </div>
        <LoginForm />
      </div>
    </Card>
  </div>
</template>
