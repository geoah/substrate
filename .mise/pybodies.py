"""Every Python function body and bundle module under kinds/ and samples/, as files.

    python3 .mise/pybodies.py lint [PATH...]   # `lint:python`
    python3 .mise/pybodies.py audit            # `audit:python`
    python3 .mise/pybodies.py extract DIR

The bodies are YAML literal scalars, so no linter and no audit reads them
where they live. This writes each one to a temporary tree and points the tool
there. `lint` runs ruff over the PATHs (from the repository root) and over
every body, and fails if either has a finding. `audit` resolves each PEP 723
block with `uv export --script` and runs pip-audit over the resolved set.
`extract` writes the tree to DIR and stops.

A body is the `source: |` block of a document declaring `runtime: python`, or
a `<name>.py: |` entry under a bundle document's `modules:`. It is written to
`<yaml path>.<function>.py` or `<yaml path>.modules.<name>.py`, padded with
blank lines so a line number in the file IS the line in the YAML file, and
`lint` rewrites ruff's paths and columns back onto the YAML file. A
per-file-ignores pattern in .ruff.toml names the written path.

A python document whose source this cannot read is a failure, never a skip:
the number of bodies written must equal the number of documents declaring
`runtime: python`, or the lint would pass over code it never saw.

Stdlib only, and no YAML parser: it runs on whatever python3 the machine has.
"""

import os
import re
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TREES = ("kinds", "samples")
CONFIG = os.path.join(ROOT, ".ruff.toml")

# The COUNT the extraction is held to, read loosely (any depth, key and value
# quoted or not) so a spelling the extractor misses is a mismatch rather than a
# silent skip. kinds/kinds_test.go holds both to a real YAML parser.
RUNTIME_PYTHON = re.compile(r"""^\s*(["']?)runtime\1:\s*(["']?)python\2\s*(#.*)?$""")
# A literal block under a key: `source: |`, `"name.py": |-`. An indentation
# indicator (`|2`) does not match, because the dedent below takes the indent
# from the block's first line.
LITERAL = re.compile(r"""^(\s*)(["']?)([A-Za-z0-9_.-]+)\2:\s*\|[-+]?\s*(#.*)?$""")
# A top-level key, so `modules:` is read where the bundle declares it
# (`data.modules`) whatever the document's `kind:` line looks like.
TOP_KEY = re.compile(r"""^(["']?)([A-Za-z_][A-Za-z0-9_-]*)\1:(\s.*)?$""")
METADATA_ID = re.compile(r"""^  (["']?)id\1:\s*(["']?)([^"'\s#]+)\2\s*(#.*)?$""")
MODULES = re.compile(r"""^  (["']?)modules\1:(.*)$""")
# PEP 723's own regex, the one internal/runner/pep723.go matches with.
PEP723 = re.compile(r"(?m)^# /// (?P<type>[a-zA-Z0-9-]+)$\s(?P<content>(^#(| .*)$\s)+)^# ///$")


class Body:
    def __init__(self, yaml, name, line, indent, text, function):
        self.yaml = yaml  # repository-relative YAML path
        self.name = name  # "<function>.py" or "modules.<name>.py"
        self.line = line  # 1-based YAML line of the body's first line
        self.indent = indent  # the body's column offset in the YAML
        self.text = text  # dedented, padded so its lines are the YAML's
        self.function = function

    @property
    def path(self):
        return self.yaml + "." + self.name


class Unreadable(Exception):
    pass


def indent_of(line):
    return len(line) - len(line.lstrip(" "))


def child(line, parent_indent):
    """Whether `line` belongs to the node opened at `parent_indent`."""
    return not line.strip() or indent_of(line) > parent_indent


def documents(lines):
    start = 0
    for i, line in enumerate(lines):
        if line.rstrip() == "---":
            yield start, i
            start = i + 1
    yield start, len(lines)


def literal(rel, lines, key, end):
    """The literal block under lines[key]: its first line, indent and text."""
    block = []
    i = key + 1
    while i < end and child(lines[i], indent_of(lines[key])):
        block.append(lines[i])
        i += 1
    while block and not block[-1].strip():
        block.pop()
    if not block:
        raise Unreadable("%s:%d: the block is empty" % (rel, key + 1))
    ind = indent_of(next(line for line in block if line.strip()))
    for n, line in enumerate(block):
        if line.strip() and indent_of(line) < ind:
            raise Unreadable("%s:%d: left of the block's first line, which sets its indent"
                             % (rel, key + 2 + n))
    first = key + 2
    text = "\n" * (first - 1) + "\n".join(line[ind:] for line in block) + "\n"
    return first, ind, text


