import MarkdownIt from "markdown-it";

export function isWebLink(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:";
  } catch { return false; }
}

const markdown = new MarkdownIt({html: false, linkify: false, typographer: false});
markdown.validateLink = isWebLink;
markdown.renderer.rules.image = (tokens, index) =>
  `<span class="markdown-image">[Image: ${markdown.utils.escapeHtml(tokens[index].content || "image")}]</span>`;
markdown.renderer.rules.link_open = (tokens, index, options, _env, renderer) => {
  tokens[index].attrSet("target", "_blank");
  tokens[index].attrSet("rel", "noopener noreferrer");
  return renderer.renderToken(tokens, index, options);
};
markdown.renderer.rules.fence = (tokens, index) => {
  const token = tokens[index];
  const language = markdown.utils.escapeHtml(token.info.trim().split(/\s+/)[0] || "text");
  const code = markdown.utils.escapeHtml(token.content);
  return `<div class="markdown-code"><div class="markdown-code-header"><span>${language}</span><button type="button" class="markdown-code-copy" aria-label="Copy code">Copy</button></div><pre><code>${code}</code></pre></div>\n`;
};
markdown.renderer.rules.table_open = () => '<div class="markdown-table"><table>\n';
markdown.renderer.rules.table_close = () => '</table></div>\n';

export function renderMarkdown(text: string): string {
  return markdown.render(text);
}
