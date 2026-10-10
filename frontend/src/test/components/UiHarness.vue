<script setup lang="ts">
// Test fixture: shared UI components wired to local state, so tests can
// assert behaviour through roles and visible text.
import { computed, ref } from "vue";
import { confirm } from "@/shared/lib";
import {
  Combobox,
  CopyButton,
  DataTable,
  Field,
  FilePicker,
  Pagination,
  RadioGroup,
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
// Server-style suggestions: nothing until something is typed, then matches.
const suggestions = computed(() =>
  search.value
    ? [
        { value: "openai/codex", label: "Codex CLI" },
        { value: "anthropic/claude-code", label: "Claude Code" },
      ].filter((option) => option.label.toLowerCase().includes(search.value.toLowerCase()))
    : [],
);
const selected = ref<string>();
const chosen = ref("");
const submitted = ref("");
const confirmed = ref<string>("pending");
const tab = ref("one");
const files = ref<File[]>([]);
const mode = ref<string | undefined>("a");

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
        :options="suggestions"
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

    <template v-else-if="props.part === 'keptTabs'">
      <Tabs
        v-model="tab"
        label="Sections"
        keep-mounted
        :items="[
          { value: 'one', label: 'First' },
          { value: 'two', label: 'Second' },
        ]"
      >
        <template #one><input aria-label="Draft" /></template>
        <template #two>Second panel</template>
      </Tabs>
    </template>

    <template v-else-if="props.part === 'fields'">
      <Field v-slot="{ control }" label="Package" description="A ZIP file." error="Required.">
        <FilePicker v-bind="control" v-model="files" />
      </Field>
      <Field v-slot="{ control }" label="Mode">
        <RadioGroup
          v-bind="control"
          v-model="mode"
          :options="[
            { value: 'a', label: 'Alpha' },
            { value: 'b', label: 'Beta' },
          ]"
        />
      </Field>
      <CopyButton text="secret-token" />
    </template>
  </div>
</template>
