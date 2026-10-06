<script setup lang="ts">
import RelativeTime from "./RelativeTime.vue";
import Icon from "./Icon.vue";
import IconButton from "./IconButton.vue";
import DisclosureIcon from "./DisclosureIcon.vue";
import { computed, reactive, ref, watch } from "vue";
import { bytes, type Resource, type VersionSummary } from "../api";
import { appAPI } from "../bootstrap";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import { errorText, localDate, stateLabel, t } from "../i18n";
import PageNavigation from "./PageNavigation.vue";
const props = withDefaults(
  defineProps<{ application: string; automatic?: boolean }>(),
  { automatic: true },
);
const version = ref("");
const automatic = computed(() => props.automatic);
watch(
  () => props.application,
  () => {
    version.value = "";
  },
  { flush: "sync" },
);
const versions = reactive(
  useNumberedCollection<VersionSummary>(
    computed(() => `${appAPI(props.application)}/versions`),
    25,
    automatic,
  ),
);
const resources = reactive(
  useNumberedCollection<Resource>(
    computed(
      () =>
        `${appAPI(props.application)}/resources${version.value ? `?version=${encodeURIComponent(version.value)}` : ""}`,
    ),
    25,
    automatic,
  ),
);
</script>
<template>
  <section class="resource-workspace">
    <section class="panel version-panel">
      <div class="section-heading">
        <h2>{{ t("Versions") }}</h2>
        <button
          class="secondary all-versions"
          :aria-pressed="!version"
          @click="version = ''"
        >
          {{ t("All versions") }}
        </button>
      </div>
      <div v-if="versions.error" class="error" role="alert">
        {{ errorText(versions.error)
        }}<small v-if="versions.loaded">{{
          t("Showing the last successful snapshot.")
        }}</small
        ><IconButton
          class="secondary"
          :disabled="versions.loading"
          @click="versions.refresh"
          icon="refresh"
          :label="t('Retry')"
        />
      </div>
      <p v-if="!versions.loaded && versions.loading" role="status">
        {{ t("Loading…") }}
      </p>
      <div class="version-list">
        <button
          v-for="item in versions.items" :key="item.version"
          type="button" class="version-button"
          :aria-pressed="version === item.version"
          :title="localDate(item.first_seen)"
          @click="version = version === item.version ? '' : item.version"
        >
          <span class="version-row-heading"><strong>{{ item.version }}</strong><Icon v-if="version === item.version" name="check" :size="18" /></span>
          <span class="version-row-meta">
            <RelativeTime :value="item.first_seen" :focusable="false" />
            <span>{{ t("{count} artifact requests", { count: item.requests }) }}</span>
          </span>
        </button>
        <p v-if="versions.loaded && !versions.items.length" class="empty">
          {{
            t("No versions discovered yet. Downloads are fetched on demand.")
          }}
        </p>
      </div>
      <PageNavigation
        compact
        :label="t('Version pages')"
        :page="versions.page"
        :total="versions.total"
        :total-pages="versions.totalPages"
        @go="versions.go"
        :previous="versions.previousAvailable"
        :next="versions.nextAvailable"
        :loading="versions.loading"
        @previous="versions.previous"
        @next="versions.next"
        @refresh="versions.refresh"
      />
    </section>
    <section class="panel resource-panel">
      <div class="section-heading">
        <h2>{{ t("Resources") }}</h2>
        <span class="state-label">{{ version || t("All versions") }}</span>
      </div>
      <div v-if="resources.error" class="error" role="alert">
        {{ errorText(resources.error)
        }}<small v-if="resources.loaded">{{
          t("Showing the last successful snapshot.")
        }}</small
        ><IconButton
          class="secondary"
          :disabled="resources.loading"
          @click="resources.refresh"
          icon="refresh"
          :label="t('Retry')"
        />
      </div>
      <p v-if="!resources.loaded && resources.loading" role="status">
        {{ t("Loading…") }}
      </p>
      <p class="muted small-text">
        {{
          t(
            "Average effective speed includes retries and excludes verification; recent speed is a five-second snapshot.",
          )
        }}
      </p>
      <div
        class="table-wrap"
        v-if="resources.items.length"
        tabindex="0"
        :aria-label="t('Versions and resources')"
      >
        <table>
          <thead>
            <tr>
              <th>{{ t("Version / file") }}</th>
              <th>{{ t("Generation / state") }}</th>
              <th>{{ t("On disk / readers") }}</th>
              <th>{{ t("Average / recent") }}</th>
              <th>{{ t("Resumes / verification") }}</th>
              <th>{{ t("Started / finished") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in resources.items" :key="item.ID">
              <td>
                <strong>{{ item.Resource.Version }}</strong
                ><code class="file-name">{{ item.Resource.Key }}</code>
              </td>
              <td>
                <code>{{ item.ID.slice(0, 8) }}</code
                ><span
                  :class="[
                    'state-badge',
                    {
                      'state-success': item.State === 'complete',
                      'state-error': ['failed', 'invalid'].includes(
                        item.State,
                      ),
                    },
                  ]"
                  >{{ stateLabel(item.State) }}</span
                ><small v-if="item.Retired">{{ t("Pending deletion") }}</small>
              </td>
              <td>
                {{ bytes(item.Bytes)
                }}<small>{{ t("Active readers") }}: {{ item.Readers }}</small>
              </td>
              <td>
                {{ bytes(item.AverageBPS) }}/s<small
                  >{{ bytes(item.RecentBPS) }}/s</small
                >
              </td>
              <td>
                {{ item.Resumes }} /
                {{ (item.VerificationNS / 1e6).toFixed(1) }} ms
              </td>
              <td>
                <time>{{ localDate(item.Started) }}</time
                ><small>{{ localDate(item.Finished) }}</small>
                <details v-if="item.Error">
                  <summary><DisclosureIcon />{{ t("Last error") }}</summary>
                  <p class="diagnostic">{{ item.Error }}</p>
                </details>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else-if="resources.loaded" class="empty">
        {{ t("No cached resources for this selection.") }}
      </p>
      <PageNavigation
        :label="t('Resource pages')"
        :page="resources.page"
        :total="resources.total"
        :total-pages="resources.totalPages"
        @go="resources.go"
        :previous="resources.previousAvailable"
        :next="resources.nextAvailable"
        :loading="resources.loading"
        @previous="resources.previous"
        @next="resources.next"
        @refresh="resources.refresh"
      />
    </section>
  </section>
</template>
