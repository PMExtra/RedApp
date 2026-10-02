<script setup lang="ts">
import { computed, ref } from "vue";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{ origin: string }>();
const commands = computed(() => [
  { name: "Shell", command: `curl -fsSL '${props.origin}/install.sh' | sh` },
  { name: "PowerShell", command: `irm '${props.origin}/install.ps1' | iex` },
]);
const canCopy =
  window.isSecureContext === true &&
  typeof navigator.clipboard?.writeText === "function";
const result = ref<"success" | "error">(),
  copying = ref(false);
async function copy(command: string) {
  if (copying.value) return;
  copying.value = true;
  result.value = undefined;
  try {
    if (!navigator.clipboard) throw Error();
    await navigator.clipboard.writeText(command);
    result.value = "success";
  } catch {
    result.value = "error";
  } finally {
    copying.value = false;
  }
}
</script>
<template>
  <section class="panel install-commands" aria-labelledby="install-title">
    <div class="section-heading">
      <div>
        <span class="eyebrow">{{ t("Run in your terminal") }}</span>
        <h2 id="install-title">{{ t("Install Codex") }}</h2>
      </div>
    </div>
    <p class="muted">{{ t("Uses CODEX_RELEASE if set, otherwise latest.") }}</p>
    <div class="command-stack">
      <article v-for="item in commands" :key="item.name" class="command">
        <div class="section-heading">
          <h3>{{ item.name }}</h3>
          <button
            v-if="canCopy"
            class="secondary copy-button"
            type="button"
            :disabled="copying"
            @click="copy(item.command)"
            :aria-label="t('Copy {name} command', { name: item.name })"
          >
            <Icon name="copy" :size="16" />{{ t("Copy") }}
          </button>
        </div>
        <pre
          tabindex="0"
          :aria-label="item.name"
        ><code>{{item.command}}</code></pre>
      </article>
    </div>
    <p
      v-if="result"
      :class="result === 'success' ? 'copy-feedback' : 'error'"
      role="status"
    >
      {{
        result === "success"
          ? t("Command copied")
          : t("Copy unavailable. Select and copy the command manually.")
      }}
    </p>
    <p class="muted small-text">
      {{
        t(
          "This command downloads and runs an installer that verifies package hashes. Use a service you trust. Signing in and using Codex still requires its provider’s services.",
        )
      }}
    </p>
  </section>
</template>
