import type { LocaleModule } from "@/shared/i18n";

export default {
  taxonomy: {
    title: "Categories and tags",
    description:
      "Categories group applications on the public site. Tags are private search keywords for administrators.",
    categories: {
      label: "Categories",
      selected: "Selected categories",
      placeholder: "Search categories or type a new name",
      hint: "Choose a category, or type a new name and press Enter. New categories are created when you save.",
      pendingLabel: "{name} (new)",
      ambiguous: "This name matches several categories. Choose one from the list.",
      tooLong: "Category names can have at most {max} characters.",
      tooMany: "An application can have at most {max} categories.",
      loadFailed: "The category list could not be loaded; only typed names can be added.",
    },
    tags: {
      label: "Tags",
      new: "New tag",
      placeholder: "Add a tag",
      add: "Add tag",
      hint: "Press Enter to add. A leading # is ignored; tags are never shown on public pages.",
      tooLong: "Tags can have at most {max} characters.",
      tooMany: "An application can have at most {max} tags.",
    },
    page: {
      description:
        "Categories are created while editing applications and removed automatically when nothing uses them. Here you can rename them.",
      search: "Search categories",
      caption: "Categories",
      name: "Name",
      id: "ID",
      otherName: "Other language",
      applications: "Applications",
      actions: "Actions",
      builtin: "Built-in",
      rename: "Rename {name}",
      empty: "No categories yet. Add one while editing an application.",
      noMatches: "No categories match this search.",
    },
    edit: {
      title: "Rename category {id}",
      description: "Names must be unique across categories. They are shown on the public site.",
      nameIn: "Name ({language})",
      nameRequired: "Enter a name.",
      nameTooLong: "At most 256 bytes.",
    },
  },
} satisfies LocaleModule;
