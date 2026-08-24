import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

/**
 * Renders an answer as markdown.
 *
 * The backend writes markdown, so the raw text shows a comparison table as rows
 * of pipes and every citation as its own bare URL in the middle of a sentence.
 *
 * react-markdown builds React elements rather than HTML and never interprets
 * markup inside the answer, and it drops a URL whose scheme it does not know.
 * An answer therefore cannot inject an element or a javascript: link, which a
 * hand-written renderer would have to guard against on its own.
 */
export function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // A citation points at the open web, not at this interface, so it
          // opens in its own tab and carries no referrer back.
          a(props) {
            return (
              <a href={props.href} title={props.title} target="_blank" rel="noreferrer">
                {props.children}
              </a>
            )
          },
          // A comparison table is wider than the bubble more often than not, so
          // it gets a box of its own to scroll in.
          table(props) {
            return (
              <div className="markdown-table">
                <table>{props.children}</table>
              </div>
            )
          },
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
