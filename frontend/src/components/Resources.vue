<script setup lang="ts">
import { computed, ref } from "vue";
import { bytes, type Status } from "../api";
import { localDate, stateLabel, t } from "../i18n";
import SelectMenu from "./SelectMenu.vue";
const props = defineProps<{ status: Status }>();
const version = ref("");
const resources = computed(() =>
  props.status.resources.filter(
    (item) => !version.value || item.Resource.Labels.version === version.value,
  ),
);
</script>
<template>
  <section class="panel">
    <div class="section-heading">
      <h2>{{ t("Versions and resources") }}</h2>
      <div class="inline-label">
        <span>{{ t("Version") }}</span
        ><SelectMenu
          v-model="version"
          :label="t('Version')"
          :options="[
            { value: '', label: t('All versions') },
            ...Object.keys(status.versions).map((name) => ({
              value: name,
              label: name,
            })),
          ]"
        />
      </div>
    </div>
    <div class="version-list">
      <article v-for="(firstSeen, name) in status.versions" :key="name">
        <button
          class="version-button secondary"
          :aria-pressed="version === name"
          @click="version = version === name ? '' : name"
        >
          {{ name }}
        </button>
        <div>
          <span>{{ t("First seen") }} {{ localDate(firstSeen) }}</span
          ><small class="muted">{{
            t("{count} artifact requests", {
              count: status.counters["version:" + name + ":requests"] || 0,
            })
          }}</small>
        </div>
      </article>
      <p v-if="!Object.keys(status.versions).length" class="empty">
        {{ t("No versions discovered yet. Downloads are fetched on demand.") }}
      </p>
    </div>
    <p class="muted small-text">
      {{
        t(
          "Average effective speed includes retries and excludes verification; recent speed is a five-second snapshot.",
        )
      }}
    </p>
    <div
      class="table-wrap"
      v-if="resources.length"
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
          <tr v-for="item in resources" :key="item.ID">
            <td>
              <strong>{{ item.Resource.Labels.version }}</strong
              ><code class="file-name">{{ item.Resource.Labels.name }}</code>
            </td>
            <td>
              <code>{{ item.ID.slice(0, 8) }}</code
              ><span
                :class="[
                  'state-badge',
                  {
                    'state-success': item.State === 'complete',
                    'state-error': ['failed', 'invalid'].includes(item.State),
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
                <summary>{{ t("Last error") }}</summary>
                <p class="diagnostic">{{ item.Error }}</p>
              </details>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else class="empty">
      {{ t("No cached resources for this selection.") }}
    </p>
  </section>
</template>
