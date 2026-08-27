#!/usr/bin/env node

import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  linkedRoutesByOffset,
  runtimeJourneys,
} from "./lib/runtime_npc_manifest.js";
import { auditNPCTransitions } from "./lib/npc_transition_audit.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const sourcePath = path.join(root, "data/source/scheduled-actors.json");
const motionPath = path.join(
  root,
  "data/source/scheduled-actor-motions.json",
);
const outputPath = path.join(
  root,
  "internal/npcdata/manifest.json",
);
const auditPath = path.join(
  root,
  "internal/npcdata/transition-audit.json",
);

function runtimeActor(actor, manifest, linkedRoutes, movementProfiles) {
  if (!actor.scheduleVariants.some(
    (variant) => variant.scheduleVariantId === actor.defaultScheduleVariantId,
  )) {
    throw new Error(`Missing default schedule variant for ${actor.instanceId}`);
  }
  const scheduleVariants = actor.scheduleVariants.map((variant) => {
    const journeys = runtimeJourneys(actor, variant, linkedRoutes);
    for (const journey of journeys) {
      for (const operation of journey.operations) {
        if (operation.operation !== 1) continue;
        const key = `0x${Number.parseInt(operation.movementMode, 16).toString(16)}`;
        const step = movementProfiles[key]?.nativePathStepPerUpdate;
        if (!(step > 0)) {
          throw new Error(
            `Missing native motion path step for ${operation.movementMode}`,
          );
        }
        operation.nativePathStepPerUpdate = step;
      }
    }
    return {
      scheduleVariantId: variant.scheduleVariantId,
      selectorIndices: variant.selectorIndices,
      journeys,
    };
  });
  return {
    instanceId: actor.instanceId,
    actorCode: actor.actorCode,
    label: actor.label,
    modelCode: actor.modelCode,
    modelOverrides: actor.modelOverrides || [],
    nativeDefaultPathSpeedPerGameSecond:
      actor.nativeDefaultPathSpeedPerGameSecond,
    nativeDefaultMotionStateId: actor.nativeDefaultMotionStateId,
    defaultArea: actor.defaultArea,
    playbackWorldIds: actor.playbackWorldIds,
    scheduleSelector: actor.scheduleSelector,
    defaultScheduleVariantId: actor.defaultScheduleVariantId,
    scheduleVariants,
  };
}

const manifest = JSON.parse(await fs.readFile(sourcePath, "utf8"));
const motionManifest = JSON.parse(await fs.readFile(motionPath, "utf8"));
const linkedRoutes = linkedRoutesByOffset(manifest);
const actors = manifest.actors.map(
  (actor) => runtimeActor(
    actor,
    manifest,
    linkedRoutes,
    motionManifest.movementProfiles,
  ),
);
const output = {
  schema: "new-yokosuka-server-npcs-v1",
  generatedFrom: "data/source/scheduled-actors.json",
  areaWorlds: manifest.areaWorlds,
  actors,
};

await fs.mkdir(path.dirname(outputPath), { recursive: true });
await fs.writeFile(outputPath, `${JSON.stringify(output)}\n`);
const transitions = auditNPCTransitions(actors.flatMap((actor) => (
  actor.scheduleVariants.map((variant) => ({
    ...actor,
    scheduleVariantId: variant.scheduleVariantId,
    journeys: variant.journeys,
  }))
)));
const classifications = {};
for (const transition of transitions) {
  classifications[transition.classification] = (
    classifications[transition.classification] || 0
  ) + 1;
}
await fs.writeFile(auditPath, `${JSON.stringify({
  schema: "new-yokosuka-npc-transition-audit-v1",
  generatedFrom: "internal/npcdata/manifest.json",
  summary: {
    transitionCount: transitions.length,
    visibleWarpCount: transitions.filter(
      (transition) => transition.visibleWarp,
    ).length,
    classifications,
  },
  transitions,
})}\n`);
process.stdout.write(
  `Wrote ${actors.length} server NPC definitions to `
  + `${path.relative(root, outputPath)} and audited `
  + `${transitions.length} route transitions.\n`,
);
