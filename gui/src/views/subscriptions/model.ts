import { deleteTouch, getSharingAddress, putSubscription } from "@/api";
import type { TouchSubscription } from "@/api/types";
import { whichOf } from "../nodes/model";

// Subscription mutations, without a copy of the subscription list: the views
// that own the list already refresh it, so syncing here only added a second
// /touch round trip per action.
export function useSubscriptions() {
  async function update(subscription: TouchSubscription) {
    await putSubscription(whichOf(subscription));
  }

  async function remove(subscription: TouchSubscription) {
    await deleteTouch([whichOf(subscription)]);
  }

  async function sharingLink(subscription: TouchSubscription) {
    return (await getSharingAddress(whichOf(subscription))).sharingAddress;
  }

  return {
    update,
    remove,
    sharingLink,
  };
}
