<script setup lang="ts">
import { computed, ref } from "vue";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = withDefaults(
  defineProps<{ origin: string; application?: string }>(),
  { application: "codex" },
);
const isClaude = computed(() => props.application === "claude-code");
const installerBase = computed(
  () => props.origin + (isClaude.value ? "/claude-code" : ""),
);
const commands = computed(() => [
  {
    name: "Shell",
    command: `curl -fsSL '${installerBase.value}/install.sh' | ${isClaude.value ? "bash" : "sh"}`,
  },
  {
    name: "PowerShell",
    command: `irm '${installerBase.value}/install.ps1' | iex`,
  },
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
        <h2 id="install-title">
          {{ isClaude ? t("Install Claude Code") : t("Install Codex") }}
        </h2>
      </div>
    </div>
    <p class="muted">
      {{
        isClaude
          ? t(
              "Installs latest. Use stable or a version to pin your installation.",
            )
          : t("Uses CODEX_RELEASE if set, otherwise latest.")
      }}
    </p>
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
        isClaude
          ? t(
              "The service verifies Anthropic’s signed manifest; the installer verifies the binary hash. The managed launcher disables official updates. Signing in and using Claude Code still requires its provider’s services.",
            )
          : t(
              "This command downloads and runs an installer that verifies package hashes. Use a service you trust. Signing in and using Codex still requires its provider’s services.",
            )
      }}
    </p>
    <p v-if="isClaude" class="muted small-text">
      {{
        t(
          "Trust this HTTPS service. Run claude through its managed launcher; executing the version binary directly bypasses update control. Upgrade by rerunning this installer.",
        )
      }}
    </p>
  </section>
</template>
