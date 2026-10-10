# Third-party assets

Everything listed here is also shown in the dashboard footer (`CREDITS` in `src/lib/models.ts`).
Keep the two in sync whenever you add or swap an asset.

## 3D vehicle models

| Asset | Author | License | Source | Files |
|---|---|---|---|---|
| Car Kit 3.1 | Kenney | [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/) | https://kenney.nl/assets/car-kit | `public/models/kenney/*` (license text in `License.txt`) |

CC0 means no attribution is legally required; we credit Kenney anyway because it's good practice.

The fleet's *real* model names (Tesla Model 3, Rivian R1T, …) are used only as labels for simulated
data. The 3D shapes are generic stylised vehicles, not replicas of those products, and no manufacturer
logos or trade dress are included.

## Design, type and icons

| Asset | License | Notes |
|---|---|---|
| Color palette & spacing | — | Inspired by Rivian's public design language (`DESIGN.md`). Only colors/spacing are borrowed; no Rivian logos, fonts or trademarks are used. |
| Inter (`@fontsource-variable/inter`) | SIL Open Font License 1.1 | Stand-in for Rivian's proprietary *Adventure* typeface |
| Lucide icons (`lucide-react`, plus inlined status glyphs) | ISC | https://lucide.dev |

## Map

Basemap tiles © [OpenStreetMap](https://www.openstreetmap.org/copyright) contributors (ODbL), shown
in the map's attribution control. The default tile server is for light use only under the
[OSM tile usage policy](https://operations.osmfoundation.org/policies/tiles/); set `VITE_MAP_TILES` to
your own provider for anything beyond local development.

## Simulator road network

`internal/simulator/routes.json` (the roads simulated vehicles drive on) was generated once by
`tools/routegen/fetch_routes.py` from the [OSRM](https://project-osrm.org) demo server.
Road geometry © [OpenStreetMap](https://www.openstreetmap.org/copyright) contributors, ODbL 1.0.
Re-running the script respects the demo server's policy (≤ 1 request/second, 11 requests total).

## Adding a realistic model (e.g. from Sketchfab)

1. **Check the license on the model page.** Good choices are **CC0** and **CC-BY 4.0**.
   Avoid *Editorial*, *NonCommercial (NC)* and *NoDerivatives (ND)* if you plan to modify or monetise,
   and never use models ripped from games. Respect any extra notes on the page (some say "NoAI").
2. Download as **glTF / .glb**. Smaller is better for the web: aim for under 5 MB. Compress with
   `npx @gltf-transform/cli optimize in.glb out.glb` if needed.
3. Put it in `public/models/<source>/` and point the entry in `src/lib/models.ts` at it. Open it
   at https://gltf.report to find node names for wheels / doors, and fill in `parts`.
4. **Attribute it** (required by CC-BY): add a row above *and* an entry to `CREDITS` in
   `src/lib/models.ts`, using the TASL format:

   > "**Title**" by **Author** is licensed under **CC BY 4.0**, source: **URL**. Changes: *what you changed*.