def under(lines, start, end, key):
    """The line indexes of the document's top-level `key:` mapping."""
    top = ""
    for i in range(start, end):
        m = TOP_KEY.match(lines[i])
        if m:
            top = m.group(2)
        elif top == key:
            yield i


def metadata_id(lines, start, end):
    for i in under(lines, start, end, "metadata"):
        m = METADATA_ID.match(lines[i])
        if m:
            return m.group(3)
    return ""


def function_body(rel, lines, start, end):
    keys = [i for i in range(start, end)
            if LITERAL.match(lines[i]) and LITERAL.match(lines[i]).group(3) == "source"]
    if len(keys) != 1:
        raise Unreadable("%s:%d: a `runtime: python` document with %d `source: |` blocks, not one"
                         % (rel, start + 1, len(keys)))
    ident = metadata_id(lines, start, end)
    if not ident:
        raise Unreadable("%s:%d: a `runtime: python` document with no metadata.id"
                         % (rel, start + 1))
    first, ind, text = literal(rel, lines, keys[0], end)
    return Body(rel, ident.rsplit("/", 1)[-1] + ".py", first, ind, text, True)


def modules(rel, lines, start, end):
    """Every `<name>.py: |` entry under a document's `data.modules`, which only
    the bundle kind declares."""
    out = []
    for i in under(lines, start, end, "data"):
        m = MODULES.match(lines[i])
        if not m:
            continue
        rest = m.group(2).strip()
        if rest and not rest.startswith("#"):
            raise Unreadable("%s:%d: `modules:` is read as a block mapping of literal blocks"
                             % (rel, i + 1))
        entry = None
        j = i + 1
        while j < end and child(lines[j], 2):
            line = lines[j]
            if line.strip() and not line.lstrip().startswith("#"):
                if entry is None:
                    entry = indent_of(line)
                if indent_of(line) == entry:
                    e = LITERAL.match(line)
                    if not e or not e.group(3).endswith(".py"):
                        raise Unreadable("%s:%d: a module is a `<name>.py: |` literal block"
                                         % (rel, j + 1))
                    first, ind, text = literal(rel, lines, j, end)
                    out.append(Body(rel, "modules." + e.group(3), first, ind, text, False))
            j += 1
    return out


def scan(rel, text):
    """One YAML file's bodies, its count of python documents, and every
    problem that kept a body from being read."""
    lines = text.split("\n")
    bodies, problems, python_docs = [], [], 0
    for start, end in documents(lines):
        try:
            if any(RUNTIME_PYTHON.match(line) for line in lines[start:end]):
                python_docs += 1
                bodies.append(function_body(rel, lines, start, end))
            bodies.extend(modules(rel, lines, start, end))
        except Unreadable as e:
            problems.append(str(e))
    return bodies, python_docs, problems


def collect(root=ROOT):
    """Every body in the tree, or exit 1 naming each one that could not be read."""
    bodies, problems, python_docs = [], [], 0
    for top in TREES:
        for dirpath, dirnames, filenames in os.walk(os.path.join(root, top)):
            dirnames.sort()
            for name in sorted(filenames):
                if not name.endswith(".yaml"):
                    continue
                path = os.path.join(dirpath, name)
                rel = os.path.relpath(path, root).replace(os.sep, "/")
                with open(path, encoding="utf-8") as f:
                    found, docs, unread = scan(rel, f.read())
                bodies += found
                python_docs += docs
                problems += unread
    functions = sum(1 for b in bodies if b.function)
    if functions != python_docs:
        problems.append("%d documents declare `runtime: python` and %d bodies were read"
                        % (python_docs, functions))
    if not python_docs:
        problems.append("no document under %s declares `runtime: python`: the reader no longer "
                        "matches the tree" % " or ".join(TREES))
    paths = [b.path for b in bodies]
    problems += ["two bodies would be written to %s" % p for p in sorted(set(paths)) if paths.count(p) > 1]
    if problems:
        for p in problems:
            print("pybodies: " + p, file=sys.stderr)
        sys.exit(1)
    return bodies


