# Distilled server-data inputs

Files in `data/source` are the self-contained, versioned inputs needed to
regenerate the server's embedded NPC, transition-access, vending, and avatar
definitions. No client checkout or local game-asset directory is required.

Run `npm run generate` from the repository root after changing an input. The
generators write only to the corresponding files under `internal`.

These records contain reverse-engineering observations and names associated
with Shenmue. No disc image, executable, model, texture, audio, or arcade ROM is
included. The repository's MIT license does not claim rights in the underlying
game or its content.
