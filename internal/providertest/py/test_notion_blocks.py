"""The notion sync body's typed block payloads, without a database or uv.

bodyunit_test.go writes the `notionsync` function body of
kinds/providers.substrate.reamde.dev/notion/bundle.yaml into the directory
named by SUBSTRATE_BODIES_DIR and runs this file with `-I`, under a Python the
body's `requires-python` admits, which uv finds. The body's top level is
exec'd into its own namespace, exactly the code the runner registers, so a
case calls the shipped helper and not a copy of it.

The body imports `requests`, which that interpreter need not have, so an empty
stub is installed in `sys.modules` first: no case here sends a request.
"""

import os
import sys
import types
import unittest


def _load():
    sys.modules.setdefault("requests", types.ModuleType("requests"))
    path = os.path.join(os.environ["SUBSTRATE_BODIES_DIR"], "notionsync.py")
    with open(path, encoding="utf-8") as f:
        source = f.read()
    namespace = {"__name__": "notionsync"}
    exec(compile(source, path, "exec"), namespace)
    return namespace


BODY = _load()


class ColumnTest(unittest.TestCase):
    """`column.widthRatio` is declared a decimal, and the engine refuses a
    decimal written as a bare JSON number: one such block parked the whole
    sync of the page that held it."""

    def payload(self, value):
        return BODY["_block_payload"](None, "acct", "column", value)

    def test_a_width_ratio_is_written_as_a_string(self):
        self.assertEqual(self.payload({"width_ratio": 0.5}), {"widthRatio": "0.5"})
        self.assertEqual(self.payload({"width_ratio": 19.99}), {"widthRatio": "19.99"})
        self.assertEqual(self.payload({"width_ratio": 1}), {"widthRatio": "1"})

    def test_a_small_ratio_has_no_exponent(self):
        self.assertEqual(self.payload({"width_ratio": 0.00001}), {"widthRatio": "0.00001"})

    def test_a_column_without_a_ratio_writes_none(self):
        self.assertIsNone(self.payload({}))
        self.assertIsNone(self.payload({"width_ratio": None}))

    def test_a_ratio_that_is_no_number_is_not_written(self):
        for value in (True, float("nan"), float("inf"), [0.5], {"n": 1}):
            self.assertIsNone(self.payload({"width_ratio": value}), value)


class DecimalTest(unittest.TestCase):
    def test_every_typed_payload_field_declared_decimal_has_the_cast(self):
        # The one decimal field among the typed payloads today; a second one
        # added with no cast would park a sync the same way.
        casts = {name: cast for fields in BODY["TYPED_PAYLOADS"].values()
                 for _, name, cast in fields}
        self.assertIs(casts["widthRatio"], BODY["_decimal"])


if __name__ == "__main__":
    unittest.main()
