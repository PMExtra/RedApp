<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from "vue";
import { api, ApiError } from "../api";
import { errorText, t } from "../i18n";
import Icon from "./Icon.vue";
const emit = defineEmits<{ close: []; changed: []; error: [unknown] }>();
const dialog = ref<HTMLDialogElement>(),
  oldPassword = ref(""),
  newPassword = ref(""),
  confirm = ref(""),
  busy = ref(false),
  error = ref<unknown>(),
  validation = ref<
    "Passwords do not match." | "The new password is too short."
  >();
let controller: AbortController | undefined,
  disposed = false;
const previous = document.activeElement as HTMLElement | null;
function close() {
  if (!busy.value) emit("close");
}
async function save() {
  if (busy.value) return;
  validation.value = undefined;
  error.value = undefined;
  if (newPassword.value !== confirm.value) {
    validation.value = "Passwords do not match.";
    return;
  }
  if (new TextEncoder().encode(newPassword.value).length < 12) {
    validation.value = "The new password is too short.";
    return;
  }
  busy.value = true;
  controller = new AbortController();
  try {
    await api(
      "password",
      { old: oldPassword.value, new: newPassword.value },
      controller.signal,
    );
    if (!disposed) {
      oldPassword.value = "";
      newPassword.value = "";
      confirm.value = "";
      emit("changed");
    }
  } catch (reason) {
    if (!disposed) {
      error.value = reason;
      if (reason instanceof ApiError && reason.status === 401)
        emit("error", reason);
    }
  } finally {
    busy.value = false;
  }
}
onMounted(() => dialog.value?.showModal());
onBeforeUnmount(() => {
  disposed = true;
  controller?.abort();
  oldPassword.value = "";
  newPassword.value = "";
  confirm.value = "";
  dialog.value?.close();
  previous?.focus();
});
</script>
<template>
  <dialog
    ref="dialog"
    class="password-dialog"
    aria-labelledby="password-title"
    @cancel.prevent="close"
  >
    <div class="dialog-heading">
      <div>
        <span class="eyebrow">{{ t("Account") }}</span>
        <h2 id="password-title">{{ t("Change password") }}</h2>
      </div>
      <button
        class="icon-button secondary"
        :aria-label="t('Close')"
        :disabled="busy"
        @click="close"
      >
        <Icon name="close" />
      </button>
    </div>
    <p class="muted">
      {{
        t(
          "Your new password must contain at least 12 UTF-8 bytes. All sessions will be signed out.",
        )
      }}
    </p>
    <p v-if="validation || error" class="error" role="alert">
      {{ validation ? t(validation) : errorText(error) }}
    </p>
    <form @submit.prevent="save">
      <fieldset :disabled="busy">
        <label
          >{{ t("Current password")
          }}<input
            v-model="oldPassword"
            type="password"
            autocomplete="current-password"
            required
            autofocus /></label
        ><label
          >{{ t("New password")
          }}<input
            v-model="newPassword"
            type="password"
            autocomplete="new-password"
            required /></label
        ><label
          >{{ t("Confirm new password")
          }}<input
            v-model="confirm"
            type="password"
            autocomplete="new-password"
            required
        /></label>
      </fieldset>
      <div class="dialog-actions">
        <button class="secondary" type="button" :disabled="busy" @click="close">
          {{ t("Cancel") }}</button
        ><button :disabled="busy">
          {{ busy ? t("Saving…") : t("Change password and sign out") }}
        </button>
      </div>
    </form>
  </dialog>
</template>