def write(bodies, out):
    for b in bodies:
        path = os.path.join(out, b.path)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(b.text)


def onto_yaml(output, bodies):
    """ruff's output with every written path, line and column put back onto
    the YAML file the body lives in."""
    where = {b.path: b for b in bodies}
    pattern = re.compile("(%s):(\\d+):(\\d+)" % "|".join(
        re.escape(p) for p in sorted(where, key=len, reverse=True)))

    def back(m):
        b = where[m.group(1)]
        return "%s:%s:%d" % (b.yaml, m.group(2), int(m.group(3)) + b.indent)
    return pattern.sub(back, output)


def run(argv, **kw):
    try:
        return subprocess.run(argv, **kw)
    except FileNotFoundError:
        sys.exit("pybodies: `%s` is not on PATH; run this through its mise task" % argv[0])


def lint(paths):
    bodies = collect()
    status = 0
    if paths:
        status = run(["ruff", "check", "--config", CONFIG] + paths, cwd=ROOT).returncode
    with tempfile.TemporaryDirectory(prefix="pybodies-") as tmp:
        write(bodies, tmp)
        # From inside the tree, so the paths ruff prints and the
        # per-file-ignores patterns it resolves (against the working directory,
        # under --config) are both the repository-relative written paths.
        # Every file is named, so ruff's default excludes (`build`, `dist`)
        # cannot drop a package that happens to be called that.
        r = run(["ruff", "check", "--config", CONFIG, "--no-cache"] + [b.path for b in bodies],
                cwd=tmp, stdout=subprocess.PIPE, text=True)
    sys.stdout.write(onto_yaml(r.stdout, bodies))
    sys.stdout.flush()
    print("pybodies: linted %d function bodies and %d modules"
          % (sum(1 for b in bodies if b.function), sum(1 for b in bodies if not b.function)),
          file=sys.stderr)
    return max(status, r.returncode)


def audit():
    scripts = []
    for b in collect():
        if not b.function:
            continue
        block = next((m.group(0) for m in PEP723.finditer(b.text) if m.group("type") == "script"), None)
        if block:
            scripts.append((b, block))
    if not scripts:
        print("pybodies: no function body carries a `# /// script` block: nothing to audit, "
              "which is more likely a reader that stopped matching", file=sys.stderr)
        return 1
    failed = []
    with tempfile.TemporaryDirectory(prefix="pybodies-") as tmp:
        for n, (b, block) in enumerate(scripts):
            label = "%s:%d (%s)" % (b.yaml, b.line, b.name[:-3])
            print("pybodies: %s" % label, flush=True)
            # The block alone, as the runner embeds it atop its uv host script.
            script = os.path.join(tmp, "script%d.py" % n)
            with open(script, "w", encoding="utf-8") as f:
                f.write(block + "\n")
            reqs = script + ".txt"
            # `uv export --script`, not `uv pip compile`: it is the resolution
            # `uv sync --script` makes at registration, and it reads the
            # block's own `[tool.uv]` table, which `uv pip compile` ignores.
            # Hashes come with it, which lets pip-audit take the file as the
            # whole set without resolving it again through pip.
            steps = [
                ["uv", "export", "--quiet", "--script", script, "--format", "requirements.txt",
                 "--no-header", "--output-file", reqs],
                ["pip-audit", "--disable-pip", "--strict", "--progress-spinner", "off",
                 "--requirement", reqs],
            ]
            for step in steps:
                if run(step).returncode != 0:
                    failed.append(label)
                    break
    for label in failed:
        print("pybodies: %s failed its audit" % label, file=sys.stderr)
    print("pybodies: audited %d PEP 723 blocks" % len(scripts), file=sys.stderr)
    return 1 if failed else 0


def main(argv):
    if len(argv) >= 2 and argv[1] == "lint":
        return lint(argv[2:])
    if len(argv) == 2 and argv[1] == "audit":
        return audit()
    if len(argv) == 3 and argv[1] == "extract":
        bodies = collect()
        write(bodies, argv[2])
        print("pybodies: wrote %d bodies under %s" % (len(bodies), argv[2]), file=sys.stderr)
        return 0
    print(__doc__.strip().split("\n\n")[1], file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
