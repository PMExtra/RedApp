<script setup lang="ts">
import { computed, useId } from "vue";
import { Plus, X } from "@lucide/vue";
import { useField, useSubmitCount } from "vee-validate";
import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { Button, Field, IconButton, Input } from "@/shared/ui";
import { isRedirectOrigin, isRedirectPrefix, MAX_REDIRECT_ALLOWLIST } from "./schemas";

/**
 * The `redirect_allowlist` of an `http-cache` form: origins and directory
 * prefixes the source may redirect to under strict redirect protection,
 * besides its own base URLs. Must be rendered inside a vee-validate form.
 */
defineProps<{ disabled?: boolean }>();
defineSlots<{ reset?: () => unknown }>();
const { t } = useI18n();
const id = useId();
type Entry = Schema<"RedirectAllowlist">[number];
// Literal URL attributes confuse vue-tsc (comment syntax); bind them.
const ORIGIN_PLACEHOLDER = "https://cdn.example.com";

const { value, setValue } = useField<Entry[] | undefined>("redirect_allowlist");
const submitCount = useSubmitCount();
const entries = computed(() => value.value ?? []);

function update(index: number, change: Partial<Entry>): void {
  setValue(entries.value.map((entry, i) => (i === index ? { ...entry, ...change } : entry)));
}

function add(): void {
  setValue([...entries.value, { origin: "", prefix: "/" }]);
}

function remove(index: number): void {
  setValue(entries.value.filter((_, i) => i !== index));
}

// Row errors are derived here: the schema reports them on nested paths,
// which are not fields of their own.
function originError(index: number): string | undefined {
  if (submitCount.value === 0) return undefined;
  const entry = entries.value[index];
  if (!entry || !isRedirectOrigin(entry.origin)) return t("directory.validation.redirectOrigin");
  const first = entries.value.findIndex(
    (other) => other.origin === entry.origin && other.prefix === entry.prefix,
  );
  return first !== index ? t("directory.validation.redirectUnique") : undefined;
}

function prefixError(index: number): string | undefined {
  if (submitCount.value === 0) return undefined;
  const entry = entries.value[index];
  return entry && isRedirectPrefix(entry.prefix)
    ? undefined
    : t("directory.validation.redirectPrefix");
}
</script>

<template>
  <fieldset class="flex flex-col gap-2" :disabled="disabled" :aria-labelledby="`${id}-legend`">
    <div class="flex h-6 items-center justify-between gap-2">
      <legend :id="`${id}-legend`" class="text-sm font-medium">
        {{ t("directory.redirect.allowlist") }}
      </legend>
      <slot name="reset" />
    </div>
    <p class="text-xs text-muted">{{ t("directory.redirect.allowlistHint") }}</p>
    <p v-if="entries.length === 0" class="text-sm text-muted">
      {{ t("directory.redirect.allowlistEmpty") }}
    </p>
    <ul v-else class="flex flex-col gap-2">
      <li v-for="(entry, index) in entries" :key="index" class="flex items-start gap-2">
        <div class="grid min-w-0 flex-1 gap-2 sm:grid-cols-[2fr_1fr]">
          <Field
            v-slot="{ control }"
            :label="t('directory.redirect.origin', { number: index + 1 })"
            :error="originError(index)"
            hide-label
          >
            <Input
              v-bind="control"
              :model-value="entry.origin"
              type="url"
              inputmode="url"
              spellcheck="false"
              autocomplete="off"
              maxlength="2048"
              :placeholder="ORIGIN_PLACEHOLDER"
              :disabled="disabled"
              @update:model-value="update(index, { origin: $event })"
            />
          </Field>
          <Field
            v-slot="{ control }"
            :label="t('directory.redirect.prefix', { number: index + 1 })"
            :error="prefixError(index)"
            hide-label
          >
            <Input
              v-bind="control"
              :model-value="entry.prefix"
              spellcheck="false"
              autocomplete="off"
              maxlength="2048"
              placeholder="/"
              :disabled="disabled"
              @update:model-value="update(index, { prefix: $event })"
            />
          </Field>
        </div>
        <IconButton
          :label="t('directory.redirect.remove', { number: index + 1 })"
          :disabled="disabled"
          @click="remove(index)"
        >
          <X aria-hidden="true" />
        </IconButton>
      </li>
    </ul>
    <div>
      <Button
        size="sm"
        :disabled="disabled || entries.length >= MAX_REDIRECT_ALLOWLIST"
        @click="add"
      >
        <Plus aria-hidden="true" /> {{ t("directory.redirect.add") }}
      </Button>
    </div>
  </fieldset>
</template>
