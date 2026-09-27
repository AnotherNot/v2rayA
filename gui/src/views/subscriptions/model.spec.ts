// @vitest-environment happy-dom
import { afterEach, expect, test } from "vitest";
import { client } from "@/api/client";
import type { TouchSubscription } from "@/api/types";
import { useSubscriptions } from "./model";

const subscription: TouchSubscription = {
  id: 3,
  _type: "subscription",
  remarks: "Travel",
  host: "example.test",
  address: "https://example.test/subscription",
  status: "2026-09-19T12:00:00Z",
  info: "Used 1 GiB / 10 GiB · Expires 2026-10-01",
  servers: [],
  autoSelect: false,
};
const adapter = client.defaults.adapter;
afterEach(() => {
  client.defaults.adapter = adapter;
});

test("update and delete address the subscription and leave the reload to the caller", async () => {
  const sent: { method?: string; path: string; body: unknown }[] = [];
  client.defaults.adapter = async (config) => {
    sent.push({
      method: config.method,
      path: new URL(config.url!, "http://backend.test").pathname,
      body: config.data ? JSON.parse(config.data) : undefined,
    });
    return {
      config,
      status: 200,
      statusText: "OK",
      headers: {},
      data: {
        code: "SUCCESS",
        data: { sharingAddress: "https://example.test/s" },
      },
    };
  };
  const model = useSubscriptions();
  await model.update(subscription);
  await model.remove(subscription);
  await model.sharingLink(subscription);
  // The views that own the subscription list refresh it themselves, so a
  // /touch here was a second round trip per action.
  expect(sent).toEqual([
    {
      method: "put",
      path: "/api/subscription",
      body: { id: 3, _type: "subscription" },
    },
    {
      method: "delete",
      path: "/api/touch",
      body: { touches: [{ id: 3, _type: "subscription" }] },
    },
    { method: "get", path: "/api/sharingAddress", body: undefined },
  ]);
});
