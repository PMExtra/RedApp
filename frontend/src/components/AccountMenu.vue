<script setup lang="ts">
import { nextTick } from "vue";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import { usePopover } from "../composables/usePopover";
defineProps<{ busy?: boolean }>();
const emit = defineEmits<{ password: []; logout: [] }>();
const { id, open, root, trigger, close, show } = usePopover();
async function toggle() {
  if (open.value) close();
  else show();
  if (open.value) {
    await nextTick();
    root.value?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
  }
}
function focusout(event: FocusEvent) {
  if (!root.value?.contains(event.relatedTarget as Node)) close();
}
function key(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    close(true);
    return;
  }
  const items = [
    ...(root.value?.querySelectorAll<HTMLButtonElement>(
      '[role="menuitem"]:not(:disabled)',
    ) || []),
  ];
  const index = items.indexOf(document.activeElement as HTMLButtonElement);
  if (
    ["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key) &&
    items.length
  ) {
    event.preventDefault();
    const next =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? items.length - 1
          : (index + (event.key === "ArrowDown" ? 1 : -1) + items.length) %
            items.length;
    items[next]?.focus();
  }
}
function select(action: "password" | "logout") {
  close(true);
  if (action === "password") emit("password");
  else emit("logout");
}
</script>
<template>
  <div ref="root" class="account-control" @focusout="focusout" @keydown="key">
    <button
      ref="trigger"
      class="account-trigger popover-trigger secondary"
      type="button"
      :aria-label="t('Account')"
      aria-haspopup="menu"
      :aria-expanded="open"
      :aria-controls="open ? `${id}-menu` : undefined"
      :disabled="busy"
      @click="toggle"
      @keydown.down.prevent.stop="!open && toggle()"
    >
      <Icon name="user" /><span>{{ t("Administrator") }}</span
      ><Icon name="chevron" :size="14" />
    </button>
    <div
      v-if="open"
      :id="`${id}-menu`"
      role="menu"
      :aria-label="t('Account actions')"
      class="account-menu popover-panel"
    >
      <button role="menuitem" type="button" @click="select('password')">
        <Icon name="lock" />{{ t("Change password") }}</button
      ><button
        role="menuitem"
        type="button"
        :disabled="busy"
        @click="select('logout')"
      >
        <Icon name="exit" />{{ t("Sign out") }}
      </button>
    </div>
  </div>
</template>
