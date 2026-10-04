<script setup lang="ts">
import { onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { loginSession } from "../session";
import { errorText, t } from "../i18n";
import Icon from "../components/Icon.vue";
const password = ref(""),
  busy = ref(false),
  error = ref<unknown>();
import { adminReturnPath } from "../router";
const router = useRouter(),
  route = useRoute();
let disposed = false;
const controller = new AbortController();
async function login() {
  if (busy.value) return;
  busy.value = true;
  error.value = undefined;
  try {
    await loginSession(password.value, controller.signal);
    if (!disposed) {
      password.value = "";
      await router.replace(adminReturnPath(route.query.returnTo));
    }
  } catch (reason) {
    if (!disposed) error.value = reason;
  } finally {
    busy.value = false;
  }
}
onUnmounted(() => {
  disposed = true;
  controller.abort();
  password.value = "";
});
</script>
<template>
  <section class="login panel">
    <div class="login-mark"><Icon name="lock" :size="24" /></div>
    <span class="eyebrow">{{ t("Administrator access") }}</span>
    <h1>{{ t("Sign in to RedApp") }}</h1>
    <p class="muted">
      {{
        t(
          "Use the password from the first initialization logs. Change it after signing in.",
        )
      }}
    </p>
    <p v-if="error" class="error" role="alert">{{ errorText(error) }}</p>
    <form @submit.prevent="login">
      <label
        >{{ t("Password")
        }}<input
          v-model="password"
          type="password"
          autocomplete="current-password"
          :disabled="busy"
          required /></label
      ><button :disabled="busy">
        {{ busy ? t("Signing in…") : t("Sign in") }}<Icon name="arrow" />
      </button>
    </form>
  </section>
</template>
