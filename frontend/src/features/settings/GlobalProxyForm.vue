<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { ProxyFields } from "@/features/proxy";
import { isApiError, type Schema } from "@/shared/api";
import { useDirtyGuard } from "@/shared/forms";
import { toast } from "@/shared/lib";
import { AsyncState, Button, Card, RevisionConflictAlert } from "@/shared/ui";
import {
  useGlobalProxySettings,
  useSaveGlobalProxy,
  type GlobalProxySettings,
  type GlobalProxyState,
} from "./queries";

// `ProxyFields`' model (the spec's ProxyConfig; `inherit` is not offered globally).
type ProxyValue = Schema<"ProxyConfig">;

const { t } = useI18n();
const settings = useGlobalProxySettings();
const save = useSaveGlobalProxy(settings.data);
const state = computed(() => settings.data.value);

function draftOf(value: GlobalProxyState): ProxyValue {
  return value.mode === "url" ? { mode: "url", url: value.url ?? "" } : { mode: "direct" };
}
function same(a: ProxyValue, b: ProxyValue) {
  return a.mode === b.mode && (a.mode !== "url" || a.url === b.url);
}

// The saved value the draft was made from; `null` until loaded.
const base = ref<ProxyValue | null>(null);
const draft = ref<ProxyValue>({ mode: "direct" });
const dirty = computed(() => base.value !== null && !same(draft.value, base.value));
useDirtyGuard(dirty);

function adopt(value: GlobalProxyState) {
  base.value = draftOf(value);
  draft.value = draftOf(value);
}

// Adopt the server state unless the user has unsaved edits.
watch(
  state,
  (value) => {
    if (value && !dirty.value) adopt(value);
  },
  { immediate: true },
);

const submitted = ref(false);
const clientError = computed(() => {
  if (draft.value.mode !== "url") return undefined;
  const url = (draft.value.url ?? "").trim();
  if (!url) return t("settings.proxy.urlRequired");
  if (!/^(https?|socks5):\/\//i.test(url)) return t("settings.proxy.urlScheme");
  return undefined;
});
const urlError = computed(() => {
  if (submitted.value && clientError.value) return clientError.value;
  if (isApiError(save.error.value, "PROXY_REDACTED_MISMATCH")) {
    return t("errors.codes.PROXY_REDACTED_MISMATCH");
  }
  return undefined;
});
watch(draft, () => {
  if (isApiError(save.error.value, "PROXY_REDACTED_MISMATCH")) save.reset();
});

function submit() {
  submitted.value = true;
  if (clientError.value) return;
  const body: GlobalProxySettings =
    draft.value.mode === "url"
      ? { mode: "url", url: (draft.value.url ?? "").trim() }
      : { mode: "direct" };
  save.mutate(body, {
    onSuccess: (value) => {
      submitted.value = false;
      adopt(value);
      toast({ tone: "success", title: t("settings.proxy.saved") });
    },
  });
}

async function reload() {
  await save.reload();
  if (state.value) adopt(state.value);
}

function discard() {
  submitted.value = false;
  if (base.value) draft.value = { ...base.value };
}
</script>

<template>
  <Card :title="t('settings.proxy.title')" :description="t('settings.proxy.description')">
    <AsyncState
      :loading="settings.isPending.value"
      :error="state ? undefined : settings.error.value"
      @retry="settings.refetch()"
    >
      <form v-if="state" class="flex flex-col gap-5" novalidate @submit.prevent="submit">
        <RevisionConflictAlert v-if="save.hasConflict.value" @reload="reload" />
        <ProxyFields v-model="draft" :disabled="save.isPending.value" :url-error="urlError" />
        <p class="text-sm text-muted">
          {{
            t(state.mode === "url" ? `settings.proxy.dns.${state.dns}` : "settings.proxy.direct")
          }}
        </p>
        <div class="flex flex-wrap justify-end gap-2">
          <Button v-if="dirty" :disabled="save.isPending.value" @click="discard">
            {{ t("settings.discard") }}
          </Button>
          <Button
            type="submit"
            variant="primary"
            :loading="save.isPending.value"
            :disabled="!dirty"
          >
            {{ t("settings.proxy.save") }}
          </Button>
        </div>
      </form>
    </AsyncState>
  </Card>
</template>
