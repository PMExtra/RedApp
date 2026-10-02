<script setup lang="ts">
import { computed, ref } from "vue";
import { language, t } from "../i18n";
import Icon from "./Icon.vue";
import type { Application } from "../bootstrap";
const props = defineProps<{ origin: string; application: Application }>();
const commands = computed(() =>
  props.application.installers.map((item) => {
    const url = `${props.origin}/${props.application.id}/${item.file}`;
    return {
      name: item.shell === "powershell" ? "PowerShell" : "Shell",
      command:
        item.shell === "powershell"
          ? `irm '${url}' | iex`
          : `curl -fsSL '${url}' | ${item.shell}`,
    };
  }),
);
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
          {{ t("Install {name}", { name: application.name[language] }) }}
        </h2>
      </div>
    </div>
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
          "This command downloads and runs an installer. Use a service you trust.",
        )
      }}
    </p>
    <p class="muted small-text">{{ application.update_policy[language] }}</p>
  </section>
</template>
