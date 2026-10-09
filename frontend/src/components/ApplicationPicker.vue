<script setup lang="ts">
import EntityIcon from "./EntityIcon.vue";
import { computed, onUnmounted, ref, useId } from "vue";
import { api, isCancellation } from "../api";
import { directoryIcon, type ManagedApplication } from "../directory";
import { errorText, language, t } from "../i18n";
// Searches administrator applications across vendors; emits the canonical key of the chosen one.
const props = defineProps<{ exclude?: string[]; disabled?: boolean }>();
const emit = defineEmits<{ select: [ManagedApplication] }>();
const id = useId();
const query = ref(""),
  items = ref<ManagedApplication[]>([]),
  open = ref(false),
  active = ref(-1),
  loading = ref(false),
  error = ref<unknown>();
let ticket = 0,
  controller: AbortController | undefined,
  timer: ReturnType<typeof setTimeout> | undefined;
const excluded = computed(() => new Set(props.exclude || []));
function stop() {
  ticket++;
  clearTimeout(timer);
  controller?.abort();
  controller = undefined;
  loading.value = false;
}
function close() {
  stop();
  open.value = false;
  active.value = -1;
}
function search() {
  stop();
  error.value = undefined;
  const text = query.value.trim(),
    attempt = ticket;
  open.value = true;
  loading.value = true;
  timer = setTimeout(async () => {
    const request = new AbortController();
    controller = request;
    try {
      const page = await api<{ items: ManagedApplication[] }>(
        `apps?q=${encodeURIComponent(text)}&limit=8`,
        undefined,
        request.signal,
      );
      if (attempt !== ticket) return;
      items.value = page.items;
      active.value = page.items.findIndex((item) => !excluded.value.has(item.key));
    } catch (reason) {
      if (attempt === ticket && !isCancellation(reason)) {
        items.value = [];
        error.value = reason;
      }
    } finally {
      if (attempt === ticket) loading.value = false;
    }
  }, 200);
}
function choose(item?: ManagedApplication) {
  if (!item || excluded.value.has(item.key)) return;
  emit("select", item);
  query.value = "";
  items.value = [];
  close();
}
function move(step: number) {
  if (!open.value) return search();
  const count = items.value.length;
  if (!count) return;
  let next = active.value;
  for (let i = 0; i < count; i++) {
    next = (next + step + count) % count;
    if (!excluded.value.has(items.value[next]!.key)) break;
  }
  active.value = next;
}
function enter(event: KeyboardEvent) {
  // An IME confirmation must not pick an application.
  if (event.isComposing || event.keyCode === 229) return;
  event.preventDefault();
  if (open.value) choose(items.value[active.value]);
}
const root = ref<HTMLElement>();
function leave(event: FocusEvent) {
  if (!root.value?.contains(event.relatedTarget as Node | null)) close();
}
onUnmounted(stop);
</script>
<template>
  <div ref="root" class="application-picker" @focusout="leave">
    <input
      v-model="query"
      type="search"
      role="combobox"
      autocomplete="off"
      spellcheck="false"
      :aria-label="t('Search applications to pin')"
      :placeholder="t('Search applications to pin')"
      :aria-expanded="open"
      :aria-controls="`${id}-list`"
      :aria-activedescendant="active >= 0 && open ? `${id}-${active}` : undefined"
      :disabled="disabled"
      @input="search"
      @focus="search"
      @keydown.down.prevent="move(1)"
      @keydown.up.prevent="move(-1)"
      @keydown.enter="enter"
      @keydown.esc="close"
    />
    <ul
      v-show="open"
      :id="`${id}-list`"
      class="application-picker-options"
      role="listbox"
      :aria-label="t('Applications')"
    >
      <li v-if="loading" class="muted" role="presentation">{{ t("Loading…") }}</li>
      <li v-else-if="error" class="error" role="presentation">{{ errorText(error) }}</li>
      <li v-else-if="!items.length" class="muted" role="presentation">
        {{ t("No matching applications.") }}
      </li>
      <template v-else>
        <li
          v-for="(item, index) in items"
          :id="`${id}-${index}`"
          :key="item.uid"
          role="option"
          tabindex="-1"
          :aria-selected="index === active"
          :aria-disabled="excluded.has(item.key)"
          :class="{ active: index === active }"
          @mousedown.prevent
          @click="choose(item)"
        >
          <EntityIcon :src="directoryIcon(item.icon)" size="search" />
          <span class="application-picker-name">{{ item.name[language] || item.key }}</span>
          <code>{{ item.key }}</code>
          <small v-if="excluded.has(item.key)" class="muted">{{ t("Already pinned") }}</small>
          <small v-else-if="!item.enabled" class="muted">{{ t("Disabled") }}</small>
        </li>
      </template>
    </ul>
  </div>
</template>
