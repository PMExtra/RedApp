<script setup lang="ts">
import { vendorName } from "../vendorName";
import Icon from "./Icon.vue";
import EntityIcon from "./EntityIcon.vue";
import VendorLogo from "./VendorLogo.vue";
import { computed, ref, watch, onMounted, onUnmounted, useId } from "vue";
import { useRoute, useRouter } from "vue-router";
import { publicFetch } from "../public";
import { isCancellation } from "../api";
import type { LocalizedText } from "../site";
import { language, t } from "../i18n";
const route = useRoute(),
  router = useRouter(),
  id = useId();
interface Suggestion {
  kind: "vendor" | "app";
  id: string;
  name: LocalizedText;
  url: string;
  icon: string;
  localized_icons?: LocalizedText;
}
const search = ref(typeof route.query.q === "string" ? route.query.q : ""),
  items = ref<Suggestion[]>([]),
  open = ref(false),
  active = ref(-1),
  loading = ref(false),
  failed = ref(false);
const composing = ref(false);
const root = ref<HTMLElement>();
const popular = computed(() => !search.value.trim());
let ticket = 0,
  controller: AbortController | undefined,
  timer: ReturnType<typeof setTimeout> | undefined;
function stop() {
  ticket++;
  clearTimeout(timer);
  controller?.abort();
  controller = undefined;
  loading.value = false;
  items.value = [];
  active.value = -1;
}
function close() {
  stop();
  open.value = false;
  failed.value = false;
}
function suggest() {
  stop();
  failed.value = false;
  if (composing.value) {
    open.value = false;
    return;
  }
  open.value = true;
  const attempt = ticket, query = search.value.trim();
  loading.value = true;
  timer = setTimeout(async () => {
    const request = new AbortController();
    controller = request;
    loading.value = true;
    try {
      let suggestions: Suggestion[];
      if (query) {
        const data = await publicFetch<{ items: Suggestion[] }>(`/api/search?q=${encodeURIComponent(query)}`, request.signal);
        suggestions = data.items;
      } else {
        // Reuse the public homepage's ranking and visibility filtering. Keep
        // the same six-application limit as ordinary application suggestions.
        const data = await publicFetch<{ ranking: { id: string; name: LocalizedText; icon: string; detail_url: string }[] }>("/api/home", request.signal);
        suggestions = data.ranking.slice(0, 6).map(item => ({ kind: "app", id: item.id, name: item.name, icon: item.icon, url: item.detail_url }));
      }
      if (attempt === ticket) {
        items.value = suggestions;
        active.value = -1;
      }
    } catch (e) {
      if (attempt === ticket && !isCancellation(e)) failed.value = true;
    } finally {
      if (attempt === ticket) {
        loading.value = false;
        controller = undefined;
      }
    }
  }, 200);
}
watch(search, suggest, { flush: "sync" });
watch(
  () => route.fullPath,
  () => {
    close();
    search.value = typeof route.query.q === "string" ? route.query.q : "";
    close();
  },
  { flush: "sync" },
);
function navigate(item?: Suggestion) {
  const q = search.value.trim();
  close();
  void router.push(item?.url || { path: "/all", query: q ? { q } : {} });
}
function key(event: KeyboardEvent) {
  if (composing.value || event.isComposing || event.keyCode === 229) return;
  if (event.key === "Escape") {
    event.preventDefault();
    close();
  } else if (event.key === "Enter") {
    event.preventDefault();
    navigate(
      open.value && active.value >= 0 ? items.value[active.value] : undefined,
    );
  } else if (["ArrowDown", "ArrowUp"].includes(event.key)) {
    event.preventDefault();
    if (!open.value) suggest();
    else if (items.value.length)
      active.value = active.value < 0 ? (event.key === "ArrowDown" ? 0 : items.value.length - 1) :
        (active.value +
          (event.key === "ArrowDown" ? 1 : -1) +
          items.value.length) %
        items.value.length;
  }
}
function outside(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) close();
}
onMounted(() => document.addEventListener("pointerdown", outside));
onUnmounted(() => { stop(); document.removeEventListener("pointerdown", outside); });
</script>
<template>
  <div
    ref="root"
    class="public-search"
    @focusout="
      !($event.currentTarget as HTMLElement).contains(
        $event.relatedTarget as Node,
      ) && close()
    "
  >
    <Icon name="search" class="search-input-icon" />
    <input
      v-model="search"
      role="combobox"
      type="search"
      maxlength="128"
      :aria-label="t('Search vendors and applications')"
      :placeholder="t('Search applications')"
      aria-autocomplete="list"
      :aria-expanded="open"
      :aria-controls="`${id}-suggestions`"
      :aria-activedescendant="
        open && active >= 0 ? `${id}-${active}` : undefined
      "
      @keydown="key"
      @focus="suggest"
      @compositionstart="
        composing = true;
        close();
      "
      @compositionend="
        composing = false;
        suggest();
      "
    />
    <div v-if="open" class="search-popover">
      <p v-if="popular" class="search-suggestions-heading">{{ t("Popular applications") }}</p>
      <p v-if="loading" role="status">{{ t("Loading…") }}</p>
      <p v-else-if="failed" role="status">
        {{ popular ? t("Popular applications unavailable. Press Enter to open all applications.") : t("Search unavailable. Press Enter to open all applications.") }}
      </p>
      <ul
        :id="`${id}-suggestions`"
        role="listbox"
        :aria-label="popular ? t('Popular applications') : t('Search suggestions')"
      >
        <li
          v-for="(item, index) in items"
          :id="`${id}-${index}`"
          :key="item.kind + item.id"
          role="option"
          :aria-selected="active === index"
          @mousedown.prevent
          @click="navigate(item)"
        >
          <VendorLogo v-if="item.kind === 'vendor'" :vendor="item" /><EntityIcon v-else :src="item.icon" />
          <span class="search-suggestion-text"
            ><span>{{ item.kind === "vendor" ? vendorName(item) : item.name[language] }}</span
            ><small
              >{{ item.kind === "vendor" ? t("Vendor") : t("Application") }} ·
              {{ item.id }}</small
            ></span
          >
        </li>
      </ul>
      <p v-if="!loading && !failed && !items.length" class="muted">
        {{ popular ? t("No popular applications yet. Press Enter to open all applications.") : t("No suggestions. Press Enter to search all applications.") }}
      </p>
    </div>
  </div>
</template>
