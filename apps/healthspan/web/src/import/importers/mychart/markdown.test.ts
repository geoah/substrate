import { describe, expect, it } from "vitest"
import { markdown, tokenize, unescapeHtml } from "./markdown"

// Each case's markdown is what an HTML parser that repairs nothing (the
// old script's) makes of it: the converter is held to those outputs.
const CASES: [string, string, string | null][] = [
  [
    "letter",
    "<html><head><title>Letter</title><style>p{}</style></head><body>\n<p><b>Example Hospital</b></p>\n<table><tr><td>Hospital No: 1234567</td><td>NHS No: 999 999 9999</td></tr>\n<tr><td>Patient Name: Ada Example</td></tr>\n<tr><th>GP practice identifier</th><td>A12345</td></tr></table>\n<h1>Diagnosis</h1><ul><li>First&nbsp;item</li><li><p>Nested words</p></li></ul>\n<p>Seen by patient Ada Example on 1/3/2026</p>\n<p>Yours &amp; sincerely,<br/>Dr Chris Example</p>\n</body></html>",
    "Example Hospital\n\nHospital No: 1234567 | NHS No: 999 999 9999\n\nPatient Name: Ada Example\n\nGP practice identifier | A12345\n\n**Diagnosis**\n\n- First item\n\n- Nested words\n\n\nYours & sincerely,\nDr Chris Example",
  ],
  [
    "note",
    "<p>Doing well.</p><p></p><p>Review in a year.</p>",
    "Doing well.\n\nReview in a year.",
  ],
  [
    "report",
    "<div><h4>Report</h4><p>No acute&#160;change.</p><p>Seen by patient Ada Example on 6/3/2026 10:00</p></div>",
    "**Report**\n\nNo acute change.",
  ],
  [
    "msg",
    '<div><img src="Image/Load?fileName=a"><p>See you&nbsp;soon.</p><img src="Image/Load?fileName=b"></div>',
    "See you soon.",
  ],
  [
    "refs",
    "<p>&lt;5 &gt; &quot;x&quot; &apos;y&apos; &#8211; &#x2014; &#150; &#0; &copy &ampfoo &unknown; &eacute;t&eacute; &rsquo;s &nbsp</p>",
    "<5 > \"x\" 'y' \u2013 \u2014 \u2013 \ufffd \u00a9 &foo &unknown; \u00e9t\u00e9 \u2019s",
  ],
  [
    "tags",
    "<P CLASS='a>b'>Upper</P><!-- a comment <p>hidden</p> --><p>a < b</p><!DOCTYPE html><?xml x?><script>if (a<b) {}</script><p>after</p></ p><br>",
    "Upper\n\na < b\n\nafter",
  ],
  [
    "spaces",
    "<div>  line \n  with\u00a0break \t </div><div>\u200b</div><div>-</div><div>words</div>",
    "line \n with break\n\n- words",
  ],
  [
    "cells",
    "<table><tr><td></td><td>b</td><td>c</td></tr><tr><th>| d</th></tr></table>",
    "b | c\n\nd",
  ],
  ["empty", "<div> <br> </div>", null],
  [
    "nested",
    "<ul><li><div>-</div></li><li><span>x</span><div>y</div></li></ul>",
    "- -\n\n- x\ny",
  ],
]

describe("MyChart's HTML as markdown", () => {
  it.each(CASES)("reads %s as written", (_, html, expected) => {
    expect(markdown(html)).toBe(expected)
  })

  it("gives null for nothing", () => {
    expect(markdown(null)).toBeNull()
    expect(markdown("")).toBeNull()
    expect(markdown("<p> </p><style>x</style>")).toBeNull()
  })

  it("leaves out the line a result shows each time it is opened", () => {
    expect(
      markdown("<p>Seen by patient Ada Example on 1/1/2026</p><p>Text</p>")
    ).toBe("Text")
  })
})

describe("character references", () => {
  it("reads numeric references, replacing what a parser replaces", () => {
    expect(unescapeHtml("&#65;&#x42;&#X43")).toBe("ABC")
    expect(unescapeHtml("&#150;&#128;")).toBe("–€")
    expect(unescapeHtml("&#0;&#xD800;&#1114112;")).toBe("���")
    expect(unescapeHtml("a&#1;b&#13;c")).toBe("ab\rc")
  })

  it("reads named references, the Latin-1 ones without a semicolon too", () => {
    expect(unescapeHtml("&amp;&lt;&gt;&quot;&apos;&nbsp;")).toBe("&<>\"' ")
    expect(unescapeHtml("&copy 2026 &eacute;t&eacute;")).toBe("© 2026 été")
    expect(unescapeHtml("&ndash;&rsquo;&hellip;")).toBe("–’…")
    expect(unescapeHtml("&ampfoo")).toBe("&foo")
  })

  it("keeps a name it does not know as written", () => {
    expect(unescapeHtml("&unknown; &rsquo")).toBe("&unknown; &rsquo")
  })
})

describe("the tokenizer", () => {
  it("gives tags in the order written, names in lower case", () => {
    expect(tokenize("<P class='a>b'>x</P><br/>")).toEqual([
      { type: "start", name: "p", selfClosing: false },
      { type: "text", text: "x" },
      { type: "end", name: "p" },
      { type: "start", name: "br", selfClosing: true },
      { type: "end", name: "br" },
    ])
  })

  it("drops comments, declarations and instructions", () => {
    expect(tokenize("a<!-- <p>b</p> -->c<!DOCTYPE html><?x y?>d")).toEqual([
      { type: "text", text: "a" },
      { type: "text", text: "c" },
      { type: "text", text: "d" },
    ])
  })

  it("reads a < that starts no tag as text, and a script's body raw", () => {
    expect(tokenize("1 < 2")).toEqual([{ type: "text", text: "1 < 2" }])
    expect(tokenize("<script>a<b&amp;</script>")).toEqual([
      { type: "start", name: "script", selfClosing: false },
      { type: "text", text: "a<b&amp;" },
      { type: "end", name: "script" },
    ])
  })

  it("keeps an unclosed tag's text", () => {
    expect(tokenize("a <b")).toEqual([{ type: "text", text: "a <b" }])
  })
})
