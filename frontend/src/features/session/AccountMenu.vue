<script setup lang="ts">
import { ref } from "vue";
import { ExternalLink, KeyRound, LogOut, UserRound } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { confirmDiscardDrafts } from "@/shared/forms";
import { notifyError } from "@/shared/lib";
import { Button, DropdownMenu, DropdownMenuItem, DropdownMenuSeparator } from "@/shared/ui";
import PasswordDialog from "./PasswordDialog.vue";
import { useSessionStore } from "./store";

const { t } = useI18n();
const session = useSessionStore();
const passwordOpen = ref(false);

async function signOut() {
  if (!(await confirmDiscardDrafts())) return;
  try {
    // The admin shell moves to the sign-in page when the session ends.
    await session.signOut();
  } catch (error) {
    notifyError(error);
  }
}
</script>

<template>
  <DropdownMenu>
    <template #trigger>
      <Button variant="ghost" size="sm">
        <UserRound aria-hidden="true" />
        <span class="hidden sm:inline">{{ t("session.account.menu") }}</span>
      </Button>
    </template>
    <DropdownMenuItem @select="passwordOpen = true">
      <KeyRound aria-hidden="true" />
      {{ t("session.account.changePassword") }}
    </DropdownMenuItem>
    <DropdownMenuItem as-child>
      <a href="/">
        <ExternalLink aria-hidden="true" />
        {{ t("session.account.publicSite") }}
      </a>
    </DropdownMenuItem>
    <DropdownMenuSeparator class="my-1 h-px bg-border" />
    <DropdownMenuItem @select="signOut">
      <LogOut aria-hidden="true" />
      {{ t("session.account.signOut") }}
    </DropdownMenuItem>
  </DropdownMenu>
  <PasswordDialog v-model:open="passwordOpen" />
</template>
