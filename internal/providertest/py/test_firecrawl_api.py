"""Where the Firecrawl bodies send the key.

bodyunit_test.go writes the `firecrawl_api.py` module of
kinds/providers.substrate.reamde.dev/firecrawl/bundle.yaml into the directory
named by SUBSTRATE_BODIES_DIR and runs this file with `-I`. The module is
imported from there, exactly the code the runner puts on a body's path.

No case opens a socket: the opener is replaced by a recorder wherever a
request would be sent. TestFirecrawlProviderRefusesRedirects in Go follows a
real redirect through the runner.
"""

import importlib.util
import os
import unittest
import urllib.request

ORIGIN = "https://api.firecrawl.dev"


def _load():
    path = os.path.join(os.environ["SUBSTRATE_BODIES_DIR"], "firecrawl_api.py")
    spec = importlib.util.spec_from_file_location("firecrawl_api", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


api = _load()


class _Recorder:
    """Stands in for the opener: records each request, answers success."""

    def __init__(self):
        self.requests = []

    def open(self, req, timeout=None):
        self.requests.append(req)
        return _Answer()


class _Answer:
    def __enter__(self):
        return self

    def __exit__(self, *_):
        return False

    def read(self):
        return b'{"success": true}'


class OriginTest(unittest.TestCase):
    def test_no_base_url_is_the_pinned_origin(self):
        # What an operator-paid call gets: the key and no baseUrl.
        self.assertEqual(api.origin({"apiKey": "k"}), ORIGIN)

    def test_the_pinned_origin_spelled_with_its_port(self):
        self.assertEqual(api.origin({"baseUrl": "https://api.firecrawl.dev:443/"}), ORIGIN)

    def test_loopback_is_the_test_seam(self):
        self.assertEqual(api.origin({"baseUrl": "http://127.0.0.1:8080"}), "http://127.0.0.1:8080")
        self.assertEqual(api.origin({"baseUrl": "http://[::1]:8080"}), "http://[::1]:8080")
        self.assertEqual(api.origin({"baseUrl": "http://localhost:9"}), "http://localhost:9")

    def test_every_other_origin_refuses(self):
        for base in (
            "https://evil.example.com",
            "https://api.firecrawl.dev:8443",
            "http://api.firecrawl.dev",
            "https://api.firecrawl.dev.evil.example.com",
            "https://localhost.evil.example.com",
            "ftp://127.0.0.1",
        ):
            with self.subTest(base=base), self.assertRaisesRegex(ValueError, "pinned provider origin"):
                api.origin({"baseUrl": base})

    def test_a_path_credentials_or_query_refuse(self):
        for base, words in (
            ("https://api.firecrawl.dev/v2", "carries a path"),
            ("https://api.firecrawl.dev/?x=1", "carries a path"),
            ("https://user:pw@api.firecrawl.dev", "embeds credentials"),
        ):
            with self.subTest(base=base), self.assertRaisesRegex(ValueError, words):
                api.origin({"baseUrl": base})


class PostTest(unittest.TestCase):
    def setUp(self):
        self.real = api._OPENER
        self.recorder = _Recorder()
        api._OPENER = self.recorder

    def tearDown(self):
        api._OPENER = self.real

    def test_the_key_alone_goes_to_the_pinned_origin(self):
        api.post({"apiKey": "fc-k"}, "/v2/search", {"query": "q"})
        (req,) = self.recorder.requests
        self.assertEqual(req.full_url, ORIGIN + "/v2/search")
        self.assertEqual(req.get_header("Authorization"), "Bearer fc-k")
        self.assertEqual(req.get_method(), "POST")

    def test_an_empty_key_sends_nothing(self):
        with self.assertRaisesRegex(ValueError, "API key is empty"):
            api.post({"baseUrl": "http://127.0.0.1:9"}, "/v2/search", {})
        self.assertEqual(self.recorder.requests, [])

    def test_a_refused_origin_sends_nothing(self):
        with self.assertRaises(ValueError):
            api.post({"apiKey": "fc-k", "baseUrl": "https://evil.example.com"}, "/v2/search", {})
        self.assertEqual(self.recorder.requests, [])


class OpenerTest(unittest.TestCase):
    def test_every_redirect_handler_refuses(self):
        handlers = [h for h in self.real_handlers() if isinstance(h, urllib.request.HTTPRedirectHandler)]
        self.assertEqual(len(handlers), 1)
        self.assertIsInstance(handlers[0], api._RefuseRedirect)

    def test_a_redirect_raises_naming_it(self):
        req = urllib.request.Request(ORIGIN + "/v2/search")
        with self.assertRaisesRegex(Exception, "redirect"):
            api._RefuseRedirect().redirect_request(req, None, 302, "Found", {}, "https://evil.example.com/v2/search")

    def real_handlers(self):
        return api._OPENER.handlers


if __name__ == "__main__":
    unittest.main(verbosity=2)
