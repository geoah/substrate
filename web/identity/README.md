# Identity field

A standalone playground for the substrate mark: one HTML file, no build, no
dependencies, nothing fetched. Open `index.html` in a browser, or serve the
directory (`python3 -m http.server` from here) to use it from another machine.
It is not part of the server image.

Every family of the identity brief is drawn from one figure-eight and ten
dials, one per family (07 and 09 share Volume, 10 and 11 share Order). Each
dial is an ingredient that acts on whatever else is on, so families combine
rather than trade off; "+ Blend" on a family card adds it to the current mark.
A separate Form group (Shape, Balance, Scale) shapes the loop every family
shares and is kept when a family is loaded.

## State

The whole state is the URL fragment (`#v=3&…`), rewritten on every change, so
a copied address reproduces the exact image: the seed fixes every random
choice and the renderer is deterministic. "Copy code" gives the same state as
an `sbst3.` string for pasting into "Import". "Save" keeps named states in the
browser's local storage, per origin. A key in the fragment, once shipped, is
never renamed or reused; a missing key reads as its default.

## Geometry

The curve is the figure-eight whose curvature is a smooth wave along its
length, κ(s) = A·(sin s + c·sin 3s), with A chosen so it closes (`c = 0` is
the first zero of the Bessel function J0). Curvature is zero at the crossings
and largest at the tips, so a band whose in-plane width grows the same way
never folds; Shape moves `c`.

The band is a ribbon with exactly one full twist, half a turn through each
crossing. That is the only twist that keeps the outer edge outer in both lobes
while the crossing stays open; a Möbius band swaps the edges. The two passes
are separated in depth only, never on screen, so the crossing stays where the
lines meet and both lobes stay the same size under any tilt.

Surfaces (Volume toward Manifold and Solid) use WebGL when it is there and
fall back to Canvas 2D patches when it is not.
