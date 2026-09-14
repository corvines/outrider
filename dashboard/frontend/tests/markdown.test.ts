import {describe, expect, it} from "bun:test";
import {isWebLink, renderMarkdown} from "../src/markdown";

describe("chat Markdown", () => {
  it("renders headings, paragraphs, emphasis and inline code", () => {
    const html = renderMarkdown("## Hello\n\n**Bold**, *italic*, ~~old~~ and `a < b`.\n\nNext paragraph.");
    for (const part of ["<h2>Hello</h2>", "<strong>Bold</strong>", "<em>italic</em>", "<s>old</s>", "<code>a &lt; b</code>", "<p>Next paragraph.</p>"]) {
      expect(html).toContain(part);
    }
  });
  it("renders nested lists, ordered lists, quotes and tables", () => {
    const html = renderMarkdown("- One\n  - Nested\n- Two\n\n1. First\n2. Second\n\n> Quoted\n\n| Key | Value |\n| --- | --- |\n| Name | Ling | ");
    for (const part of ["<ul>", "<ol>", "<li>Nested</li>", "<blockquote>", '<div class="markdown-table"><table>', "<th>Key</th>", "<td>Ling</td>"]) {
      expect(html).toContain(part);
    }
  });
  it("keeps code literal and gives fenced blocks a copy control", () => {
    const html = renderMarkdown('```html\n<script>alert("hi")</script>\n\n**literal**\n```');
    expect(html).toContain('<span>html</span>');
    expect(html).toContain('aria-label="Copy code"');
    expect(html).toContain('&lt;script&gt;alert(&quot;hi&quot;)&lt;/script&gt;\n\n**literal**\n');
    expect(html).not.toContain('<script>');
    expect(html).not.toContain('<strong>literal</strong>');
  });
  it("renders an unfinished fence during streaming", () => {
    const partial = renderMarkdown('```go\nfmt.Println("hello")');
    expect(partial).toContain('<span>go</span>');
    expect(partial).toContain('fmt.Println(&quot;hello&quot;)');
    expect(renderMarkdown('```go\nfmt.Println("hello")\n```\n\nDone.')).toContain('<p>Done.</p>');
  });
  it("escapes HTML and fence labels instead of executing them", () => {
    const html = renderMarkdown('<img src="https://example.test/pixel" onerror="alert(1)">\n<script>alert(1)</script>\n<iframe src="https://example.test"></iframe>\n\n```<img>\ntext\n```');
    expect(html).not.toMatch(/<(img|script|iframe)\b/);
    expect(html).toContain('&lt;img&gt;');
    expect(html).toContain('&lt;script&gt;');
  });
  it("does not fetch images", () => {
    const html = renderMarkdown('![a < b](https://example.test/private-pixel)');
    expect(html).toContain('[Image: a &lt; b]');
    expect(html).not.toContain('<img');
    expect(html).not.toContain('src=');
    expect(html).not.toContain('private-pixel');
  });
  it("permits only explicit web links", () => {
    expect(renderMarkdown('[Docs](https://example.test/docs?q=one&x=two)')).toContain('href="https://example.test/docs?q=one&amp;x=two"');
    expect(renderMarkdown('[Docs](https://example.test)')).toContain('rel="noopener noreferrer"');
    for (const url of ['javascript:alert(1)', 'java&#x73;cript:alert(1)', 'data:text/html,test', 'file:///private/file', 'wails://localhost/', 'mailto:user@example.test', '//example.test', '/relative']) {
      expect(isWebLink(url)).toBe(false);
      expect(renderMarkdown(`[link](${url})`)).not.toContain('href=');
    }
    expect(isWebLink('http://127.0.0.1:11435')).toBe(true);
    expect(isWebLink('https://example.test')).toBe(true);
  });
  it("escapes raw link attributes and code body markup", () => {
    const html = renderMarkdown('[safe](https://example.test "\" onclick=\"alert(1)")\n\n```\n</code><img src=x>\n```');
    expect(html).not.toContain('" onclick="');
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;/code&gt;&lt;img src=x&gt;');
  });
});
