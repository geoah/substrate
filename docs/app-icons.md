# App icons

Every app built on a substrate has an icon in one shared frame. Each app gets
its own glyph and colour. Every icon has the same band along the bottom with
the substrate mark, three stacked layers, so on a home screen the apps read
as one family and each one is still easy to find. This page holds the rules
that keep it that way.
[`apps/xcode/scripts/icons.mjs`](../apps/xcode/scripts/icons.mjs) draws every
icon from code. Where this page and the script disagree, the
script is right and this page is the bug.

## The frame

On the 1024 px canvas, from top to bottom:

- **Background:** a pale gradient in the app's hue, from near white at the
  top to a light tint at the bottom, with a soft glow behind the glyph.
- **Glyph:** centred at (512, 352), about 480 px across, inside the top two
  thirds.
- **Band:** from y = 690 down to the bottom of the canvas. Its colour is a deeper shade of
  the same background, not black and not a contrasting colour.
- **Mark:** three stacked layers centred in the band at (512, 845), 180 px
  across, in the app's deep colour. The top layer is solid, the two under it
  are lighter, and each is cut away where the one above it sits, so the band
  shows between them.

The band and the mark are identical in shape and position on every app.
Only their shades follow the app's hue.

## Glyphs

A glyph is one simple, recognisable object, readable at 29 px:

- **Close shades of one hue.** A glyph has a front, a middle and a back
  layer, each a nearby shade of the app's colour. If the shades are far apart
  (one near white, one near black), the glyph splits into two shapes.
- **A light frosting, never gloss.** Each layer gets a faint tint from top to
  bottom, a thin light rim, and the whole glyph casts one soft shadow. Do not
  add specular highlights, glossy reflections, bevels or strong gradients:
  they look like 1990s plastic.
- **An object, not an illustration or a logo mark.** Draw a shape that says
  what the app does (layers, arrows, a dumbbell). Do not draw a scene, a
  textured material, or an abstract construction of lines and dots.
- **Matching parts share a colour.** Parts that are the same kind of thing
  (a dumbbell's plates) stay within one shade of each other.
- **Use the app's own shade family.** No rainbow palettes, and no colours from
  another app's family.

Shapes are filled and bold. Thin strokes vanish at 29 px and in the tinted
appearance.

## The apps

| App | Glyph | Hue | Background (top, bottom) | Light, deep |
| --- | --- | --- | --- | --- |
| Substrate | a window of records | blue | `#f5f9ff`, `#d9e7ff` | `#86bbff`, `#2d5fe6` |
| Substrate Sync | two arrows round a core | green | `#f2fcf8`, `#d3f2e5` | `#72e2bb`, `#0f936d` |
| Substrate Chat | a speech bubble with a spark | purple | `#f8f5ff`, `#e4dcfd` | `#bba2ff`, `#6a3fe0` |
| Substrate Workouts | a dumbbell | orange | `#fff8f2`, `#ffdfcc` | `#ffab72`, `#e3471f` |
| Substrate Tasks | a ticked checklist card | rose | `#fff5f7`, `#ffdbe4` | `#ff9bb2`, `#d92c58` |
| The template (`apps:new`) | a lidded box | grey | `#f7f8fa`, `#e1e5ea` | `#aab4c0`, `#5f6b7a` |

A new app takes a hue no other app uses, with a background pale enough that
the glyph's deep shade reads on it. The template's grey is no hue: an app
`mise run apps:new` makes starts with it and the template's box, and takes
its own entry before it ships. An app's accent, app.json's `accent`, is
its own choice in the same hue: Tasks' light accent is its deep colour,
and the template's is too.

## Appearances

Each icon ships in the three appearances iOS 18 and later can show:

- **Light:** as described above.
- **Dark:** a near-black background with a glow in the app's hue. The glyph's
  lighter shade sits in front, and the band is a dark shade of the hue.
- **Tinted:** greys only, light in front, which iOS colours itself.

The macOS sizes are the light icon as a rounded tile with margin and a shadow,
because macOS does not mask icons. The console favicon
(`web/console/public/substrate.svg`) is the light Substrate icon, rounded.

Liquid Glass (iOS 26 and later) renders these as they are. For an Icon
Composer `.icon` file, export the background, glyph, band and mark as
separate layers. Leave out the frosting's rim and shadow, because the system
adds its own.

## Android

An app on Android ships an adaptive icon: three 108 dp layers that the
launcher masks to its own shape (a circle, a squircle or a rounded square)
inside the inner 72 dp. `icons.mjs` writes them from the same entry and
glyph as the Apple icon, as vector drawables, so no density is drawn
twice:

- **Background:** the light frame's gradient, glow and band, full bleed.
  The 1024 px frame maps onto the inner 72 dp, so the masked icon is the
  light iOS icon.
- **Foreground:** the glyph and the mark, drawn at 0.88 about the
  centre so the glyph's top corners stay inside the 66 dp safe zone that
  every mask keeps. A vector drawable has no blur, so the glyph's soft
  shadow is three widening silhouettes at a low opacity. It has no masks
  either, so each layer of the mark is cut from the one below by an outline
  in the band's colour.
- **Monochrome:** the glyph's silhouette and the mark in one colour,
  which Android 13 and later tint for themed icons. The back level is
  faint, so the card still sits behind the marks.

`mipmap-anydpi/ic_launcher.xml` and `ic_launcher_round.xml` name the three
layers. The minimum SDK is 28, so there are no per-density PNGs and no
`-v26` qualifier. Write them, and the previews that show them under each
mask, themed and over the safe zone, from the repository root:

```bash
node apps/xcode/scripts/icons.mjs adaptive <app> apps/<app>/android/src/main/res
node apps/xcode/scripts/icons.mjs adaptive-previews <app> apps/<app>/docs/screenshots/android
```

The manifest names `@mipmap/ic_launcher` as the icon and
`@mipmap/ic_launcher_round` as the round icon. Substrate Tasks' previews
are in [apps/tasks/docs/screenshots/android](../apps/tasks/docs/screenshots/android).

## Adding an app

1. Add an entry to `APPS` in `icons.mjs` with a name, the background pair,
   the light and deep colours, and a glyph name.
2. Add the glyph to `GLYPHS` as parts with a level (`front`, `mid`, `back`)
   around (512, 470).
3. Render a sheet at 1024, 60 and 29 px in all three appearances and check:
   the glyph reads at 29 px, the shades sit close together, and nothing looks
   glossy.
4. Write the app's AppIcon set:
   `node apps/xcode/scripts/icons.mjs appiconset <app> apps/<app>/apple/Assets.xcassets/AppIcon.appiconset`, from the repository root
   (add `--mac` for a Mac app), and for an app on Android its adaptive
   icon ([Android](#android)).

An app in another repository copies `icons.mjs` and adds its own entry.
Frame changes are made here first, then copied.

## Directions that were rejected

The owner reviewed these on 2026-10-31 before choosing the frame above, so
they are not options:

- glossy illustrations: lit spheres, amber, geodes, glass objects;
- flat pictures on beige paper;
- abstract logo marks: monoline constructions with dot terminals, and
  overlapping tinted rectangles;
- plain white SF Symbols-style glyphs on a saturated gradient, which looked
  too basic;
- a black band in light mode that flips to white in dark mode;
- glyphs whose shades are far apart;
- an infinity for the band's mark, which reads as Meta's logo. It was the
  mark until 2026-11-04.
