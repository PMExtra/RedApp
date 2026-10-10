<script setup lang="ts">
import { computed } from "vue";
import { pendingConfirm, settleConfirm } from "@/shared/lib/confirm";
import ConfirmDialog from "./ConfirmDialog.vue";

/** Renders `confirm()` requests; mounted once by each app shell. */
const pending = pendingConfirm();
const open = computed(() => pending.value !== null);
</script>

<template>
  <ConfirmDialog
    v-if="pending"
    :open="open"
    :title="pending.options.title"
    :description="pending.options.description"
    :confirm-label="pending.options.confirmLabel"
    :cancel-label="pending.options.cancelLabel"
    :tone="pending.options.tone"
    @confirm="settleConfirm(true)"
    @cancel="settleConfirm(false)"
  />
</template>
