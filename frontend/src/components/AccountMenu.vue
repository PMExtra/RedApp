<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from "vue";
import { t } from "../i18n";
import Icon from "./Icon.vue";
defineProps<{ busy?: boolean }>();
const emit = defineEmits<{ password: []; logout: [] }>();
const open = ref(false),
  root = ref<HTMLElement>(),
  trigger = ref<HTMLButtonElement>();
function close(focus = false) {
  open.value = false;
  if (focus) trigger.value?.focus();
}
async function toggle() {
  open.value = !open.value;
  if (open.value) {
    await nextTick();
    root.value?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
  }
}
function outside(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) close();
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
onMounted(() => document.addEventListener("pointerdown", outside));
onUnmounted(() => document.removeEventListener("pointerdown", outside));
</script>
<template>
  <div ref="root" class="account-control" @focusout="focusout" @keydown="key">
    <button
      ref="trigger"
      class="account-trigger secondary"
      type="button"
      :aria-label="t('Account')"
      aria-haspopup="menu"
      :aria-expanded="open"
      aria-controls="account-menu"
      :disabled="busy"
      @click="toggle"
      @keydown.down.prevent.stop="!open && toggle()"
    >
      <Icon name="user" /><span>{{ t("Administrator") }}</span
      ><Icon name="chevron" :size="14" />
    </button>
    <div
      v-if="open"
      id="account-menu"
      role="menu"
      :aria-label="t('Account actions')"
      class="account-menu"
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
