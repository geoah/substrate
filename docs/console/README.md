# Console design

The design documentation for the web console, written over its redesign
(PR #648). [The web console](../console.md) describes each page as a user
meets it; these pages describe the system behind them and how it was
reached.

- [Design guide](design-guide.md): what the console is for, its two readers,
  navigation, layout, the tokens, the words and the accessibility rules.
- [Elements](elements.md): every shared component, what it is for, its
  props, where it is used and the rule it holds.
- Reviews:
  - [2026-09-24, the first review](reviews/2026-09-24-first-review.md): the
    console before the redesign, what was wrong with it, and the proposal.
  - [2026-09-26, the design review](reviews/2026-09-26-design-review.md):
    the redesign measured against the prototype and the guide, each finding
    with its status now.
- [The prototype](prototype/console-prototype.html): the approved static
  prototype of the Paper direction, on example data. Open it in a browser;
  **Everyday** and **Technical** switch the reader. It is the reference the
  build was made from, not the product: where it and the console differ, the
  console is what ships.

The decisions made over the redesign:

- [0133](../decisions/0133-a-kind-declares-its-purpose.md): a kind declares
  its purpose, so navigation lists what a person browses.
- [0130](../decisions/0130-the-console-serves-four-things-and-its-navigation-follows-them.md):
  the console serves four things, and its navigation follows them.
- [0131](../decisions/0131-the-console-writes-for-two-readers-behind-one-switch.md):
  the console writes for two readers, and one switch tells them apart.
- [0132](../decisions/0132-console-preferences-follow-the-person-and-a-window-fact-stays-in-the-browser.md):
  console preferences follow the person, and a window's own facts stay in
  the browser.
