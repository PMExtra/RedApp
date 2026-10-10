<script setup lang="ts">
// Test fixture: shared UI components wired to local state, so tests can
// assert behaviour through roles and visible text.
import { ref } from "vue";
import { confirm } from "@/shared/lib";
import {
  Combobox,
  DataTable,
  Pagination,
  SortableList,
  Switch,
  Tabs,
  type DataTableSort,
} from "@/shared/ui";

const props = defineProps<{
  part: string;
  tableError?: unknown;
  rows?: { id: string; name: string }[];
}>();

const enabled = ref(false);
const sort = ref<DataTableSort | null>(null);
const page = ref(1);
const items = ref([
  { id: "a", name: "Alpha" },
  { id: "b", name: "Beta" },
  { id: "c", name: "Gamma" },
]);
const search = ref("");
const selected = ref<string>();
const chosen = ref("");
const submitted = ref("");
const confirmed = ref<string>("pending");
const tab = ref("one");

async function ask() {
  confirmed.value = String(
    await confirm({
      title: "Delete vendor?",
      description: "This cannot be undone.",
      tone: "danger",
    }),
  );
}
</script>

<template>
  <div>
    <template v-if="props.part === 'switch'">
      <label for="auto">Auto refresh</label>
      <Switch id="auto" v-model="enabled" />
      <p>Enabled: {{ enabled }}</p>
    </template>

    <template v-else-if="props.part === 'table'">
      <DataTable
        v-model:sort="sort"
        caption="Applications"
        :columns="[
          { key: 'name', label: 'Name', sortable: true },
          { key: 'id', label: 'ID' },
        ]"
        :rows="props.rows"
        :error="props.tableError"
        :row-key="(row) => row.id"
      />
    </template>

    <template v-else-if="props.part === 'pagination'">
      <Pagination v-model:page="page" :total="95" :page-size="10" />
      <p>Current page {{ page }}</p>
    </template>

    <template v-else-if="props.part === 'sortable'">
      <SortableList v-model="items" :item-key="(item) => item.id" :item-label="(item) => item.name">
        <template #item="{ item }">{{ item.name }}</template>
      </SortableList>
      <p>Order: {{ items.map((item) => item.name).join(", ") }}</p>
    </template>

    <template v-else-if="props.part === 'combobox'">
      <Combobox
        v-model="selected"
        v-model:search="search"
        aria-label="Search"
        :options="
          search
            ? [
                { value: 'openai/codex', label: 'Codex CLI' },
                { value: 'anthropic/claude-code', label: 'Claude Code' },
              ]
            : []
        "
        keep-search
        @select="chosen = $event.value"
        @submit="submitted = $event"
      />
      <p>Chosen: {{ chosen }}</p>
      <p>Submitted: {{ submitted }}</p>
    </template>

    <template v-else-if="props.part === 'confirm'">
      <button type="button" @click="ask">Delete</button>
      <p>Confirmed: {{ confirmed }}</p>
    </template>

    <template v-else-if="props.part === 'tabs'">
      <Tabs
        v-model="tab"
        label="Sections"
        :items="[
          { value: 'one', label: 'First' },
          { value: 'two', label: 'Second' },
        ]"
      >
        <template #one>First panel</template>
        <template #two>Second panel</template>
      </Tabs>
    </template>
  </div>
</template>
