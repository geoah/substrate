/// Code as people paste it, a sample for each language the highlighter
/// reads: each with what its language lets run over lines, and with the
/// things that are easy to colour wrongly. What the highlighter's tests
/// read and a gallery draws. No sample imports a module by a bare name
/// but Swift's: the check that keeps this package portable reads a file's
/// lines, a sample's among them.
public enum CodeFixtures {
    public static let swift = #"""
        import Foundation

        /// A counter, kept on the main actor.
        @MainActor
        struct Counter {
            private var count = 0 /* nested /* twice */ still */
            let label = "Count: \(count + ["a": 1]["a"]!)"
            let raw = #"A "quoted" word"#
            func bump(by amount: Int = 0x1F) async throws -> Int {
                #if DEBUG
                print("""
                    several "lines"
                    """)
                #endif
                for step in 1 ..< 5 where step.isMultiple(of: 2) { count += step }
                return count + amount // as it stands
            }
            var kind: Kind { .default }
        }
        """#

    public static let go = #"""
        package main

        import "fmt"

        // Greet says hello.
        func Greet(names []string) (int, error) {
            query := `SELECT *
                FROM people WHERE name = 'x'`
            for i, name := range names {
                fmt.Printf("%d: %s\n", i, name)
            }
            r := 'a' /* a rune */
            return len(names) + 0x10, nil
        }
        """#

    public static let python = #"""
        from re import search
        from dataclasses import dataclass

        @dataclass
        class Point:
            """A point
            on a plane."""
            x: float = 1.5e-3
            y: float = 0

            def norm(self) -> float:  # its length
                pattern = search(r"\d+'s", f'{self.x}')
                return (self.x ** 2 + self.y ** 2) ** 0.5 if pattern is None else 0

        match = Point(1, 2).norm() // 2
        """#

    public static let javascript = #"""
        import { readFile } from "node:fs/promises";

        /* Reads a file
           and counts its words. */
        export async function count(path, { trim = true } = {}) {
          const text = await readFile(path, 'utf8');
          const words = text.split(/\s+["']?/g).filter(Boolean);
          const half = words.length / 2 / 1;
          console.log(`found ${words.length} words in ${path.replace(/`/g, "")}
            over ${text.split("\n").length} lines`);
          return trim ? words.map((w) => w.trim()) : words; // done
        }
        const el = <p className="x">It's here</p>;
        """#

    public static let typescript = #"""
        interface Task { id: string; done?: boolean }

        type Listed<T> = readonly T[];

        @Injectable()
        export class Tasks implements Store<Task> {
          private items: Listed<Task> = [];
          constructor(private readonly type: string) {}
          open(): Task[] {
            const from = "the start";
            return this.items.filter((t) => !t.done) as Task[];
          }
        }
        """#

    public static let json = #"""
        {
          "name": "substrate",
          "version": 3,
          "ratio": -1.5e3,
          "tags": ["a", "b: c"],
          "nested": { "ok": true, "none": null },
          "url": "https://example.com/a?b=1" // where it lives
        }
        """#

    public static let yaml = #"""
        # A workflow
        ---
        name: Build and test
        on:
          push:
            branches: [main, "release/*"]
        env:
          RETRIES: 3
          VERBOSE: true
          VERSION: 1.2.3
          URL: https://example.com:8080/path # not a key
        jobs:
          - name: test
            run: |
              echo "key: value"
              if true; then exit 1; fi
            at: 12:30
            anchors: &base
              image: 'it''s'
            use: *base
        """#

    public static let shell = #"""
        #!/usr/bin/env bash
        set -euo pipefail

        NAME="${1:-world}" # who to greet
        for file in *.txt; do
          if [ -f "$file" ] && grep -q 'done' "$file"; then
            echo "Hello, $NAME: $file is done"
          fi
        done
        cat <<EOF
        if "this" were code, it's not: $NAME
        EOF
        echo 'several
        lines' | wc -l
        export PATH="$HOME/bin:$PATH"
        echo $HOME ${USER} done
        """#

    public static let sql = #"""
        -- Tasks due this week
        SELECT t.id, t.title, count(*) AS n
        FROM tasks t
        LEFT JOIN people p ON p.id = t.owner
        WHERE t.due <= '2026-11-20' AND t.title LIKE 'it''s %'
          /* open
             ones */
          AND t."order" IS NOT NULL
        GROUP BY t.id
        ORDER BY n DESC
        LIMIT 10;
        """#

    public static let html = #"""
        <!DOCTYPE html>
        <html lang="en">
          <head>
            <!-- the page's
                 head -->
            <style>
              body { color: red; }
            </style>
            <script>
              if (a < b) { console.log("x") }
            </script>
          </head>
          <body class="page" data-id='7'>
            <p>Fish &amp; chips, it's 5 < 6</p>
            <br />
          </body>
        </html>
        """#

    public static let css = #"""
        @import "base.css";

        /* The page */
        :root { --accent: #6A3FE0; }

        a:hover, .row > .cell-2 {
          color: var(--accent);
          margin: -4px 0 1.5rem;
          background: url("img/a.png") no-repeat;
        }

        @media (min-width: 600px) {
          #main h1 { font-size: 200%; }
        }
        """#

    public static let rust = #"""
        use std::collections::HashMap;

        /// Counts the words.
        #[derive(Debug, Clone)]
        pub struct Count<'a> {
            words: HashMap<&'a str, usize>,
        }

        impl<'a> Count<'a> {
            pub fn add(&mut self, text: &'a str) -> usize {
                let pattern = r#"a "raw"
                    string"#;
                for word in text.split(' ') {
                    *self.words.entry(word).or_insert(0) += 1; /* one /* more */ */
                }
                let message = "several
                    lines";
                let r#type = b'x';
                self.words.len() + pattern.len() + 0xFF_usize
            }
        }
        """#

    public static let kotlin = #"""
        package com.example

        /** A greeter. */
        @Suppress("unused")
        data class Greeter(val name: String, private val times: Int = 3) {
            suspend fun greet(): String {
                val text = """
                    Hello, "$name"
                """.trimIndent()
                delay(1_000L)
                return "${text.map { "$it" }.joinToString("")}: ${times}x" // done
            }
            val initial: Char = 'G'
        }
        """#

    public static let diff = #"""
        diff --git a/notes.md b/notes.md
        index 83b5ef3..f14b06e 100644
        --- a/notes.md
        +++ b/notes.md
        @@ -1,5 +1,5 @@ # Notes
         A line that stays.
        --- a removed line that starts with dashes
        +++ an added line that starts with pluses
        -old
        +new
         Another that stays.
        \ No newline at end of file
        """#

    /// Every sample with the name its fence would carry.
    public static let all: [(language: String, code: String)] = [
        ("swift", swift), ("go", go), ("python", python), ("javascript", javascript), ("typescript", typescript), ("json", json),
        ("yaml", yaml), ("bash", shell), ("sql", sql), ("html", html), ("css", css), ("rust", rust), ("kotlin", kotlin), ("diff", diff),
    ]
}

public extension CodeFixtures {
    /// The samples as a reply would hold them: each under its language's
    /// name, in its own fence.
    static func reply(_ languages: [String]) -> String {
        all.filter { languages.contains($0.language) }.map { "In `\($0.language)`:\n\n```\($0.language)\n\($0.code)\n```" }.joined(separator: "\n\n")
    }
}
