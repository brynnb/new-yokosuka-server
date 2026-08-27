# Server documentation

This directory documents behavior implemented and operated by this repository.
The source code, migrations, generated manifests, and tests remain authoritative
when a document and implementation disagree.

- [Server architecture and interfaces](multiplayer-server.md): process shape,
  HTTP and WebSocket boundaries, persistence, and package ownership.
- [Scheduled NPC simulation](scheduled-npcs.md): generated inputs, runtime
  evaluation, collision avoidance, replication, and checkpoints.
- [World state](world-state.md): the authoritative clock, seasons, weather, and
  world-state broadcasts.
- [Script authoring and runtime](scripted-events.md): Yarn compilation,
  repository workflow, preview, publication, and live event execution.
- [Deployment](deployment.md): build, host layout, reverse proxy, and release
  verification.

Game extraction notes, native-operation indexes, actor research, scene catalogs,
rendering rules, asset variants, and narrative canon are intentionally outside
this repository. Those subjects belong with the extractor/research tooling or
the browser client and content-authoring project. The distilled records required
to regenerate server runtime data are documented in [`data/README.md`](../data/README.md).
