// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { flushPromises, type VueWrapper } from "@vue/test-utils";
import { mountWithApp } from "@/test/mount";
import RuleSource from "./RuleSource.vue";

const source = {
  url: "https://example.test/template.json",
  version: "RUv1",
  remarks: "Заблокированное",
  domainStrategy: "IPOnDemand",
  items: [
    {
      remarks: "Всё",
      url: "https://example.test/all.json",
      domainStrategy: "",
    },
    {
      remarks: "Заблокированное",
      url: "https://example.test/only.json",
      domainStrategy: "IPOnDemand",
    },
  ],
};

let wrapper: VueWrapper;
const importSource = vi.fn().mockResolvedValue(undefined);
const clearSource = vi.fn().mockResolvedValue(undefined);

function mountPanel(props: Record<string, unknown> = {}) {
  wrapper = mountWithApp(RuleSource, {
    props: {
      subject: "routing",
      source,
      picksSet: true,
      importSource,
      clearSource,
      ...props,
    },
  });
  return wrapper;
}

beforeEach(() => {
  vi.clearAllMocks();
  importSource.mockResolvedValue(undefined);
  clearSource.mockResolvedValue(undefined);
});
afterEach(() => wrapper?.unmount());

test("a configured source is shown, and importing passes the chosen set", async () => {
  mountPanel();
  await flushPromises();

  expect(wrapper.text()).toContain("Rule source");
  expect(wrapper.text()).toContain("Заблокированное");
  // expand the panel to reach the controls
  await wrapper.find(".v-expansion-panel-title").trigger("click");
  await flushPromises();

  const apply = wrapper
    .findAll("button")
    .find((b) => b.text().trim() === "Import");
  expect(apply).toBeDefined();
  await apply!.trigger("click");
  await flushPromises();

  expect(importSource).toHaveBeenCalledWith(
    "https://example.test/template.json",
    "Заблокированное",
  );
});

test("with no source configured the form is open and ready to be filled", async () => {
  mountPanel({ source: null });
  await flushPromises();
  // an unconfigured source has nothing to collapse, so the URL field is
  // reachable without opening anything
  expect(wrapper.text()).toContain("Source URL");
  const field = wrapper.find('input[type="text"]');
  await field.setValue("https://example.test/new.json");
  await flushPromises();
  const apply = wrapper
    .findAll("button")
    .find((b) => b.text().trim() === "Import");
  expect(apply).toBeDefined();
  await apply!.trigger("click");
  await flushPromises();
  expect(importSource).toHaveBeenCalledWith(
    "https://example.test/new.json",
    undefined,
  );
});

test("a URL that is not http is refused before a request is made", async () => {
  mountPanel();
  await flushPromises();
  await wrapper.find(".v-expansion-panel-title").trigger("click");
  await flushPromises();

  const field = wrapper.find('input[type="text"]');
  await field.setValue("ftp://example.test/rules.json");
  await flushPromises();
  expect(wrapper.text()).toContain("http://");

  const apply = wrapper
    .findAll("button")
    .find((b) => b.text().trim() === "Import");
  expect(apply!.attributes("disabled")).toBeDefined();
  expect(importSource).not.toHaveBeenCalled();
});

test("forgetting the source is offered and delegated", async () => {
  mountPanel();
  await flushPromises();
  await wrapper.find(".v-expansion-panel-title").trigger("click");
  await flushPromises();

  const forget = wrapper.find('button[title="Forget the source"]');
  expect(forget.exists()).toBe(true);
  await forget.trigger("click");
  await flushPromises();
  expect(clearSource).toHaveBeenCalled();
});

// The DNS source's index names no sets, so offering a choice would show an
// empty picker and lose the only configuration the user has.
test("the DNS panel does not offer a set picker", async () => {
  mountPanel({ subject: "dns", picksSet: false });
  await flushPromises();
  await wrapper.find(".v-expansion-panel-title").trigger("click");
  await flushPromises();
  expect(wrapper.text()).not.toContain("Rule set");
  expect(wrapper.text()).not.toContain("Domain strategy");
});
