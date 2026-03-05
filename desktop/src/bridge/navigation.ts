import { showConnectionDetailsPanel, type BridgeStatusPayload } from "./view_model";

export type BridgeRoute = "signin" | "details";

export function routeFromHash(hash: string): BridgeRoute {
  const normalizedHash = hash.trim().toLowerCase();

  if (normalizedHash === "#/details") {
    return "details";
  }

  return "signin";
}

export function hashForRoute(route: BridgeRoute): string {
  if (route === "details") {
    return "#/details";
  }

  return "#/signin";
}

export function targetRouteForStatus(status: BridgeStatusPayload): BridgeRoute {
  return showConnectionDetailsPanel(status) ? "details" : "signin";
}

export function normalizeRouteForStatus(
  _currentRoute: BridgeRoute,
  status: BridgeStatusPayload,
): BridgeRoute {
  return targetRouteForStatus(status);
}
