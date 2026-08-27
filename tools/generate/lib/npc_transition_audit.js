function separation(left, right) {
  return Math.hypot(...left.map((value, axis) => value - right[axis]));
}

function operationRoutes(operation) {
  if (operation.operation === 1 && operation.points?.length) {
    return [{
      id: operation.routeId,
      kind: "schedule-route",
      points: operation.points,
      objectCode: null,
    }];
  }
  if (operation.operation === 0x16) {
    return [
      operation.linkedPlacement?.controllerRoutePoints?.length
        ? {
            id: operation.linkedPlacement.controllerRouteId,
            kind: "linked-controller",
            points: operation.linkedPlacement.controllerRoutePoints,
            objectCode: null,
          }
        : null,
      operation.linkedPlacement?.handoffRoutePoints?.length
        ? {
            id: operation.linkedPlacement.handoffRouteId,
            kind: "recovered-handoff",
            points: operation.linkedPlacement.handoffRoutePoints,
            objectCode: null,
          }
        : null,
    ].filter(Boolean);
  }
  return [];
}

function classification(from, to, distance) {
  if (to.kind === "recovered-handoff") {
    return {
      classification: "recovered-linked-controller-handoff",
      visibleWarp: false,
    };
  }
  if (distance <= 0.05) {
    return {
      classification: "continuous",
      visibleWarp: false,
    };
  }
  return {
    // The extraction proves both route endpoints but contains no connecting
    // path. Keeping this explicit prevents an invented straight line through
    // buildings and makes every remaining discontinuity inspectable.
    classification: "authored-discontinuity-no-recovered-connector",
    visibleWarp: true,
  };
}

export function auditNPCTransitions(actors) {
  const transitions = [];
  for (const actor of actors) {
    for (const journey of actor.journeys || []) {
      let previous = null;
      for (const operation of journey.operations || []) {
        for (const route of operationRoutes(operation)) {
          if (previous) {
            const distance = separation(
              previous.points.at(-1),
              route.points[0],
            );
            transitions.push({
              actorId: actor.instanceId,
              scheduleVariantId: actor.scheduleVariantId || null,
              journeyStartSecond: journey.startSecond,
              fromRouteId: previous.id,
              toRouteId: route.id,
              distance,
              ...classification(previous, route, distance),
            });
          }
          previous = route;
        }
      }

      const secondary = (journey.operations || [])
        .filter((operation) => (
          operation.operation === 0x1c
          && operation.routeId
          && operation.secondaryRoute?.points?.length
        ))
        .map((operation) => ({
          id: operation.routeId,
          kind: "secondary-route",
          points: operation.secondaryRoute.points,
          objectCode: operation.secondaryObjectCode,
        }));
      for (let index = 0; index < secondary.length; index++) {
        const from = secondary[index];
        const to = secondary[(index + 1) % secondary.length];
        const distance = separation(from.points.at(-1), to.points[0]);
        const objectChanged = from.objectCode !== to.objectCode;
        transitions.push({
          actorId: actor.instanceId,
          scheduleVariantId: actor.scheduleVariantId || null,
          journeyStartSecond: journey.startSecond,
          fromRouteId: from.id,
          toRouteId: to.id,
          distance,
          classification: distance <= 0.05
            ? "continuous-secondary-loop"
            : distance <= 10
              ? "native-nearby-secondary-handoff"
            : objectChanged
              ? "authored-secondary-object-switch"
              : "native-secondary-controller-snap",
          visibleWarp: distance > 10,
        });
      }
    }
  }
  return transitions;
}
