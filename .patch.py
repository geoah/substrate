import re

# Keep MAIN's prose (the samples branch rewrote it) and apply only the kind
# rename onto it; keep main's version numbers and bump the llm package by one.
resolutions = {
    "samples/notes/bundle.yaml": [
        (
            "<<<<<<< HEAD\n"
            "# The AGENTS need one thing more: an `llmprovider` row at the id they name\n"
            "# (`default`), which is what the llm sample ships. Import that one too and\n"
            "# fill in its key, or the first agent run refuses at dispatch saying which row\n"
            "# it wanted. See docs/bundles-catalog.md.\n"
            "=======\n"
            "# The agents want an `llm/provider` row at the id they name (`default`); the\n"
            "# functions want nothing at all. See docs/bundles-catalog.md.\n"
            ">>>>>>> d511359 (feat(core)!: the llm kinds move to the substrate.reamde.dev/llm package)\n",
            "# The AGENTS need one thing more: an `llm/provider` row at the id they name\n"
            "# (`default`), which is what the llm sample ships. Import that one too and\n"
            "# fill in its key, or the first agent run refuses at dispatch saying which row\n"
            "# it wanted. See docs/bundles-catalog.md.\n",
        )
    ],
    "samples/pebble/bundle.yaml": [
        (
            "<<<<<<< HEAD\n"
            "# The AGENT wants an `llmprovider` row at the id it names (`default`), which\n"
            "# is what the llm sample ships: import that one too and fill in its key, or\n"
            "# the first agent run refuses at dispatch saying which row it wanted. The\n"
            "=======\n"
            "# The agent wants an `llm/provider` row at the id it names (`default`); the\n"
            ">>>>>>> d511359 (feat(core)!: the llm kinds move to the substrate.reamde.dev/llm package)\n",
            "# The AGENT wants an `llm/provider` row at the id it names (`default`), which\n"
            "# is what the llm sample ships: import that one too and fill in its key, or\n"
            "# the first agent run refuses at dispatch saying which row it wanted. The\n",
        )
    ],
    "samples/llm/bundle.yaml": [
        (
            "<<<<<<< HEAD\n"
            "#   2. Data → llmproviders → `default` → Edit, and put your key in `apiKey`.\n"
            "=======\n"
            "#   2. Data → providers → `anthropic` → Edit, and put your key in `apiKey`.\n"
            ">>>>>>> d511359 (feat(core)!: the llm kinds move to the substrate.reamde.dev/llm package)\n",
            "#   2. Data → providers → `default` → Edit, and put your key in `apiKey`.\n",
        ),
        (
            "<<<<<<< HEAD\n  version: 14\n=======\n  version: 13\n"
            ">>>>>>> d511359 (feat(core)!: the llm kinds move to the substrate.reamde.dev/llm package)\n",
            "  version: 15\n",
        ),
    ],
}
for p, subs in resolutions.items():
    s = open(p).read()
    for a, b in subs:
        if a not in s:
            print("MISS", p, a[:60])
        s = s.replace(a, b, 1)
    if "<<<<<<<" in s:
        print("STILL CONFLICTED", p)
    open(p, "w").write(s)
print("done")
