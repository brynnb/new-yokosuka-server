#!/usr/bin/env node

import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const sources = Object.freeze({
  storeState: "data/source/d000-scripted-store-state.json",
  doorTransitions: "data/source/d000-door-transitions.json",
  worldTransitions: "data/source/native-map-transitions.json",
  worldAccessPolicy: "data/source/world-access-policy.json",
});
const serverOutput = "internal/travelaccess/rules.json";

async function readJSON(relativePath) {
  const bytes = await fs.readFile(path.join(root, relativePath));
  return {
    bytes,
    value: JSON.parse(bytes.toString("utf8")),
  };
}

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function onlyContainedDoor(overlay) {
  const doors = overlay.exactHorizontalContainmentBindings.flatMap(
    (binding) => binding.containedDoorSources,
  );
  if (doors.length !== 1) {
    throw new Error(
      `Expected one exact door association for D000 layer ${overlay.layer}`,
    );
  }
  return doors[0];
}

const [
  storeSource,
  doorSource,
  worldSource,
  worldAccessPolicySource,
] = await Promise.all([
  readJSON(sources.storeState),
  readJSON(sources.doorTransitions),
  readJSON(sources.worldTransitions),
  readJSON(sources.worldAccessPolicy),
]);

if (
  worldAccessPolicySource.value.schema
    !== "new-yokosuka-world-access-policy-v1"
) {
  throw new Error("The world access policy schema is unsupported");
}
const alwaysAccessibleInteriorWorldIds = (
  worldAccessPolicySource.value.alwaysAccessibleInteriorWorldIds
);
if (
  !Array.isArray(alwaysAccessibleInteriorWorldIds)
  || alwaysAccessibleInteriorWorldIds.length === 0
  || alwaysAccessibleInteriorWorldIds.some((worldId) => (
    typeof worldId !== "string" || worldId.trim() !== worldId || !worldId
  ))
  || new Set(alwaysAccessibleInteriorWorldIds).size
    !== alwaysAccessibleInteriorWorldIds.length
) {
  throw new Error("The always-accessible interior list is invalid");
}

const layer14 = storeSource.value.overlays.find((overlay) => (
  overlay.layer === 14
));
const layer13 = storeSource.value.overlays.find((overlay) => (
  overlay.layer === 13
));
if (!layer14 || !layer13) {
  throw new Error("The two reviewed D000 standalone clock overlays are missing");
}

const entrance = onlyContainedDoor(layer14);
const presentationOnly = onlyContainedDoor(layer13);
if (
  entrance.selector !== 30
  || entrance.dispatchKind !== "direct-transition"
  || entrance.destination?.area !== "DCHA"
) {
  throw new Error("D000 layer 14 is no longer the proven selector-30 DCHA entrance");
}
if (
  presentationOnly.selector !== 63
  || presentationOnly.dispatchKind !== "native-non-transition"
  || presentationOnly.destination !== null
) {
  throw new Error("D000 layer 13 is no longer the proven selector-63 non-transition");
}

const nativeDoorTransition = doorSource.value.directTransitions.find(
  (transition) => transition.selector === entrance.selector,
);
if (
  nativeDoorTransition?.destination.area !== entrance.destination.area
  || nativeDoorTransition.destination.entry !== entrance.destination.entry
) {
  throw new Error("The native selector-30 transition disagrees with store-state evidence");
}

const worldTransition = worldSource.value.d000DirectTransitions.find(
  (transition) => (
    transition.source.doorSelector === entrance.selector
    && transition.destination.area === entrance.destination.area
    && transition.destination.entry === entrance.destination.entry
  ),
);
if (!worldTransition?.supported) {
  throw new Error("The proven selector-30 DCHA transition is not browser-supported");
}

const manifest = {
  schema: "new-yokosuka-timed-transition-access-v1",
  generatedFrom: Object.entries(sources).map(([kind, relativePath]) => {
    const source = {
      storeState: storeSource,
      doorTransitions: doorSource,
      worldTransitions: worldSource,
      worldAccessPolicy: worldAccessPolicySource,
    }[kind];
    return {
      path: relativePath,
      sha256: sha256(source.bytes),
    };
  }),
  alwaysAccessibleInteriorWorldIds: [
    ...alwaysAccessibleInteriorWorldIds,
  ],
  alwaysAccessibleInteriorPolicy: worldAccessPolicySource.value.policy,
  rules: [
    {
      id: "dobuita-layer-14-selector-30-public-hours",
      transitionId: worldTransition.id,
      source: {
        worldId: worldTransition.source.worldId,
        area: worldTransition.source.area,
        doorSelector: entrance.selector,
        layer: layer14.layer,
        position: [...entrance.sourceDoor.position],
        maximumHorizontalDistance: 10,
      },
      destination: {
        worldId: worldTransition.destination.worldId,
        area: worldTransition.destination.area,
        entry: worldTransition.destination.entry,
      },
      openWindow: { ...layer14.openWindow },
      authorizationLifetimeMs: 15_000,
      denialMessage: "This shop is closed. It is open from 8:00 AM to 7:30 PM.",
      accessDomain: "public-opening-hours",
      personalStoryFallback: {
        status: "none-reviewed",
        policy: "Mandatory personal objectives require an explicit reviewed fallback; public hours never inspect personal story progress.",
      },
      evidence: {
        overlayLayer: layer14.layer,
        dispatchKind: entrance.dispatchKind,
        sourceDoorModel: entrance.sourceDoor.model,
        selectorCompareFileOffset:
          nativeDoorTransition.selectorCompareFileOffset,
        coroutineCallFileOffset:
          nativeDoorTransition.coroutineCallFileOffset,
      },
    },
  ],
  presentationOnlyAssociations: [
    {
      sourceWorldId: "dobuita",
      layer: layer13.layer,
      doorSelector: presentationOnly.selector,
      dispatchKind: presentationOnly.dispatchKind,
      destination: null,
      openWindow: { ...layer13.openWindow },
    },
  ],
  evidenceBoundary: "Only the exact D000 layer-14/selector-30/DCHA association is an authoritative player-access rule. Layer 13 retains clock-synchronized presentation, but selector 63 is a native non-transition and is never emitted as a player portal. Scheduled-actor operation 0x2a is not an access input.",
};

const rendered = `${JSON.stringify(manifest, null, 2)}\n`;
for (const relativePath of [serverOutput]) {
  const outputPath = path.join(root, relativePath);
  await fs.mkdir(path.dirname(outputPath), { recursive: true });
  await fs.writeFile(outputPath, rendered);
}

process.stdout.write(
  `Wrote ${manifest.rules.length} timed transition access rule to `
  + `${serverOutput}.\n`,
);
