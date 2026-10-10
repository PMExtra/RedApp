<script setup lang="ts">
import { computed, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Button, Input, Pagination } from "@/shared/ui";

/**
 * Page links plus a "go to page" box once there are more pages than the
 * links can show. Emits page changes only; the caller loads the page.
 */
const page = defineModel<number>("page", { required: true });
const props = defineProps<{ total: number; pageSize: number }>();
const { t } = useI18n();
const id = useId();
const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const requested = ref("");
const invalid = ref(false);

watch(page, () => {
  requested.value = "";
  invalid.value = false;
});

function jump() {
  const value = requested.value.trim();
  const next = Number(value);
  invalid.value = !/^\d+$/.test(value) || next < 1 || next > totalPages.value;
  if (invalid.value) return;
  requested.value = "";
  page.value = next;
}
</script>

<template>
  <div v-if="totalPages > 1" class="flex flex-wrap items-center justify-between gap-3">
    <Pagination v-model:page="page" :total="total" :page-size="pageSize" />
    <form v-if="totalPages > 7" class="flex flex-col gap-1" novalidate @submit.prevent="jump">
      <div class="flex items-center gap-2">
        <label :for="`${id}-page`" class="text-sm text-muted">
          {{ t("catalog.pageJump.label") }}
        </label>
        <Input
          :id="`${id}-page`"
          v-model="requested"
          class="w-20"
          inputmode="numeric"
          :placeholder="String(page)"
          :aria-invalid="invalid ? 'true' : undefined"
          :aria-describedby="invalid ? `${id}-error` : undefined"
          @input="invalid = false"
        />
        <Button type="submit" size="sm">{{ t("catalog.pageJump.go") }}</Button>
      </div>
      <p v-if="invalid" :id="`${id}-error`" role="alert" class="text-xs text-danger">
        {{ t("catalog.pageJump.invalid", { total: totalPages }) }}
      </p>
    </form>
  </div>
</template>
